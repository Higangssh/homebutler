package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDockerEvent_ContainerName(t *testing.T) {
	ev := dockerEvent{
		Status: "die",
		ID:     "abc123",
		Actor: dockerEventActor{
			Attributes: map[string]string{"name": "my-nginx"},
		},
		Time: 1700000000,
	}
	if got := ev.containerName(); got != "my-nginx" {
		t.Errorf("expected my-nginx, got %s", got)
	}
}

func TestDockerEvent_ContainerName_Fallback(t *testing.T) {
	ev := dockerEvent{
		Status: "die",
		ID:     "abc123",
		Actor:  dockerEventActor{},
		Time:   1700000000,
	}
	if got := ev.containerName(); got != "abc123" {
		t.Errorf("expected abc123 (fallback to ID), got %s", got)
	}
}

func TestDockerEvent_ContainerName_NilAttributes(t *testing.T) {
	ev := dockerEvent{
		Status: "die",
		ID:     "abc123",
		Actor:  dockerEventActor{Attributes: nil},
	}
	if got := ev.containerName(); got != "abc123" {
		t.Errorf("expected abc123 (nil attributes), got %s", got)
	}
}

func TestDockerEvent_ParseJSON(t *testing.T) {
	raw := `{"status":"die","id":"deadbeef","Actor":{"Attributes":{"name":"redis"}},"time":1700000000}`
	var ev dockerEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if ev.Status != "die" {
		t.Errorf("expected die, got %s", ev.Status)
	}
	if ev.containerName() != "redis" {
		t.Errorf("expected redis, got %s", ev.containerName())
	}
}

func TestCaptureLogsWithRunner_Success(t *testing.T) {
	runner := func(name string, args ...string) (string, error) {
		return "line1\nline2\nline3", nil
	}
	out := captureLogsWithRunner(runner, "docker", "nginx", "100")
	if out != "line1\nline2\nline3" {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestCaptureLogsWithRunner_Error(t *testing.T) {
	runner := func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("container not found")
	}
	out := captureLogsWithRunner(runner, "docker", "nginx", "100")
	if out == "" {
		t.Error("expected non-empty error message")
	}
}

// fakeEventStream creates an EventStreamer that writes the given lines then closes.
func fakeEventStream(lines ...string) EventStreamer {
	return func(ctx context.Context) (io.ReadCloser, func(), error) {
		data := strings.Join(lines, "\n") + "\n"
		r := io.NopCloser(bytes.NewReader([]byte(data)))
		return r, func() {}, nil
	}
}

func TestDockerMonitor_Watch_EmptyTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	dm := &DockerMonitor{}
	incCh := make(chan Incident, 10)
	err := dm.Watch(ctx, nil, incCh)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestDockerMonitor_Watch_EmptyTargetsSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	dm := &DockerMonitor{}
	incCh := make(chan Incident, 10)
	err := dm.Watch(ctx, []Target{}, incCh)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestDockerMonitor_Watch_MalformedJSON(t *testing.T) {
	// Malformed JSON lines should be skipped, not cause a crash
	runner := func(name string, args ...string) (string, error) {
		return "fake logs", nil
	}

	dm := &DockerMonitor{
		Run:          runner,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			"this is not json",
			`{"broken`,
			`{"status":"die","id":"abc","Actor":{"Attributes":{"name":"nginx"}},"time":1700000000}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident after malformed JSON")
	}
}

func TestDockerMonitor_Watch_UnwatchedContainer(t *testing.T) {
	// Events for unwatched containers should be ignored
	runner := func(name string, args ...string) (string, error) {
		return "logs", nil
	}

	dm := &DockerMonitor{
		Run:          runner,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			`{"status":"die","id":"unk","Actor":{"Attributes":{"name":"unknown-container"}},"time":1700000000}`,
			`{"status":"die","id":"ng","Actor":{"Attributes":{"name":"nginx"}},"time":1700000001}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		// Should only get nginx, not unknown-container
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestDockerMonitor_Watch_ContextCancel(t *testing.T) {
	// A blocking stream that never sends events; cancel the context
	dm := &DockerMonitor{
		Run: func(name string, args ...string) (string, error) {
			return "", nil
		},
		Events: func(ctx context.Context) (io.ReadCloser, func(), error) {
			// A reader that blocks until context is done
			pr, pw := io.Pipe()
			go func() {
				<-ctx.Done()
				pw.Close()
			}()
			return pr, func() {}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	done := make(chan error, 1)
	go func() {
		done <- dm.Watch(ctx, targets, incCh)
	}()

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after context cancel")
	}
}

func TestDockerMonitor_Watch_EventStreamerError(t *testing.T) {
	dm := &DockerMonitor{
		Events: func(ctx context.Context) (io.ReadCloser, func(), error) {
			return nil, nil, fmt.Errorf("docker not available")
		},
	}

	ctx := context.Background()
	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	err := dm.Watch(ctx, targets, incCh)
	if err == nil {
		t.Fatal("expected error from event streamer")
	}
	if !strings.Contains(err.Error(), "docker not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDockerMonitor_Watch_CommandRunnerError(t *testing.T) {
	// Even when log capture fails, an incident should still be sent
	runner := func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("docker daemon not running")
	}

	dm := &DockerMonitor{
		Run:          runner,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			`{"status":"die","id":"abc","Actor":{"Attributes":{"name":"nginx"}},"time":1700000000}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
		if !strings.Contains(inc.PreLogs, "failed to capture logs") {
			t.Errorf("expected failure message in PreLogs, got: %s", inc.PreLogs)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestDockerMonitor_Watch_StreamEnded(t *testing.T) {
	// Stream with no events that closes immediately
	dm := &DockerMonitor{
		Run: func(name string, args ...string) (string, error) {
			return "", nil
		},
		Events: fakeEventStream(), // empty
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	err := dm.Watch(ctx, targets, incCh)
	if err == nil || !strings.Contains(err.Error(), "docker events stream ended") {
		t.Errorf("expected stream ended error, got: %v", err)
	}
}

func TestDockerMonitor_Watch_SaveIncident(t *testing.T) {
	dir := t.TempDir()
	runner := func(name string, args ...string) (string, error) {
		return "some logs", nil
	}

	dm := &DockerMonitor{
		Run:          runner,
		Dir:          dir,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			`{"status":"die","id":"abc","Actor":{"Attributes":{"name":"nginx"}},"time":1700000000}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
		// Verify incident was saved to disk
		loaded, err := LoadIncident(dir, inc.ID)
		if err != nil {
			t.Errorf("failed to load saved incident: %v", err)
		}
		if loaded != nil && loaded.Container != "nginx" {
			t.Errorf("saved incident has wrong container: %s", loaded.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestDockerMonitor_Watch_BlankLines(t *testing.T) {
	// Blank lines in the event stream should be skipped
	runner := func(name string, args ...string) (string, error) {
		return "logs", nil
	}

	dm := &DockerMonitor{
		Run:          runner,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			"",
			"  ",
			`{"status":"die","id":"abc","Actor":{"Attributes":{"name":"nginx"}},"time":1700000000}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

// errorReader is a reader that returns an error after the given data.
type errorReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *errorReader) Close() error { return nil }

func TestDockerMonitor_Watch_ScanError(t *testing.T) {
	// Scanner receives an error from the reader
	dm := &DockerMonitor{
		Run: func(name string, args ...string) (string, error) {
			return "", nil
		},
		Events: func(ctx context.Context) (io.ReadCloser, func(), error) {
			// Return a reader that produces an error
			return &errorReader{
				data: []byte{},
				err:  fmt.Errorf("broken pipe"),
			}, func() {}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "nginx", Kind: "docker"}}

	err := dm.Watch(ctx, targets, incCh)
	// Should return the stream ended error (scanner reads 0 bytes, no scan error propagated to errCh)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDockerMonitor_Watch_EffectiveUnit(t *testing.T) {
	// Target with Unit set should use Unit as the container name to watch
	runner := func(name string, args ...string) (string, error) {
		return "logs", nil
	}

	dm := &DockerMonitor{
		Run:          runner,
		PostLogDelay: 1 * time.Millisecond,
		Events: fakeEventStream(
			`{"status":"die","id":"abc","Actor":{"Attributes":{"name":"my-real-container"}},"time":1700000000}`,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "alias", Kind: "docker", Unit: "my-real-container"}}

	go func() {
		_ = dm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		// The event name matched the EffectiveUnit, so incident should be created
		if inc.Container != "my-real-container" {
			t.Errorf("expected my-real-container, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}
