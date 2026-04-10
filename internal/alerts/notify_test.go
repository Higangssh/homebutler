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
	var wp map[string]interface{}
	if err := json.Unmarshal(webhookBody, &wp); err != nil {
		t.Fatalf("failed to parse webhook body: %v", err)
	}
	if wp["name"] != "container-down" {
		t.Errorf("webhook name = %q, want %q", wp["name"], "container-down")
	}
	if wp["source"] != "alerts" {
		t.Errorf("webhook source = %q, want %q", wp["source"], "alerts")
	}
}

func TestNotifyAll_NilConfig(t *testing.T) {
	errs := NotifyAll(nil, testEvent())
	if errs != nil {
		t.Fatalf("expected nil, got %v", errs)
	}
}

func TestNotifyAll_PartialFailure(t *testing.T) {
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

func TestResolveNotifyConfig_LegacyWebhook(t *testing.T) {
	cfg := &AlertsConfig{
		Webhook: WebhookConfig{URL: "https://example.com/hook"},
	}
	nc := ResolveNotifyConfig(cfg)
	if nc == nil {
		t.Fatal("expected non-nil NotifyConfig")
		return
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
