package all

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/dskit/services"

	"github.com/grafana/grafana/pkg/api"
	_ "github.com/grafana/grafana/pkg/extensions"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/infra/metrics"
	"github.com/grafana/grafana/pkg/infra/usagestats/statscollector"
	"github.com/grafana/grafana/pkg/modules"
	"github.com/grafana/grafana/pkg/registry"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	"github.com/grafana/grafana/pkg/services/provisioning"
	"github.com/grafana/grafana/pkg/setting"
)

// Options contains parameters for the New function.
type Options struct {
	HomePath    string
	PidFile     string
	Version     string
	Commit      string
	BuildBranch string
	Listener    net.Listener
}

// New returns a new instance of Server.
func New(opts Options, cfg *setting.Cfg, httpServer *api.HTTPServer, roleRegistry accesscontrol.RoleRegistry,
	provisioningService provisioning.ProvisioningService, backgroundServiceProvider registry.BackgroundServiceRegistry,
	usageStatsProvidersRegistry registry.UsageStatsProvidersRegistry, statsCollectorService *statscollector.Service,
) (*Server, error) {
	statsCollectorService.RegisterProviders(usageStatsProvidersRegistry.GetServices())
	s, err := newServer(opts, cfg, httpServer, roleRegistry, provisioningService, backgroundServiceProvider)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func newServer(opts Options, cfg *setting.Cfg, httpServer *api.HTTPServer, roleRegistry accesscontrol.RoleRegistry,
	provisioningService provisioning.ProvisioningService, backgroundServiceProvider registry.BackgroundServiceRegistry,
) (*Server, error) {
	s := &Server{
		HTTPServer:          httpServer,
		provisioningService: provisioningService,
		roleRegistry:        roleRegistry,
		log:                 log.New("server"),
		cfg:                 cfg,
		pidFile:             opts.PidFile,
		version:             opts.Version,
		commit:              opts.Commit,
		buildBranch:         opts.BuildBranch,
		backgroundServices:  backgroundServiceProvider.GetServices(),
	}

	s.BasicService = services.NewBasicService(s.start, s.run, s.stop).WithName(modules.All)

	return s, nil
}

// Server is responsible for managing the lifecycle of services.
type Server struct {
	*services.BasicService

	log log.Logger
	cfg *setting.Cfg
	mtx sync.Mutex

	pidFile            string
	version            string
	commit             string
	buildBranch        string
	backgroundServices []registry.BackgroundService

	HTTPServer          *api.HTTPServer
	roleRegistry        accesscontrol.RoleRegistry
	provisioningService provisioning.ProvisioningService
}

// start initializes the server and its services.
func (s *Server) start(ctx context.Context) error {
	if err := s.writePIDFile(); err != nil {
		return err
	}

	if err := metrics.SetEnvironmentInformation(s.cfg.MetricsGrafanaEnvironmentInfo); err != nil {
		return err
	}

	if err := s.roleRegistry.RegisterFixedRoles(ctx); err != nil {
		return err
	}

	return s.provisioningService.RunInitProvisioners(ctx)
}

func (s *Server) run(ctx context.Context) error {
	services := s.backgroundServices
	childRoutines := new(errgroup.Group)

	// Start background services.
	for _, svc := range services {
		if registry.IsDisabled(svc) {
			continue
		}

		service := svc
		serviceName := reflect.TypeOf(service).String()
		childRoutines.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			s.log.Debug("Starting background service", "service", serviceName)
			err := service.Run(ctx)
			// Do not return context.Canceled error since errgroup.Group only
			// returns the first error to the caller - thus we can miss a more
			// interesting error.
			if err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("Stopped background service", "service", serviceName, "reason", err)
				return fmt.Errorf("%s run error: %w", serviceName, err)
			}
			s.log.Debug("Stopped background service", "service", serviceName, "reason", err)
			return nil
		})
	}

	s.notifySystemd("READY=1")

	s.log.Debug("Waiting on services...")
	return childRoutines.Wait()
}

func (s *Server) stop(failureReason error) error {
	if failureReason != nil {
		s.log.Error("Server is shutting down", "reason", failureReason)
	} else {
		s.log.Info("Server is shutting down")
	}
	return nil
}

// writePIDFile retrieves the current process ID and writes it to file.
func (s *Server) writePIDFile() error {
	if s.pidFile == "" {
		return nil
	}

	// Ensure the required directory structure exists.
	err := os.MkdirAll(filepath.Dir(s.pidFile), 0700)
	if err != nil {
		s.log.Error("Failed to verify pid directory", "error", err)
		return fmt.Errorf("failed to verify pid directory: %s", err)
	}

	// Retrieve the PID and write it to file.
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(s.pidFile, []byte(pid), 0644); err != nil {
		s.log.Error("Failed to write pidfile", "error", err)
		return fmt.Errorf("failed to write pidfile: %s", err)
	}

	s.log.Info("Writing PID file", "path", s.pidFile, "pid", pid)
	return nil
}

// notifySystemd sends state notifications to systemd.
func (s *Server) notifySystemd(state string) {
	notifySocket := os.Getenv("NOTIFY_SOCKET")
	if notifySocket == "" {
		s.log.Debug(
			"NOTIFY_SOCKET environment variable empty or unset, can't send systemd notification")
		return
	}

	socketAddr := &net.UnixAddr{
		Name: notifySocket,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	if err != nil {
		s.log.Warn("Failed to connect to systemd", "err", err, "socket", notifySocket)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			s.log.Warn("Failed to close connection", "err", err)
		}
	}()

	_, err = conn.Write([]byte(state))
	if err != nil {
		s.log.Warn("Failed to write notification to systemd", "err", err)
	}
}
