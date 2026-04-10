package watch

import (
	"sync"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
)

func makeTestNotifier(s NotifySettings, called *int) *WatchNotifier {
	var mu sync.Mutex
	return &WatchNotifier{
		Settings:  s,
		AlertsCfg: &alerts.NotifyConfig{},
		cooldowns: make(map[string]time.Time),
		notifyFunc: func(cfg *alerts.NotifyConfig, ev alerts.NotifyEvent) []error {
			mu.Lock()
			*called++
			mu.Unlock()
			return nil
		},
	}
}

func baseIncident(container string, t time.Time) Incident {
	return Incident{
		ID:         "test-1",
		Container:  container,
		DetectedAt: t,
	}
}

func TestNotifyIncident_Disabled(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{Enabled: false}, &called)
	inc := baseIncident("nginx", time.Now())
	flap := FlappingResult{IsFlapping: false}

	err := wn.NotifyIncident(inc, flap, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Error("expected no notification when disabled")
	}
}

func TestNotifyIncident_OnIncident(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnIncident: true,
		Cooldown:   "5m",
	}, &called)
	inc := baseIncident("nginx", time.Now())
	flap := FlappingResult{IsFlapping: false}

	err := wn.NotifyIncident(inc, flap, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestNotifyIncident_OnFlapping(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnFlapping: true,
		Cooldown:   "5m",
	}, &called)
	inc := baseIncident("redis", time.Now())
	flap := FlappingResult{IsFlapping: true, Level: "warning", Count: 3}

	err := wn.NotifyIncident(inc, flap, &CrashSummary{Category: "oom", Reason: "killed"}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestNotifyIncident_OnIncidentFalse_NoNotify(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnIncident: false,
		OnFlapping: true,
		Cooldown:   "5m",
	}, &called)
	inc := baseIncident("nginx", time.Now())
	flap := FlappingResult{IsFlapping: false}

	err := wn.NotifyIncident(inc, flap, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Error("expected no notification when on_incident=false and not flapping")
	}
}

func TestNotifyIncident_CooldownSkip(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnIncident: true,
		Cooldown:   "5m",
	}, &called)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	inc := baseIncident("nginx", now)
	flap := FlappingResult{IsFlapping: false}

	_ = wn.NotifyIncident(inc, flap, nil, now)
	if called != 1 {
		t.Fatalf("expected 1 call after first, got %d", called)
	}

	_ = wn.NotifyIncident(inc, flap, nil, now.Add(2*time.Minute))
	if called != 1 {
		t.Error("expected cooldown to skip second notification")
	}
}

func TestNotifyIncident_CooldownExpired(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnIncident: true,
		Cooldown:   "5m",
	}, &called)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	inc := baseIncident("nginx", now)
	flap := FlappingResult{IsFlapping: false}

	_ = wn.NotifyIncident(inc, flap, nil, now)
	if called != 1 {
		t.Fatalf("expected 1 call, got %d", called)
	}

	_ = wn.NotifyIncident(inc, flap, nil, now.Add(6*time.Minute))
	if called != 2 {
		t.Errorf("expected 2 calls after cooldown expired, got %d", called)
	}
}

func TestNotifyIncident_NilAlertsCfg(t *testing.T) {
	wn := &WatchNotifier{
		Settings: NotifySettings{
			Enabled:    true,
			OnIncident: true,
			Cooldown:   "5m",
		},
		AlertsCfg: nil,
		cooldowns: make(map[string]time.Time),
	}
	inc := baseIncident("nginx", time.Now())
	flap := FlappingResult{IsFlapping: false}

	err := wn.NotifyIncident(inc, flap, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotifyIncident_IndependentCooldown(t *testing.T) {
	called := 0
	wn := makeTestNotifier(NotifySettings{
		Enabled:    true,
		OnIncident: true,
		Cooldown:   "5m",
	}, &called)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	flap := FlappingResult{IsFlapping: false}

	_ = wn.NotifyIncident(baseIncident("nginx", now), flap, nil, now)
	_ = wn.NotifyIncident(baseIncident("redis", now), flap, nil, now)

	if called != 2 {
		t.Errorf("expected 2 calls for different containers, got %d", called)
	}
}
