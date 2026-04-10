package watch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSystemdMonitor_ParseState(t *testing.T) {
	sm := &SystemdMonitor{}
	output := "ActiveState=active\nSubState=running\nExecMainStartTimestamp=Thu 2025-01-01 00:00:00 UTC"
	s := sm.parseState(output)
	if s.ActiveState != "active" {
		t.Errorf("expected active, got %s", s.ActiveState)
	}
	if s.SubState != "running" {
		t.Errorf("expected running, got %s", s.SubState)
	}
	if !strings.Contains(s.StartTS, "2025-01-01") {
		t.Errorf("unexpected StartTS: %s", s.StartTS)
	}
}

func TestSystemdMonitor_DetectFailure(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				// Initial seed: active
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			// Second poll: failed
			return "ActiveState=failed\nSubState=failed\nExecMainStartTimestamp=ts1", nil
		}
		if name == "journalctl" {
			return "Jan 01 error: segfault", nil
		}
		return "", fmt.Errorf("unexpected command: %s", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
	}

	targets := []Target{
		{Container: "nginx", Kind: "systemd", Unit: "nginx.service"},
	}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "nginx" {
			t.Errorf("expected nginx, got %s", inc.Container)
		}
		if inc.PreLogs == "" {
			t.Error("expected non-empty PreLogs")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident")
	}
}

func TestSystemdMonitor_DetectStartChange(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			// Restarted (active but different timestamp)
			return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts2", nil
		}
		if name == "journalctl" {
			return "journal log lines", nil
		}
		return "", fmt.Errorf("unexpected: %s", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
	}

	targets := []Target{
		{Container: "myunit", Kind: "systemd", Unit: "myunit.service"},
	}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "myunit" {
			t.Errorf("expected myunit, got %s", inc.Container)
		}
		if inc.PrevStarted != "ts1" {
			t.Errorf("expected PrevStarted=ts1, got %s", inc.PrevStarted)
		}
		if inc.CurrStarted != "ts2" {
			t.Errorf("expected CurrStarted=ts2, got %s", inc.CurrStarted)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident")
	}
}

func TestSystemdMonitor_NoTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	sm := &SystemdMonitor{
		Run:      func(string, ...string) (string, error) { return "", nil },
		Interval: 50 * time.Millisecond,
	}
	incCh := make(chan Incident, 10)
	err := sm.Watch(ctx, nil, incCh)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}
