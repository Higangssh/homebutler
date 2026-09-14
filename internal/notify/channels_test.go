package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func event(status string) Event {
	return Event{
		Source:  "watch",
		Name:    "plex",
		Status:  status,
		Details: "exited (137)",
		Action:  "restart",
		Result:  "success",
		Time:    time.Date(2026, 9, 14, 3, 14, 22, 0, time.UTC),
	}
}

// A channel added to ProviderConfig and not to the table would be configured
// by a user, listed by nothing, and never sent to. That is the failure #177 is
// about, one provider later.
func TestEveryProviderFieldHasATableEntry(t *testing.T) {
	fields := reflect.TypeOf(ProviderConfig{}).NumField()
	if fields != len(providers) {
		t.Fatalf("ProviderConfig has %d channels and the table has %d; add the new one to providers", fields, len(providers))
	}

	seen := map[Channel]bool{}
	for _, p := range providers {
		if seen[p.channel] {
			t.Fatalf("two entries for %q", p.channel)
		}
		seen[p.channel] = true
	}
}

// The bug this replaces: cmd/alerts.go decided which provider failed by looking
// for the channel's name in the message, so a Discord failure that quoted a URL
// containing "webhook" was reported as a webhook failure as well.
//
// #176 removed the path from these messages, which took away the commonest way
// that happened. The host survives, so a Discord webhook served from a host with
// the word in it still produces the collision — and so would adding ntfy and
// gotify to a substring match. The channel is read from the error's type now,
// and this checks that it holds even when the text names another channel.
func TestOneFailureIsReportedAgainstOneChannel(t *testing.T) {
	accepted := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer accepted.Close()

	cfg := &ProviderConfig{
		// Nothing listens on port 1, and the host carries the word that used
		// to be read as a second failure.
		Discord: &DiscordConfig{WebhookURL: "http://webhook.example.invalid:1/api/webhooks/123/abc"},
		Webhook: &WebhookConfig{URL: accepted.URL + "/hook"},
	}

	errs := SendAll(cfg, event("triggered"))
	if len(errs) != 1 {
		t.Fatalf("expected one failure, got %d: %v", len(errs), errs)
	}

	channel, ok := FailedChannel(errs[0])
	if !ok {
		t.Fatalf("the error does not name a channel: %v", errs[0])
	}
	if channel != ChannelDiscord {
		t.Fatalf("the failure was attributed to %q", channel)
	}

	// The text still contains the other channel's name, which is exactly the
	// condition under which reading it would give the wrong answer.
	if !strings.Contains(errs[0].Error(), string(ChannelWebhook)) {
		t.Skip("the message no longer contains the other channel's name; the type is what this test is about")
	}
	if strings.Contains(errs[0].Error(), string(ChannelWebhook)) && channel == ChannelWebhook {
		t.Fatal("attribution followed the text rather than the type")
	}
}

func TestNtfySendsTheMessageAsTheBodyAndTheRestAsHeaders(t *testing.T) {
	type received struct {
		path, title, priority, auth, body string
	}
	var got received

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = received{
			path:     r.URL.Path,
			title:    r.Header.Get("Title"),
			priority: r.Header.Get("Priority"),
			auth:     r.Header.Get("Authorization"),
			body:     string(body),
		}
	}))
	defer srv.Close()

	cfg := &NtfyConfig{URL: srv.URL, Topic: "homelab", Token: "tk_secret"}
	if err := sendNtfy(cfg, event("triggered")); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got.path != "/homelab" {
		t.Errorf("published to %q, want the topic", got.path)
	}
	if !strings.Contains(got.title, "plex") {
		t.Errorf("title %q does not name what happened", got.title)
	}
	if got.priority != "4" {
		t.Errorf("a triggered event arrived at priority %q, want ntfy's high", got.priority)
	}
	if got.auth != "Bearer tk_secret" {
		t.Errorf("token went somewhere other than the Authorization header: %q", got.auth)
	}
	if !strings.Contains(got.body, "exited (137)") {
		t.Errorf("body %q does not carry the detail", got.body)
	}
}

// A public topic takes no token, and sending an empty Authorization header
// would be a credential-shaped thing that is not one.
func TestNtfyWithoutATokenSendsNoAuthorization(t *testing.T) {
	var auth string
	var hadHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, hadHeader = r.Header["Authorization"]
	}))
	defer srv.Close()

	if err := sendNtfy(&NtfyConfig{URL: srv.URL, Topic: "open"}, event("resolved")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if hadHeader || auth != "" {
		t.Fatalf("sent an Authorization header with no token: %q", auth)
	}
}

func TestGotifySendsTitleMessagePriorityWithTheTokenInAHeader(t *testing.T) {
	var payload map[string]any
	var key, path string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		key = r.Header.Get("X-Gotify-Key")
		json.NewDecoder(r.Body).Decode(&payload)
	}))
	defer srv.Close()

	if err := sendGotify(&GotifyConfig{URL: srv.URL, Token: "AppToken"}, event("triggered")); err != nil {
		t.Fatalf("send: %v", err)
	}

	if path != "/message" {
		t.Errorf("posted to %q", path)
	}
	if key != "AppToken" {
		t.Errorf("token went somewhere other than X-Gotify-Key: %q", key)
	}
	if fmt.Sprint(payload["priority"]) != "8" {
		t.Errorf("a triggered event arrived at priority %v, want Gotify's high", payload["priority"])
	}
	if !strings.Contains(fmt.Sprint(payload["title"]), "plex") {
		t.Errorf("title %v does not name what happened", payload["title"])
	}
}

// The mapping is derived from the event and shared, so the two servers agree
// about what "this one matters" means.
func TestOnlyATriggerIsHighPriority(t *testing.T) {
	if priorityFor(event("triggered")) != PriorityHigh {
		t.Error("a trigger should be high")
	}
	for _, status := range []string{"resolved", "recovered", "test", ""} {
		if priorityFor(event(status)) != PriorityDefault {
			t.Errorf("%q should be default priority", status)
		}
	}
	if ntfyPriority(PriorityDefault) == ntfyPriority(PriorityHigh) {
		t.Error("ntfy's two levels are the same value")
	}
	if gotifyPriority(PriorityDefault) == gotifyPriority(PriorityHigh) {
		t.Error("gotify's two levels are the same value")
	}
}

func TestEnabledNeedsWhatEachServerActuallyRequires(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *ProviderConfig
		enabled []Channel
	}{
		{"ntfy without a topic addresses nothing", &ProviderConfig{Ntfy: &NtfyConfig{URL: "https://ntfy.sh"}}, []Channel{}},
		{"ntfy without a token is fine on a public topic", &ProviderConfig{Ntfy: &NtfyConfig{URL: "https://ntfy.sh", Topic: "t"}}, []Channel{ChannelNtfy}},
		{"gotify without a token cannot publish", &ProviderConfig{Gotify: &GotifyConfig{URL: "https://g"}}, []Channel{}},
		{"gotify with both", &ProviderConfig{Gotify: &GotifyConfig{URL: "https://g", Token: "t"}}, []Channel{ChannelGotify}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.EnabledChannels()
			if len(got) != len(tc.enabled) {
				t.Fatalf("enabled = %v, want %v", got, tc.enabled)
			}
			for i := range got {
				if got[i] != tc.enabled[i] {
					t.Fatalf("enabled = %v, want %v", got, tc.enabled)
				}
			}
		})
	}
}

// A block that is present and incomplete is what config validate warns about,
// so the two questions have to stay distinguishable.
func TestPresentIsNotTheSameAsEnabled(t *testing.T) {
	cfg := &ProviderConfig{Gotify: &GotifyConfig{URL: "https://g"}}
	if cfg.IsEmpty() {
		t.Error("a present but incomplete block is not empty")
	}
	if len(cfg.EnabledChannels()) != 0 {
		t.Error("an incomplete block is not enabled")
	}
}
