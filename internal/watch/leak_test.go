package watch

import (
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Reconnecting is the loop this process spends its life in, so anything it
// leaks per attempt accumulates for as long as docker is down. Each attempt
// spawns a scanner goroutine and a child process; both have to be gone before
// the next attempt starts.
func TestReconnectingDoesNotLeakGoroutines(t *testing.T) {
	dm := &DockerMonitor{
		Dir:          t.TempDir(),
		PostLogDelay: time.Millisecond,
		Run:          func(string, ...string) (string, error) { return "", nil },
		Events: func(context.Context) (io.ReadCloser, func(), error) {
			return io.NopCloser(strings.NewReader("")), func() {}, nil
		},
	}
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	// Settle, then take a baseline.
	for i := 0; i < 3; i++ {
		_ = dm.watchOnce(context.Background(), targets, make(chan Incident, 1))
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 200; i++ {
		_ = dm.watchOnce(context.Background(), targets, make(chan Incident, 1))
	}
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()

	// A per-attempt leak would show as roughly +200 here; a few in flight is
	// scheduling noise rather than accumulation.
	if after-before > 10 {
		t.Errorf("200 reconnects grew goroutines from %d to %d", before, after)
	}
}
