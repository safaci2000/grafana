package commands

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/urfave/cli/v2"

	"github.com/grafana/grafana/pkg/api"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/infra/process"
	"github.com/grafana/grafana/pkg/modules"
	server "github.com/grafana/grafana/pkg/modules/all"
	grafanaapiserver "github.com/grafana/grafana/pkg/modules/grafana-apiserver"
	_ "github.com/grafana/grafana/pkg/services/alerting/conditions"
	_ "github.com/grafana/grafana/pkg/services/alerting/notifiers"
	"github.com/grafana/grafana/pkg/setting"
)

type ServerOptions struct {
	Version     string
	Commit      string
	BuildBranch string
	BuildStamp  string
	Context     *cli.Context
}

func ServerCommand(version, commit, buildBranch, buildstamp string) *cli.Command {
	return &cli.Command{
		Name:  "server",
		Usage: "run the grafana server",
		Flags: commonFlags,
		Action: func(context *cli.Context) error {
			return RunServer(ServerOptions{
				Version:     version,
				Commit:      commit,
				BuildBranch: buildBranch,
				BuildStamp:  buildstamp,
				Context:     context,
			})
		},
	}
}

func RunServer(opts ServerOptions) error {
	if Version || VerboseVersion {
		fmt.Printf("Version %s (commit: %s, branch: %s)\n", opts.Version, opts.Commit, opts.BuildBranch)
		if VerboseVersion {
			fmt.Println("Dependencies:")
			if info, ok := debug.ReadBuildInfo(); ok {
				for _, dep := range info.Deps {
					fmt.Println(dep.Path, dep.Version)
				}
			}
		}
		return nil
	}

	logger := log.New("cli")
	defer func() {
		if err := log.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close log: %s\n", err)
		}
	}()

	if err := setupProfiling(Profile, ProfileAddr, ProfilePort); err != nil {
		return err
	}
	if err := setupTracing(Tracing, TracingFile, logger); err != nil {
		return err
	}

	defer func() {
		// If we've managed to initialize them, this is the last place
		// where we're able to log anything that'll end up in Grafana's
		// log files.
		// Since operators are not always looking at stderr, we'll try
		// to log any and all panics that are about to crash Grafana to
		// our regular log locations before exiting.
		if r := recover(); r != nil {
			reason := fmt.Sprintf("%v", r)
			logger.Error("Critical error", "reason", reason, "stackTrace", string(debug.Stack()))
			panic(r)
		}
	}()

	setBuildInfo(opts)
	checkPrivileges()

	configOptions := strings.Split(ConfigOverrides, " ")
	args := setting.CommandLineArgs{
		Config:   ConfigFile,
		HomePath: HomePath,
		// tailing arguments have precedence over the options string
		Args: append(configOptions, opts.Context.Args().Slice()...),
	}

	cfg, err := setting.NewCfgFromArgs(args)
	if err != nil {
		return err
	}

	ctx := context.Background()

	moduleManager := modules.New(cfg.Target)
	moduleManager.RegisterModule(modules.All, func() (services.Service, error) {
		return server.Initialize(args,
			server.Options{
				PidFile:     PidFile,
				Version:     opts.Version,
				Commit:      opts.Commit,
				BuildBranch: opts.BuildBranch,
			},
			api.ServerOptions{})
	})

	moduleManager.RegisterModule(modules.GrafanaAPIServer, func() (services.Service, error) {
		return grafanaapiserver.New(path.Join(cfg.DataPath, "k8s"))
	})

	go listenToSystemSignals(ctx, moduleManager)

	return moduleManager.Run(ctx)
}

func validPackaging(packaging string) string {
	validTypes := []string{"dev", "deb", "rpm", "docker", "brew", "hosted", "unknown"}
	for _, vt := range validTypes {
		if packaging == vt {
			return packaging
		}
	}
	return "unknown"
}

func listenToSystemSignals(ctx context.Context, s modules.Engine) {
	signalChan := make(chan os.Signal, 1)
	sighupChan := make(chan os.Signal, 1)

	signal.Notify(sighupChan, syscall.SIGHUP)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sighupChan:
			if err := log.Reload(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to reload loggers: %s\n", err)
			}
		case sig := <-signalChan:
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := s.Shutdown(ctx, fmt.Sprintf("System signal: %s", sig)); err != nil {
				fmt.Fprintf(os.Stderr, "Timed out waiting for server to shut down\n")
			}
			return
		}
	}
}

func checkPrivileges() {
	elevated, err := process.IsRunningWithElevatedPrivileges()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking server process execution privilege. error: %s\n", err.Error())
	}
	if elevated {
		fmt.Println("Grafana server is running with elevated privileges. This is not recommended")
	}
}
