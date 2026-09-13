package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The address is the credential: Telegram carries the bot token in the path,
// and a Slack, Discord or webhook URL is the secret in full. postJSON's error
// reaches the log as "→ notify error: ..." from the watcher, so anything it
// carries ends up in journald or the launchd log and in every log someone
// pastes when asking for help.
func TestPostJSONDoesNotPutTheAddressInTheError(t *testing.T) {
	const token = "7654321:AAH-not-a-real-token"

	cases := []struct {
		name     string
		endpoint string
		secret   string
	}{
		{
			name:     "telegram bot token in the path",
			endpoint: "https://api.telegram.invalid/bot" + token + "/sendMessage",
			secret:   token,
		},
		{
			name:     "slack webhook, where the url is the whole secret",
			endpoint: "https://hooks.slack.invalid/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
			secret:   "XXXXXXXXXXXXXXXXXXXXXXXX",
		},
		{
			name:     "discord webhook",
			endpoint: "https://discord.invalid/api/webhooks/123456789/aVerySecretWebhookToken",
			secret:   "aVerySecretWebhookToken",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := postJSON(tc.endpoint, map[string]string{"text": "hello"})
			if err == nil {
				t.Fatal("expected the request to fail against an invalid host")
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("the error carried the secret: %v", err)
			}
			if strings.Contains(err.Error(), tc.endpoint) {
				t.Fatalf("the error carried the address: %v", err)
			}
		})
	}
}

// The cause still has to survive, or the fix trades a leak for an error nobody
// can act on.
func TestPostJSONKeepsTheCause(t *testing.T) {
	err := postJSON("https://nothing.invalid/hook/secret-value", map[string]string{"text": "hello"})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("expected the reason the request failed, got %v", err)
	}
}

// A server that answers with a refusal is a different path, and the status is
// all it has ever reported.
func TestPostJSONReportsStatusWithoutTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := postJSON(srv.URL+"/bot-secret-token/sendMessage", map[string]string{"text": "hello"})
	if err == nil {
		t.Fatal("expected a failure for 401")
	}
	if strings.Contains(err.Error(), "bot-secret-token") {
		t.Fatalf("the error carried the secret: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected the status, got %v", err)
	}
}
