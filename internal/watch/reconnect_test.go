package watch

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A dropped event stream is not a reason to stop monitoring. Before #80 this
// returned, the process exited, and a supervisor restarted it — which turns a
// docker restart into a loop of process starts.
func TestDockerMonitorReconnectsAfterTheStreamEnds(t *testing.T) {
	var opened int32
	dm := &DockerMonitor{
		Dir:          t.TempDir(),
		PostLogDelay: time.Millisecond,
		Run:          func(string, ...string) (string, error) { return "", nil },
		Events: func(context.Context) (io.ReadCloser, func(), error) {
			// Every stream ends immediately, so Watch can only make progress by
			// reconnecting.
			atomic.AddInt32(&opened, 1)
			return io.NopCloser(strings.NewReader("")), func() {}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*reconnectMin)
	defer cancel()

	incCh := make(chan Incident, 1)
	err := dm.Watch(ctx, []Target{{Container: "nginx", Kind: "docker"}}, incCh)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Watch should only return once the context is done, got %v", err)
	}
	if n := atomic.LoadInt32(&opened); n < 2 {
		t.Errorf("stream was opened %d time(s); a dropped stream should be reconnected", n)
	}
}

// A stream that cannot be opened at all — docker down — is the same situation
// and must not return either.
func TestDockerMonitorReconnectsWhenTheStreamCannotOpen(t *testing.T) {
	var attempts int32
	dm := &DockerMonitor{
		Dir: t.TempDir(),
		Run: func(string, ...string) (string, error) { return "", nil },
		Events: func(context.Context) (io.ReadCloser, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return nil, nil, errors.New("cannot connect to the Docker daemon")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*reconnectMin)
	defer cancel()

	err := dm.Watch(ctx, []Target{{Container: "nginx", Kind: "docker"}}, make(chan Incident, 1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Watch should only return once the context is done, got %v", err)
	}
	if n := atomic.LoadInt32(&attempts); n < 2 {
		t.Errorf("connect was attempted %d time(s); a daemon that is down should be retried", n)
	}
}

// Cancelling has to win over a pending reconnect delay, or shutdown waits for
// a backoff that can be half a minute.
func TestDockerMonitorStopsPromptlyWhileWaitingToReconnect(t *testing.T) {
	dm := &DockerMonitor{
		Dir: t.TempDir(),
		Run: func(string, ...string) (string, error) { return "", nil },
		Events: func(context.Context) (io.ReadCloser, func(), error) {
			return io.NopCloser(strings.NewReader("")), func() {}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- dm.Watch(ctx, []Target{{Container: "nginx", Kind: "docker"}}, make(chan Incident, 1))
	}()

	// Let it drop once and enter the reconnect wait, then cancel.
	time.Sleep(reconnectMin / 4)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(reconnectMin):
		t.Error("Watch kept waiting on the reconnect delay after the context was cancelled")
	}
}

// Watch with nothing to watch still blocks on the context rather than spinning.
func TestDockerMonitorWithNoTargetsWaitsForCancellation(t *testing.T) {
	dm := &DockerMonitor{Dir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := dm.Watch(ctx, nil, make(chan Incident, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}
