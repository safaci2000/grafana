package all

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana/pkg/registry"
	"github.com/grafana/grafana/pkg/registry/backgroundsvcs"
	"github.com/grafana/grafana/pkg/services/accesscontrol/acimpl"
	"github.com/grafana/grafana/pkg/setting"
)

type testService struct {
	started    chan struct{}
	runErr     error
	isDisabled bool
}

func newTestService(runErr error, disabled bool) *testService {
	return &testService{
		started:    make(chan struct{}),
		runErr:     runErr,
		isDisabled: disabled,
	}
}

func (s *testService) Run(ctx context.Context) error {
	if s.isDisabled {
		return fmt.Errorf("Shouldn't run disabled service")
	}

	if s.runErr != nil {
		return s.runErr
	}
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func (s *testService) IsDisabled() bool {
	return s.isDisabled
}

func testServer(t *testing.T, services ...registry.BackgroundService) *Server {
	t.Helper()
	s, err := newServer(Options{}, setting.NewCfg(), nil, &acimpl.Service{}, nil, backgroundsvcs.NewBackgroundServiceRegistry(services...))
	require.NoError(t, err)
	return s
}

func TestServer_Run_Error(t *testing.T) {
	testErr := errors.New("boom")
	s := testServer(t, newTestService(nil, false), newTestService(testErr, false))
	_ = s.StartAsync(context.Background())
	_ = s.AwaitRunning(context.Background())
	require.ErrorIs(t, s.FailureCase(), testErr)
}

func TestServer_Shutdown(t *testing.T) {
	ctx := context.Background()

	s := testServer(t, newTestService(nil, false), newTestService(nil, true))

	ch := make(chan error)

	go func() {
		defer close(ch)

		// Wait until all services launched.
		for _, svc := range s.backgroundServices {
			if !svc.(*testService).isDisabled {
				<-svc.(*testService).started
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		s.StopAsync()
		err := s.AwaitTerminated(ctx)
		ch <- err
	}()
	err := s.StartAsync(ctx)
	require.NoError(t, err)

	err = <-ch
	require.NoError(t, err)
}
