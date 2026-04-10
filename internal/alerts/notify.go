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
	t, _ := time.Parse("2006-01-02 15:04:05", event.Time)
	ne := notify.Event{
		Source:  "alerts",
		Name:    event.RuleName,
		Status:  event.Status,
		Details: event.Details,
		Action:  event.Action,
		Result:  event.Result,
		Time:    t,
	}
	return notify.SendAll(cfg, ne)
}
