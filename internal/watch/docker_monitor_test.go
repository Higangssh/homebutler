package watch

import (
	"encoding/json"
	"fmt"
	"testing"
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
