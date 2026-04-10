package notify

import (
	"strings"
	"testing"
	"time"
)

func TestSendAll_NilConfig(t *testing.T) {
	errs := SendAll(nil, Event{})
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestSendAll_EmptyConfig(t *testing.T) {
	errs := SendAll(&ProviderConfig{}, Event{})
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestBuildTelegramText(t *testing.T) {
	ev := Event{
		Source:  "alerts",
		Name:    "high-cpu",
		Status:  "triggered",
		Details: "CPU > 90%",
		Action:  "restart",
		Result:  "success",
		Time:    time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	text := buildTelegramText(ev)

	if !strings.Contains(text, "🔴") {
		t.Error("triggered status should use 🔴")
	}
	if !strings.Contains(text, "<b>high-cpu</b>") {
		t.Error("should contain bold event name")
	}
	if !strings.Contains(text, "2026-01-15 10:30:00") {
		t.Error("should contain formatted time")
	}
	if !strings.Contains(text, "CPU > 90%") {
		t.Error("should contain details")
	}
}

func TestBuildTelegramText_Warning(t *testing.T) {
	ev := Event{
		Name:   "disk-check",
		Status: "warning",
		Time:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	text := buildTelegramText(ev)
	if !strings.Contains(text, "⚠️") {
		t.Error("non-triggered status should use ⚠️")
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name   string
		cfg    ProviderConfig
		expect bool
	}{
		{"all nil", ProviderConfig{}, true},
		{"telegram set", ProviderConfig{Telegram: &TelegramConfig{}}, false},
		{"slack set", ProviderConfig{Slack: &SlackConfig{}}, false},
		{"discord set", ProviderConfig{Discord: &DiscordConfig{}}, false},
		{"webhook set", ProviderConfig{Webhook: &WebhookConfig{}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEmpty(); got != tt.expect {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.expect)
			}
		})
	}
}
