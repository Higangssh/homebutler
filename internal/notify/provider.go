package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func SendAll(cfg *ProviderConfig, event Event) []error {
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
		if err := sendWebhook(cfg.Webhook, event); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}

	return errs
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
