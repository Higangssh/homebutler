package alerts

import (
	"testing"
	"time"
)

func TestPrevStateChanged(t *testing.T) {
	p := newPrevState()

	// First time "ok" should not trigger
	if p.changed("cpu", "ok") {
		t.Error("first ok should not trigger")
	}

	// Still "ok" should not trigger
	if p.changed("cpu", "ok") {
		t.Error("repeated ok should not trigger")
	}

	// Change to "warning" should trigger
	if !p.changed("cpu", "warning") {
		t.Error("ok -> warning should trigger")
	}

	// Stay "warning" should not trigger
	if p.changed("cpu", "warning") {
		t.Error("repeated warning should not trigger")
	}

	// Change to "critical" should trigger
	if !p.changed("cpu", "critical") {
		t.Error("warning -> critical should trigger")
	}

	// Recover to "ok" should trigger
	if !p.changed("cpu", "ok") {
		t.Error("critical -> ok should trigger")
	}
}

func TestPrevStateContainerChanged(t *testing.T) {
	p := newPrevState()

	// First time "running" should not trigger
	if p.containerChanged("nginx", "running") {
		t.Error("first running should not trigger")
	}

	// Still "running" should not trigger
	if p.containerChanged("nginx", "running") {
		t.Error("repeated running should not trigger")
	}

	// Change to "exited" should trigger
	if !p.containerChanged("nginx", "exited") {
		t.Error("running -> exited should trigger")
	}

	// Stay "exited" should not trigger
	if p.containerChanged("nginx", "exited") {
		t.Error("repeated exited should not trigger")
	}

	// Recover to "running" should trigger
	if !p.containerChanged("nginx", "running") {
		t.Error("exited -> running should trigger")
	}
}

func TestPrevStateMultipleResources(t *testing.T) {
	p := newPrevState()

	// Different resources are independent
	p.changed("cpu", "ok")
	p.changed("memory", "ok")
	p.changed("disk:/", "ok")

	if !p.changed("cpu", "critical") {
		t.Error("cpu change should trigger")
	}
	if p.changed("memory", "ok") {
		t.Error("memory unchanged should not trigger")
	}
	if !p.changed("disk:/", "warning") {
		t.Error("disk change should trigger")
	}
}

func TestCheckResource(t *testing.T) {
	prev := newPrevState()
	ch := make(chan Event, 10)
	now := time.Now()

	// First check at ok - no event
	checkResource(prev, ch, now, "cpu", 50, 90, "CPU")
	if len(ch) != 0 {
		t.Error("should not emit event for initial ok")
	}

	// Cross into critical
	checkResource(prev, ch, now, "cpu", 95, 90, "CPU")
	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}
	e := <-ch
	if e.Severity != "critical" {
		t.Errorf("expected critical, got %s", e.Severity)
	}
	if e.Current != 95 {
		t.Errorf("expected current 95, got %f", e.Current)
	}

	// Stay critical - no new event
	checkResource(prev, ch, now, "cpu", 96, 90, "CPU")
	if len(ch) != 0 {
		t.Error("should not emit duplicate event")
	}

	// Recover to ok
	checkResource(prev, ch, now, "cpu", 50, 90, "CPU")
	if len(ch) != 1 {
		t.Fatalf("expected recovery event, got %d", len(ch))
	}
	e = <-ch
	if e.Severity != "ok" {
		t.Errorf("expected ok, got %s", e.Severity)
	}
}

func TestCheckResourceWarning(t *testing.T) {
	prev := newPrevState()
	ch := make(chan Event, 10)
	now := time.Now()

	// Start ok
	checkResource(prev, ch, now, "memory", 50, 90, "Memory")
	if len(ch) != 0 {
		t.Error("should not emit for initial ok")
	}

	// Enter warning zone (90% of threshold = 81)
	checkResource(prev, ch, now, "memory", 82, 90, "Memory")
	if len(ch) != 1 {
		t.Fatalf("expected warning event, got %d", len(ch))
	}
	e := <-ch
	if e.Severity != "warning" {
		t.Errorf("expected warning, got %s", e.Severity)
	}

	// Escalate to critical
	checkResource(prev, ch, now, "memory", 92, 90, "Memory")
	if len(ch) != 1 {
		t.Fatalf("expected critical event, got %d", len(ch))
	}
	e = <-ch
	if e.Severity != "critical" {
		t.Errorf("expected critical, got %s", e.Severity)
	}
}

func TestPrevStateContainerPrune(t *testing.T) {
	p := newPrevState()

	// Add some containers
	p.containerChanged("nginx", "running")
	p.containerChanged("redis", "running")
	p.containerChanged("postgres", "running")

	if len(p.containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(p.containers))
	}

	// Simulate pruning (only nginx and redis still exist)
	seen := map[string]bool{"nginx": true, "redis": true}
	for name := range p.containers {
		if !seen[name] {
			delete(p.containers, name)
		}
	}

	if len(p.containers) != 2 {
		t.Fatalf("expected 2 containers after prune, got %d", len(p.containers))
	}
	if _, exists := p.containers["postgres"]; exists {
		t.Error("postgres should have been pruned")
	}
}

func TestFormatEvent(t *testing.T) {
	tests := []struct {
		severity string
		wantIcon string
	}{
		{"ok", "✅"},
		{"warning", "⚠️"},
		{"critical", "🔴"},
	}

	for _, tt := range tests {
		e := Event{
			Time:     time.Date(2026, 3, 2, 17, 30, 0, 0, time.UTC),
			Severity: tt.severity,
			Message:  "CPU at 95.0% (threshold: 90%)",
		}
		result := FormatEvent(e)
		if result == "" {
			t.Errorf("FormatEvent returned empty for severity %s", tt.severity)
		}
		if !containsStr(result, tt.wantIcon) {
			t.Errorf("FormatEvent(%s) missing icon %s, got: %s", tt.severity, tt.wantIcon, result)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
