package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type WebhookPayload struct {
	Source       string `json:"source"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Details      string `json:"details"`
	ActionTaken  string `json:"action_taken"`
	ActionResult string `json:"action_result"`
	Timestamp    string `json:"timestamp"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// ChannelError says which channel failed, so a caller does not have to work it
// out from the message. cmd/alerts.go used to search the text for the channel
// name, and a Discord failure quotes a URL containing "webhook", so one failure
// was reported as two (#177).
type ChannelError struct {
	Channel Channel
	Err     error
}

func (e *ChannelError) Error() string { return string(e.Channel) + ": " + e.Err.Error() }
func (e *ChannelError) Unwrap() error { return e.Err }

// FailedChannel reports which channel err came from, if it came from one.
func FailedChannel(err error) (Channel, bool) {
	var ce *ChannelError
	if errors.As(err, &ce) {
		return ce.Channel, true
	}
	return "", false
}

// SendAll delivers event through every configured channel and returns one
// error per channel that failed. A channel that is not configured is not a
// failure and says nothing.
func SendAll(cfg *ProviderConfig, event Event) []error {
	if cfg == nil {
		return nil
	}

	var errs []error
	for _, p := range providers {
		if !p.enabled(cfg) {
			continue
		}
		if err := p.send(cfg, event); err != nil {
			errs = append(errs, &ChannelError{Channel: p.channel, Err: err})
		}
	}
	return errs
}

// Priority is how loudly a channel should announce an event. It is derived
// from the event rather than configured per message, so the two servers that
// have a priority concept agree on what "this one matters" means.
type Priority int

const (
	PriorityDefault Priority = iota
	PriorityHigh
)

// priorityFor maps an event to how loudly it should arrive. A trigger is the
// thing someone wants to be woken by; a recovery is not.
func priorityFor(event Event) Priority {
	if event.Status == "triggered" {
		return PriorityHigh
	}
	return PriorityDefault
}

func buildTelegramText(event Event) string {
	icon := "⚠️"
	if event.Status == "triggered" {
		icon = "🔴"
	}
	return fmt.Sprintf(
		"%s <b>%s</b> %s\n%s\n→ Action: %s\n→ Result: %s\n⏱️ %s",
		icon, event.Name, event.Status,
		event.Details,
		event.Action, event.Result,
		event.Time.Format("2006-01-02 15:04:05"),
	)
}

func sendTelegram(cfg *TelegramConfig, event Event) error {
	text := buildTelegramText(event)

	body := map[string]string{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	return postJSON(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken),
		body,
	)
}

func sendSlack(cfg *SlackConfig, event Event) error {
	color := "#ff0000"
	if event.Result == "success" {
		color = "#36a64f"
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"blocks": []map[string]interface{}{
					{
						"type": "section",
						"text": map[string]string{
							"type": "mrkdwn",
							"text": fmt.Sprintf(
								"*%s* %s\n%s\n> Action: %s | Result: %s\n_%s_",
								event.Name, event.Status,
								event.Details,
								event.Action, event.Result,
								event.Time.Format("2006-01-02 15:04:05"),
							),
						},
					},
				},
			},
		},
	}

	return postJSON(cfg.WebhookURL, payload)
}

func sendDiscord(cfg *DiscordConfig, event Event) error {
	color := 0xff0000
	if event.Result == "success" {
		color = 0x36a64f
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("%s %s", event.Name, event.Status),
				"description": event.Details,
				"color":       color,
				"fields": []map[string]interface{}{
					{"name": "Action", "value": event.Action, "inline": true},
					{"name": "Result", "value": event.Result, "inline": true},
				},
				"footer": map[string]string{
					"text": event.Time.Format("2006-01-02 15:04:05"),
				},
			},
		},
	}

	return postJSON(cfg.WebhookURL, payload)
}

func sendWebhook(cfg *WebhookConfig, event Event) error {
	payload := WebhookPayload{
		Source:       event.Source,
		Name:         event.Name,
		Status:       event.Status,
		Details:      event.Details,
		ActionTaken:  event.Action,
		ActionResult: event.Result,
		Timestamp:    event.Time.Format("2006-01-02 15:04:05"),
	}

	return postJSON(cfg.URL, payload)
}

// safeAddress is the part of an address that is not a credential. The host has
// to survive so that "which service is unreachable" stays answerable from the
// log when more than one channel is configured; the path and query must not,
// because that is where every provider keeps its secret. Userinfo goes with
// them — url.URL.Host excludes it.
func safeAddress(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "the configured endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func postJSON(endpoint string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		// *url.Error puts the whole request URL in its message, and the secret
		// is always in the path or the query: Telegram's bot token, a Slack or
		// Discord webhook path, a generic webhook's token parameter. This error
		// is printed as "→ notify error: ..." by the watcher, which under an
		// installed service means journald or the launchd log.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("request to %s failed: %w", safeAddress(urlErr.URL), urlErr.Err)
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("returned status %d", resp.StatusCode)
	}

	return nil
}

// TestResult is what one channel did when a test message was sent through it.
// The channel comes from the error's type rather than its text, which is the
// distinction #177 drew, and this is the shape both the CLI and the MCP tool
// report so neither has to rebuild it.
type TestResult struct {
	Channel Channel `json:"channel"`
	Sent    bool    `json:"sent"`
	Error   string  `json:"error,omitempty"`
}

// Test sends event through every enabled channel and reports each one.
//
// A channel that is configured and fails is a result, not an error: the point
// of a test is to find out which ones work, and stopping at the first failure
// would hide the rest.
func Test(cfg *ProviderConfig, event Event) []TestResult {
	failed := map[Channel]error{}
	for _, err := range SendAll(cfg, event) {
		if channel, ok := FailedChannel(err); ok {
			failed[channel] = err
		}
	}

	enabled := cfg.EnabledChannels()
	results := make([]TestResult, 0, len(enabled))
	for _, channel := range enabled {
		result := TestResult{Channel: channel, Sent: true}
		if err := failed[channel]; err != nil {
			result.Sent = false
			// The wrapped cause, not the ChannelError: the channel is already
			// a field, and repeating it in the message reads as two failures.
			result.Error = errors.Unwrap(err).Error()
		}
		results = append(results, result)
	}
	return results
}
