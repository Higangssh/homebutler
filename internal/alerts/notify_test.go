package alerts

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testEvent() NotifyEvent {
	return NotifyEvent{
		RuleName: "container-down",
		Status:   "triggered",
		Details:  "uptime-kuma is exited",
		Action:   "restart",
		Result:   "success",
		Time:     "2026-04-04 21:06:43",
	}
}

func TestNotifyAll_AllProviders(t *testing.T) {
	var slackBody, discordBody, webhookBody []byte

	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer slackSrv.Close()

	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	}))
	defer discordSrv.Close()

	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer webhookSrv.Close()

	cfg := &NotifyConfig{
		Telegram: &TelegramConfig{
			BotToken: "fake-token",
			ChatID:   "12345",
		},
		Slack: &SlackConfig{
			WebhookURL: slackSrv.URL,
		},
		Discord: &DiscordConfig{
			WebhookURL: discordSrv.URL,
		},
		Webhook: &WebhookConfig{
			URL: webhookSrv.URL,
		},
	}

	// Override Telegram URL by using the test server
	// We need to temporarily patch the telegram sender — use the test server URL
	// Instead, update the config to point BotToken to a path that routes to our server
	// Actually, we can't easily override the Telegram API URL. Let's test Telegram separately.
	cfg.Telegram = nil // skip Telegram in all-providers test

	event := testEvent()
	errs := NotifyAll(cfg, event)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Verify Slack body
	if len(slackBody) == 0 {
		t.Fatal("slack body is empty")
	}
	var slackPayload map[string]interface{}
	if err := json.Unmarshal(slackBody, &slackPayload); err != nil {
		t.Fatalf("failed to parse slack body: %v", err)
	}
	attachments, ok := slackPayload["attachments"].([]interface{})
	if !ok || len(attachments) == 0 {
		t.Fatal("slack payload missing attachments")
	}

	// Verify Discord body
	if len(discordBody) == 0 {
		t.Fatal("discord body is empty")
	}
	var discordPayload map[string]interface{}
	if err := json.Unmarshal(discordBody, &discordPayload); err != nil {
		t.Fatalf("failed to parse discord body: %v", err)
	}
	embeds, ok := discordPayload["embeds"].([]interface{})
	if !ok || len(embeds) == 0 {
		t.Fatal("discord payload missing embeds")
	}
	embed := embeds[0].(map[string]interface{})
	if !strings.Contains(embed["title"].(string), "container-down") {
		t.Errorf("discord embed title should contain rule name, got %q", embed["title"])
	}

	// Verify generic webhook body
	if len(webhookBody) == 0 {
		t.Fatal("webhook body is empty")
	}
	var webhookPayload WebhookPayload
	if err := json.Unmarshal(webhookBody, &webhookPayload); err != nil {
		t.Fatalf("failed to parse webhook body: %v", err)
	}
	if webhookPayload.Rule != "container-down" {
		t.Errorf("webhook rule = %q, want %q", webhookPayload.Rule, "container-down")
	}
}

func TestNotifyAll_NilConfig(t *testing.T) {
	errs := NotifyAll(nil, testEvent())
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestNotifyAll_PartialFailure(t *testing.T) {
	// Slack succeeds, Discord fails (returns 500)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer slackSrv.Close()

	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer discordSrv.Close()

	cfg := &NotifyConfig{
		Slack:   &SlackConfig{WebhookURL: slackSrv.URL},
		Discord: &DiscordConfig{WebhookURL: discordSrv.URL},
	}

	errs := NotifyAll(cfg, testEvent())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "discord") {
		t.Errorf("error should mention discord, got %q", errs[0])
	}
}

func TestSendTelegram_Format(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Extract base URL from test server to override telegram API
	cfg := &TelegramConfig{
		BotToken: "test",
		ChatID:   "99999",
	}

	// We need to call sendTelegram with a URL we control.
	// Since sendTelegram hardcodes the URL, we test via postJSON directly
	// and verify the format logic separately.
	event := testEvent()

	// Build the expected text
	icon := "🔴"
	expectedParts := []string{
		icon, "<b>container-down</b>", "triggered",
		"uptime-kuma is exited",
		"Action: restart",
		"Result: success",
		"2026-04-04 21:06:43",
	}

	// Use the test server as telegram endpoint
	text := buildTelegramText(event)
	for _, part := range expectedParts {
		if !strings.Contains(text, part) {
			t.Errorf("telegram text missing %q, got:\n%s", part, text)
		}
	}

	// Test actual HTTP call via postJSON
	payload := map[string]string{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	err := postJSON(srv.URL, payload)
	if err != nil {
		t.Fatalf("postJSON failed: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("server received empty body")
	}

	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	if parsed["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", parsed["parse_mode"])
	}
	if parsed["chat_id"] != "99999" {
		t.Errorf("chat_id = %q, want 99999", parsed["chat_id"])
	}
}

func TestSendSlack_Format(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &SlackConfig{WebhookURL: srv.URL}
	err := sendSlack(cfg, testEvent())
	if err != nil {
		t.Fatalf("sendSlack failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}

	attachments := payload["attachments"].([]interface{})
	att := attachments[0].(map[string]interface{})
	if att["color"] != "#36a64f" {
		t.Errorf("slack color = %q, want #36a64f for success", att["color"])
	}
}

func TestSendDiscord_Format(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	cfg := &DiscordConfig{WebhookURL: srv.URL}
	err := sendDiscord(cfg, testEvent())
	if err != nil {
		t.Fatalf("sendDiscord failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}

	embeds := payload["embeds"].([]interface{})
	embed := embeds[0].(map[string]interface{})
	if embed["description"] != "uptime-kuma is exited" {
		t.Errorf("discord description = %q, want %q", embed["description"], "uptime-kuma is exited")
	}

	fields := embed["fields"].([]interface{})
	if len(fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fields))
	}
}

func TestResolveNotifyConfig_LegacyWebhook(t *testing.T) {
	cfg := &AlertsConfig{
		Webhook: WebhookConfig{URL: "https://example.com/hook"},
	}
	nc := ResolveNotifyConfig(cfg)
	if nc == nil {
		t.Fatal("expected non-nil NotifyConfig")
	}
	if nc.Webhook == nil || nc.Webhook.URL != "https://example.com/hook" {
		t.Errorf("legacy webhook not mapped, got %+v", nc.Webhook)
	}
}

func TestResolveNotifyConfig_Empty(t *testing.T) {
	cfg := &AlertsConfig{}
	nc := ResolveNotifyConfig(cfg)
	if nc != nil {
		t.Errorf("expected nil for empty config, got %+v", nc)
	}
}
