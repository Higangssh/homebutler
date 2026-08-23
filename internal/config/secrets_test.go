package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/notify"
)

// The bug this closes: hasSecrets only inspected servers[].password, so a
// config whose only credential was a notification token was never permission
// checked at all. watch notifications are set up far more often than SSH
// passwords, which made this the common case rather than the rare one.
func TestHasSecretsFindsNotifyCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{"telegram bot token", &Config{Notify: notify.ProviderConfig{
			Telegram: &notify.TelegramConfig{BotToken: "123:abc", ChatID: "42"},
		}}},
		{"slack webhook", &Config{Notify: notify.ProviderConfig{
			Slack: &notify.SlackConfig{WebhookURL: "https://hooks.slack.com/services/T/B/x"},
		}}},
		{"discord webhook", &Config{Notify: notify.ProviderConfig{
			Discord: &notify.DiscordConfig{WebhookURL: "https://discord.com/api/webhooks/1/x"},
		}}},
		{"generic webhook", &Config{Notify: notify.ProviderConfig{
			Webhook: &notify.WebhookConfig{URL: "https://example.com/hook?token=x"},
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !hasSecrets(tt.cfg) {
				t.Error("a config holding only this credential was treated as having no secrets, so its file permissions were never checked")
			}
		})
	}
}

// A configured provider with no credential in it is not a secret to protect.
func TestHasSecretsIgnoresEmptyCredentials(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{{Name: "a", Password: ""}},
		Notify: notify.ProviderConfig{
			Telegram: &notify.TelegramConfig{ChatID: "42"},
		},
	}
	if hasSecrets(cfg) {
		t.Error("an empty credential should not trigger the permission check")
	}
}

func TestHasSecretsHandlesNilAndEmpty(t *testing.T) {
	if hasSecrets(nil) {
		t.Error("a nil config has no secrets")
	}
	if hasSecrets(&Config{}) {
		t.Error("an empty config has no secrets")
	}
}

// Every field tagged as a secret must also be unserializable. Nothing
// serializes a *config.Config today, but that is a property of the current
// call sites rather than of the types, and report, doctor and the MCP tools
// serialize a growing amount of state. Tagging both ways makes a leak
// structurally impossible instead of merely absent.
func TestEverySecretFieldIsAlsoUnserializable(t *testing.T) {
	var walk func(t *testing.T, rt reflect.Type, path string, seen map[reflect.Type]bool)
	walk = func(t *testing.T, rt reflect.Type, path string, seen map[reflect.Type]bool) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true

		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			where := path + "." + f.Name
			if f.Tag.Get(secretTag) == "true" && f.Tag.Get("json") != "-" {
				t.Errorf(`%s is tagged secret but its json tag is %q; it must be "-" so it cannot be serialized`, where, f.Tag.Get("json"))
			}
			walk(t, f.Type, where, seen)
		}
	}
	walk(t, reflect.TypeOf(Config{}), "Config", map[reflect.Type]bool{})
}

// The end of the same argument: a config full of credentials, serialized, must
// not contain any of them.
func TestMarshallingAConfigLeaksNoCredentials(t *testing.T) {
	const (
		pw      = "sup3r-secret-password"
		token   = "9876543:AAH-telegram-bot-token"
		slack   = "https://hooks.slack.com/services/T00/B00/leaked"
		discord = "https://discord.com/api/webhooks/1/leaked"
		hook    = "https://example.com/hook?token=leaked"
	)

	cfg := &Config{
		Servers: []ServerConfig{{Name: "pve1", Host: "10.0.0.1", Password: pw}},
		Notify: notify.ProviderConfig{
			Telegram: &notify.TelegramConfig{BotToken: token, ChatID: "42"},
			Slack:    &notify.SlackConfig{WebhookURL: slack},
			Discord:  &notify.DiscordConfig{WebhookURL: discord},
			Webhook:  &notify.WebhookConfig{URL: hook},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, secret := range []string{pw, token, slack, discord, hook} {
		if strings.Contains(string(data), secret) {
			t.Errorf("serialized config contains %q\n%s", secret, data)
		}
	}
}

// The end of the whole change, through Load rather than through hasSecrets: a
// config whose only credential is a notification token must be refused when
// its permissions are open. Before this it loaded without complaint, and the
// guard that exists to prevent exactly that never ran.
func TestLoadRefusesAnOpenFileHoldingOnlyANotifyCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Load only enforces permissions on non-Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "notify:\n  telegram:\n    bot_token: \"123:abc\"\n    chat_id: \"999\"\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a world-readable config holding a bot token should be refused")
	} else if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("the refusal should say how to fix it, got: %v", err)
	}

	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("the same config at 0600 should load: %v", err)
	}
}
