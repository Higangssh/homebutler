package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPM2Monitor_ParseProcesses(t *testing.T) {
	pm := &PM2Monitor{}
	procs := []pm2Process{
		{Name: "api", PM2Env: pm2Env{RestartTime: 3, Status: "online"}},
		{Name: "worker", PM2Env: pm2Env{RestartTime: 0, Status: "online"}},
	}
	data, _ := json.Marshal(procs)
	parsed := pm.parseProcesses(string(data))
	if len(parsed) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(parsed))
	}
	if parsed[0].Name != "api" {
		t.Errorf("expected api, got %s", parsed[0].Name)
	}
	if parsed[0].PM2Env.RestartTime != 3 {
		t.Errorf("expected RestartTime=3, got %d", parsed[0].PM2Env.RestartTime)
	}
}

func TestPM2Monitor_ParseProcesses_Invalid(t *testing.T) {
	pm := &PM2Monitor{}
	parsed := pm.parseProcesses("not json")
	if parsed != nil {
		t.Errorf("expected nil for invalid JSON, got %v", parsed)
	}
}

func TestPM2Monitor_DetectRestart(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name != "pm2" {
			return "", fmt.Errorf("unexpected command: %s", name)
		}
		procs := []pm2Process{
			{Name: "my-api", PM2Env: pm2Env{RestartTime: callCount, Status: "online"}},
		}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
		ReadFile: func(path string) ([]byte, error) {
			return []byte("error line 1\nerror line 2"), nil
		},
	}

	targets := []Target{
		{Container: "my-api", Kind: "pm2", Unit: "my-api"},
	}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "my-api" {
			t.Errorf("expected my-api, got %s", inc.Container)
		}
		if inc.PreLogs == "" {
			t.Error("expected non-empty PreLogs")
		}
		if inc.RestartCount < 2 {
			t.Errorf("expected RestartCount >= 2, got %d", inc.RestartCount)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident")
	}
}

func TestPM2Monitor_NoTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pm := &PM2Monitor{
		Run:      func(string, ...string) (string, error) { return "[]", nil },
		Interval: 50 * time.Millisecond,
	}
	incCh := make(chan Incident, 10)
	err := pm.Watch(ctx, nil, incCh)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestPM2Monitor_CaptureErrorLog(t *testing.T) {
	pm := &PM2Monitor{}
	mockRead := func(path string) ([]byte, error) {
		lines := make([]string, 150)
		for i := range lines {
			lines[i] = fmt.Sprintf("line %d", i+1)
		}
		return []byte(strings.Join(lines, "\n")), nil
	}
	result := pm.captureErrorLog(mockRead, "test-app")
	// Should only have last 100 lines
	var nonEmpty int
	for _, l := range strings.Split(result, "\n") {
		if l != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 100 {
		t.Errorf("expected <= 100 lines, got %d", nonEmpty)
	}
}
