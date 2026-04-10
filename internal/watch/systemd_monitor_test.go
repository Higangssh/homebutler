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

func TestSystemdMonitor_ParseState_Empty(t *testing.T) {
	sm := &SystemdMonitor{}
	s := sm.parseState("")
	if s.ActiveState != "" || s.SubState != "" || s.StartTS != "" {
		t.Errorf("expected empty state from empty input, got %+v", s)
	}
}

func TestSystemdMonitor_ParseState_Garbage(t *testing.T) {
	sm := &SystemdMonitor{}
	s := sm.parseState("not a valid systemctl output\nrandom=stuff\n")
	if s.ActiveState != "" {
		t.Errorf("expected empty ActiveState from garbage, got %s", s.ActiveState)
	}
}

func TestSystemdMonitor_ParseState_ExtraWhitespace(t *testing.T) {
	sm := &SystemdMonitor{}
	s := sm.parseState("  ActiveState=active  \n  SubState=running  \n  ExecMainStartTimestamp=ts1  ")
	if s.ActiveState != "active" {
		t.Errorf("expected active (trimmed), got %q", s.ActiveState)
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

func TestSystemdMonitor_NilRunner(t *testing.T) {
	ctx := context.Background()
	sm := &SystemdMonitor{Run: nil}
	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "x", Kind: "systemd", Unit: "x.service"}}
	err := sm.Watch(ctx, targets, incCh)
	if err == nil || !strings.Contains(err.Error(), "requires a CommandRunner") {
		t.Errorf("expected CommandRunner error, got %v", err)
	}
}

func TestSystemdMonitor_ActiveToInactive_WithStartChange(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			return "ActiveState=inactive\nSubState=dead\nExecMainStartTimestamp=ts2", nil
		}
		if name == "journalctl" {
			return "journal output", nil
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "svc", Kind: "systemd", Unit: "svc.service"}}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "svc" {
			t.Errorf("expected svc, got %s", inc.Container)
		}
		if !strings.Contains(inc.PostLogs, "inactive") {
			t.Errorf("expected inactive in PostLogs, got %s", inc.PostLogs)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestSystemdMonitor_ActiveToInactive_NoStartChange(t *testing.T) {
	// active -> inactive but SAME startTS: NOT an incident (inactiveStopped && startChanged must both be true)
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			// inactive but same timestamp
			return "ActiveState=inactive\nSubState=dead\nExecMainStartTimestamp=ts1", nil
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "svc", Kind: "systemd", Unit: "svc.service"}}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case <-incCh:
		t.Error("did NOT expect incident for inactive with same startTS")
	case <-ctx.Done():
		// Good: no incident generated
	}
}

func TestSystemdMonitor_CommandRunnerError(t *testing.T) {
	// When systemctl fails on poll, it should just continue, not crash
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			return "", fmt.Errorf("connection refused")
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "svc", Kind: "systemd", Unit: "svc.service"}}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case <-incCh:
		t.Error("did not expect incident when systemctl fails")
	case <-ctx.Done():
		// Good: no crash, no incident
	}
}

func TestSystemdMonitor_SeedError(t *testing.T) {
	// Error during initial seeding should be tolerated
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "", fmt.Errorf("unit not found")
			}
			// On poll, return a state so we verify it doesn't crash
			return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "svc", Kind: "systemd", Unit: "svc.service"}}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	// Should not crash, just run until context cancels
	<-ctx.Done()
}

func TestSystemdMonitor_MultipleTargets(t *testing.T) {
	callCount := map[string]int{}
	runner := func(name string, args ...string) (string, error) {
		if name == "systemctl" {
			unit := args[1] // "show" <unit>
			callCount[unit]++
			if callCount[unit] <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			if unit == "svc-a.service" {
				return "ActiveState=failed\nSubState=failed\nExecMainStartTimestamp=ts1", nil
			}
			// svc-b stays active
			return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
		}
		if name == "journalctl" {
			return "journal", nil
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{
		{Container: "svc-a", Kind: "systemd", Unit: "svc-a.service"},
		{Container: "svc-b", Kind: "systemd", Unit: "svc-b.service"},
	}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "svc-a" {
			t.Errorf("expected svc-a incident, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestSystemdMonitor_SaveIncident(t *testing.T) {
	dir := t.TempDir()
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name == "systemctl" {
			if callCount <= 1 {
				return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=ts1", nil
			}
			return "ActiveState=failed\nSubState=failed\nExecMainStartTimestamp=ts1", nil
		}
		if name == "journalctl" {
			return "logs", nil
		}
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	sm := &SystemdMonitor{Run: runner, Interval: 100 * time.Millisecond, Dir: dir}
	targets := []Target{{Container: "svc", Kind: "systemd", Unit: "svc.service"}}

	go func() {
		_ = sm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		loaded, err := LoadIncident(dir, inc.ID)
		if err != nil {
			t.Errorf("failed to load saved incident: %v", err)
		}
		if loaded != nil && loaded.Container != "svc" {
			t.Errorf("expected svc, got %s", loaded.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}
