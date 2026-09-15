package alerts

import (
	"time"

	"github.com/Higangssh/homebutler/internal/notify"
)

// Type aliases for backward compatibility.
type TelegramConfig = notify.TelegramConfig
type SlackConfig = notify.SlackConfig
type DiscordConfig = notify.DiscordConfig
type NotifyConfig = notify.ProviderConfig

// NotifyEvent represents an alert event to be sent to notification providers.
type NotifyEvent struct {
	RuleName string `json:"rule_name"`
	Status   string `json:"status"`
	Details  string `json:"details"`
	Action   string `json:"action"`
	Result   string `json:"result"`
	Time     string `json:"time"`
}

// NotifyAll sends the event to all configured providers.
// It continues sending even if some providers fail, and returns all errors.
func NotifyAll(cfg *NotifyConfig, event NotifyEvent) []error {
	if cfg == nil {
		return nil
	}
	return notify.SendAll(cfg, toEvent(event))
}

// TestNotify sends one event through every configured channel and reports each,
// so a caller learns which channels work rather than that something failed.
func TestNotify(cfg *NotifyConfig, event NotifyEvent) []notify.TestResult {
	if cfg == nil {
		return nil
	}
	return notify.Test(cfg, toEvent(event))
}

// toEvent is the one place the alerts event shape becomes a notify event.
func toEvent(event NotifyEvent) notify.Event {
	t, _ := time.Parse("2006-01-02 15:04:05", event.Time)
	return notify.Event{
		Source:  "alerts",
		Name:    event.RuleName,
		Status:  event.Status,
		Details: event.Details,
		Action:  event.Action,
		Result:  event.Result,
		Time:    t,
	}
}
