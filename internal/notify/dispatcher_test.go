package notify

import (
	"errors"
	"testing"
	"time"
)

func newTestDispatcher(providers *ProviderConfig, cooldown time.Duration, fn func(*ProviderConfig, Event) []error) *Dispatcher {
	d := NewDispatcher(providers, cooldown)
	d.sendFunc = fn
	return d
}

func TestSend_NilProvider(t *testing.T) {
	d := NewDispatcher(nil, time.Minute)
	errs := d.Send("key", Event{}, time.Now())
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestSend_EmptyProvider(t *testing.T) {
	d := NewDispatcher(&ProviderConfig{}, time.Minute)
	errs := d.Send("key", Event{}, time.Now())
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestSend_CooldownSkips(t *testing.T) {
	called := 0
	fn := func(_ *ProviderConfig, _ Event) []error {
		called++
		return nil
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 5*time.Minute, fn)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	d.Send("k", Event{}, now)
	d.Send("k", Event{}, now.Add(2*time.Minute))

	if called != 1 {
		t.Fatalf("expected 1 call, got %d", called)
	}
}

func TestSend_CooldownExpired(t *testing.T) {
	called := 0
	fn := func(_ *ProviderConfig, _ Event) []error {
		called++
		return nil
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 5*time.Minute, fn)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	d.Send("k", Event{}, now)
	d.Send("k", Event{}, now.Add(6*time.Minute))

	if called != 2 {
		t.Fatalf("expected 2 calls, got %d", called)
	}
}

func TestSend_DifferentKeysIndependent(t *testing.T) {
	called := 0
	fn := func(_ *ProviderConfig, _ Event) []error {
		called++
		return nil
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 5*time.Minute, fn)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	d.Send("a", Event{}, now)
	d.Send("b", Event{}, now)

	if called != 2 {
		t.Fatalf("expected 2 calls, got %d", called)
	}
}

func TestSendImmediate_IgnoresCooldown(t *testing.T) {
	called := 0
	fn := func(_ *ProviderConfig, _ Event) []error {
		called++
		return nil
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 5*time.Minute, fn)

	d.SendImmediate(Event{})
	d.SendImmediate(Event{})

	if called != 2 {
		t.Fatalf("expected 2 calls, got %d", called)
	}
}

func TestSend_ZeroCooldownAlwaysSends(t *testing.T) {
	called := 0
	fn := func(_ *ProviderConfig, _ Event) []error {
		called++
		return nil
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 0, fn)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	d.Send("k", Event{}, now)
	d.Send("k", Event{}, now)
	d.Send("k", Event{}, now)

	if called != 3 {
		t.Fatalf("expected 3 calls, got %d", called)
	}
}

func TestSend_ReturnsErrors(t *testing.T) {
	fn := func(_ *ProviderConfig, _ Event) []error {
		return []error{errors.New("fail")}
	}

	providers := &ProviderConfig{Telegram: &TelegramConfig{BotToken: "t", ChatID: "c"}}
	d := newTestDispatcher(providers, 0, fn)

	errs := d.Send("k", Event{}, time.Now())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}
