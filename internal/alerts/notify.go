package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TelegramConfig holds Telegram Bot API settings.
type TelegramConfig struct {
	BotToken string `yaml:"bot_token" json:"bot_token"`
	ChatID   string `yaml:"chat_id" json:"chat_id"`
}

// SlackConfig holds Slack incoming webhook settings.
type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`
}

// DiscordConfig holds Discord webhook settings.
type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`
}

// NotifyConfig holds settings for all notification providers.
type NotifyConfig struct {
	Telegram *TelegramConfig `yaml:"telegram,omitempty" json:"telegram,omitempty"`
	Slack    *SlackConfig    `yaml:"slack,omitempty" json:"slack,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty" json:"discord,omitempty"`
	Webhook  *WebhookConfig  `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

// NotifyEvent represents an alert event to be sent to notification providers.
type NotifyEvent struct {
	RuleName string `json:"rule_name"`
	Status   string `json:"status"`
	Details  string `json:"details"`
	Action   string `json:"action"`
	Result   string `json:"result"`
	Time     string `json:"time"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// NotifyAll sends the event to all configured providers.
// It continues sending even if some providers fail, and returns all errors.
func NotifyAll(cfg *NotifyConfig, event NotifyEvent) []error {
	if cfg == nil {
		return nil
	}

	var errs []error

	if cfg.Telegram != nil && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		if err := sendTelegram(cfg.Telegram, event); err != nil {
			errs = append(errs, fmt.Errorf("telegram: %w", err))
		}
	}

	if cfg.Slack != nil && cfg.Slack.WebhookURL != "" {
		if err := sendSlack(cfg.Slack, event); err != nil {
			errs = append(errs, fmt.Errorf("slack: %w", err))
		}
	}

	if cfg.Discord != nil && cfg.Discord.WebhookURL != "" {
		if err := sendDiscord(cfg.Discord, event); err != nil {
			errs = append(errs, fmt.Errorf("discord: %w", err))
		}
	}

	if cfg.Webhook != nil && cfg.Webhook.URL != "" {
		payload := WebhookPayload{
			Rule:         event.RuleName,
			Status:       event.Status,
			Details:      event.Details,
			ActionTaken:  event.Action,
			ActionResult: event.Result,
			Timestamp:    event.Time,
		}
		if err := SendWebhook(cfg.Webhook.URL, payload); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}

	return errs
}

func buildTelegramText(event NotifyEvent) string {
	icon := "⚠️"
	if event.Status == "triggered" {
		icon = "🔴"
	}
	return fmt.Sprintf(
		"%s <b>%s</b> %s\n%s\n→ Action: %s\n→ Result: %s\n⏱️ %s",
		icon, event.RuleName, event.Status,
		event.Details,
		event.Action, event.Result,
		event.Time,
	)
}

func sendTelegram(cfg *TelegramConfig, event NotifyEvent) error {
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

func sendSlack(cfg *SlackConfig, event NotifyEvent) error {
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
								event.RuleName, event.Status,
								event.Details,
								event.Action, event.Result,
								event.Time,
							),
						},
					},
				},
			},
		},
	}

	return postJSON(cfg.WebhookURL, payload)
}

func sendDiscord(cfg *DiscordConfig, event NotifyEvent) error {
	color := 0xff0000
	if event.Result == "success" {
		color = 0x36a64f
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("%s %s", event.RuleName, event.Status),
				"description": event.Details,
				"color":       color,
				"fields": []map[string]interface{}{
					{"name": "Action", "value": event.Action, "inline": true},
					{"name": "Result", "value": event.Result, "inline": true},
				},
				"footer": map[string]string{
					"text": event.Time,
				},
			},
		},
	}

	return postJSON(cfg.WebhookURL, payload)
}

func postJSON(url string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
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
