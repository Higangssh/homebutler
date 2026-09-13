package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deadAddress is a local address nothing is listening on: a test server, shut
// down. Sending there fails with connection refused every time, with no DNS
// and no network — an unresolvable hostname would depend on what the resolver
// does with it, and an ISP that answers NXDOMAIN with an advertising page
// would make the request succeed and the test pass for the wrong reason.
func deadAddress(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()
	return addr
}

// The secret is in the path or the query of every provider homebutler has, and
// postJSON's error reaches the log as "→ notify error: ..." from the watcher —
// journald or the launchd log under an installed service, and every log someone
// pastes when asking for help.
func TestPostJSONKeepsTheSecretOutOfTheError(t *testing.T) {
	base := deadAddress(t)

	cases := []struct {
		name   string
		path   string
		secret string
	}{
		{
			name:   "telegram bot token in the path",
			path:   "/bot7654321:AAH-not-a-real-token/sendMessage",
			secret: "7654321:AAH-not-a-real-token",
		},
		{
			name:   "slack webhook, where the path is the whole secret",
			path:   "/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
			secret: "XXXXXXXXXXXXXXXXXXXXXXXX",
		},
		{
			name:   "discord webhook",
			path:   "/api/webhooks/123456789/aVerySecretWebhookToken",
			secret: "aVerySecretWebhookToken",
		},
		{
			name:   "generic webhook carrying its token in the query",
			path:   "/notify?token=aSecretQueryToken",
			secret: "aSecretQueryToken",
		},
		{
			name:   "an address with userinfo",
			path:   "/hook",
			secret: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := postJSON(base+tc.path, map[string]string{"text": "hello"})
			if err == nil {
				t.Fatal("expected the request to fail against a closed address")
			}
			if tc.secret != "" && strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("the error carried the secret: %v", err)
			}
			if strings.Contains(err.Error(), tc.path) {
				t.Fatalf("the error carried the path: %v", err)
			}
		})
	}
}

// #176 asks for the host to survive: with more than one channel configured,
// "which service is unreachable" has to stay answerable from the log.
func TestPostJSONStillNamesTheHost(t *testing.T) {
	base := deadAddress(t)

	err := postJSON(base+"/bot7654321:AAH-not-a-real-token/sendMessage", map[string]string{"text": "hello"})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), base) {
		t.Fatalf("expected the scheme and host to survive, got %v", err)
	}
}

// A fix that trades a leak for an error nobody can act on is not one.
func TestPostJSONKeepsTheCause(t *testing.T) {
	err := postJSON(deadAddress(t)+"/hook/secret-value", map[string]string{"text": "hello"})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the reason the request failed, got %v", err)
	}
}

// A server that answers with a refusal is a different path, and the status is
// all it has ever reported.
func TestPostJSONReportsStatusWithoutTheSecret(t *testing.T) {
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

func TestSafeAddressDropsEverythingButSchemeAndHost(t *testing.T) {
	cases := map[string]string{
		"https://api.telegram.org/bot123:secret/sendMessage": "https://api.telegram.org",
		"https://hooks.slack.com/services/T0/B0/XXXX":        "https://hooks.slack.com",
		"https://example.com/notify?token=secret":            "https://example.com",
		"https://user:password@example.com:8443/hook":        "https://example.com:8443",
		"not a url at all": "the configured endpoint",
	}
	for raw, want := range cases {
		if got := safeAddress(raw); got != want {
			t.Errorf("safeAddress(%q) = %q, want %q", raw, got, want)
		}
	}
}
