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

// TestEvent is the message a test sends. It lives here because three callers
// need the same one — the CLI, the MCP tool and the dashboard button — and the
// first two had each written their own, disagreeing on the status, which is
// what the delivered priority is derived from.
func TestEvent() NotifyEvent {
	return NotifyEvent{
		RuleName: "test-notification",
		Status:   "triggered",
		Details:  "This is a test notification from homebutler",
		Action:   "notify",
		Result:   "success",
		Time:     time.Now().Format("2006-01-02 15:04:05"),
	}
}

// toEvent is the one place the alerts event shape becomes a notify event.
func toEvent(event NotifyEvent) notify.Event {
	// watch start sends RFC 3339 and the rules engine sends the layout below.
	// Only the second used to parse, and the error was dropped, so every
	// container and threshold alert from watch went out stamped 0001-01-01.
	t, err := time.Parse(time.RFC3339, event.Time)
	if err != nil {
		t, _ = time.ParseInLocation("2006-01-02 15:04:05", event.Time, time.Local)
	}
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
