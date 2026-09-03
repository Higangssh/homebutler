package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// findingFor returns the first finding whose field or message matches substr.
func findingFor(r *ValidationResult, substr string) (Finding, bool) {
	for _, f := range r.Findings {
		if strings.Contains(f.Field, substr) || strings.Contains(f.Message, substr) {
			return f, true
		}
	}
	return Finding{}, false
}

func requireFinding(t *testing.T, r *ValidationResult, substr, severity string) Finding {
	t.Helper()
	f, ok := findingFor(r, substr)
	if !ok {
		t.Fatalf("expected a finding matching %q, got %+v", substr, r.Findings)
	}
	if f.Severity != severity {
		t.Errorf("finding %q: severity = %q, want %q", substr, f.Severity, severity)
	}
	return f
}

// isolateResolution points config resolution at an empty directory so tests
// never pick up the developer's real ~/.config/homebutler/config.yaml.
func isolateResolution(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOMEBUTLER_CONFIG", "")
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // os.UserHomeDir on Windows
	t.Chdir(dir)
}

func TestValidateValidConfig(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: local
    host: 127.0.0.1
    local: true
  - name: pve1
    host: 192.168.1.10
    user: root
    port: 22
wake:
  - name: desktop
    mac: "AA:BB:CC:DD:EE:FF"
alerts:
  cpu: 90
  memory: 85
  disk: 90
notify:
  telegram:
    bot_token: "token"
    chat_id: "123"
`)

	r := Validate(path)

	if !r.Valid {
		t.Errorf("expected valid config, got findings: %+v", r.Findings)
	}
	if r.Errors() != 0 {
		t.Errorf("expected 0 errors, got %d: %+v", r.Errors(), r.Findings)
	}
	if r.Warnings() != 0 {
		t.Errorf("expected 0 warnings, got %d: %+v", r.Warnings(), r.Findings)
	}
	if !r.Exists {
		t.Error("expected Exists=true")
	}
	if r.Source != string(SourceFlag) {
		t.Errorf("source = %q, want %q", r.Source, SourceFlag)
	}
}

// A path the user named explicitly must be an error when it is missing.
// Load() returns defaults in this case, so commands succeed against a config
// that was never read — the failure this command exists to surface.
func TestValidateMissingExplicitFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")

	r := Validate(path)

	if r.Valid {
		t.Error("expected invalid result for a missing explicit path")
	}
	if r.Exists {
		t.Error("expected Exists=false")
	}
	requireFinding(t, r, "does not exist", SeverityError)

	// Load() must still tolerate it, so existing behavior is unchanged.
	if _, err := Load(path); err != nil {
		t.Errorf("Load should still fall back to defaults, got %v", err)
	}
}

func TestValidateNoConfigFound(t *testing.T) {
	isolateResolution(t)

	r := Validate("")

	if !r.Valid {
		t.Errorf("running on defaults is valid, got findings: %+v", r.Findings)
	}
	if r.Source != string(SourceNone) {
		t.Errorf("source = %q, want %q", r.Source, SourceNone)
	}
	requireFinding(t, r, "No config file found", SeverityWarning)
}

// The notifiy/notify typo: yaml.Unmarshal drops unknown keys without a word.
func TestValidateUnknownTopLevelKeySuggestsTheRealOne(t *testing.T) {
	path := writeConfig(t, `
notifiy:
  telegram:
    bot_token: "token"
    chat_id: "123"
`)

	r := Validate(path)

	f := requireFinding(t, r, "notifiy", SeverityWarning)
	if !strings.Contains(f.Hint, `"notify"`) {
		t.Errorf("expected a did-you-mean hint naming notify, got %q", f.Hint)
	}
	// An ignored key is not an error: the file still parses and runs.
	if !r.Valid {
		t.Error("unknown keys are warnings, not errors")
	}
}

func TestValidateUnknownNestedKey(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
    host: 192.168.1.10
    usr: root
`)

	r := Validate(path)

	f := requireFinding(t, r, "usr", SeverityWarning)
	if strings.Contains(f.Message, "config.ServerConfig") {
		t.Errorf("message should not leak Go type names: %q", f.Message)
	}
	if !strings.Contains(f.Message, "servers[] entry") {
		t.Errorf("message should locate the key in the config, got %q", f.Message)
	}
}

func TestValidateServerErrors(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
    port: 99999
    auth: publickey
  - name: pve1
    host: 192.168.1.11
  - host: 192.168.1.12
`)

	r := Validate(path)

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "servers[0].host", SeverityError)
	requireFinding(t, r, "servers[0].port", SeverityError)
	requireFinding(t, r, "servers[0].auth", SeverityError)
	requireFinding(t, r, "servers[1].name", SeverityError) // duplicate
	requireFinding(t, r, "servers[2].name", SeverityError) // missing
}

func TestValidateProxmoxErrors(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    port: 65536
    timeout: 0s
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: inline-token
    token_file: /does/not/exist
`)

	r := Validate(path)
	if r.Valid {
		t.Fatal("expected invalid config")
	}
	for _, field := range []string{
		"proxmox[0].host", "proxmox[0].token_id", "proxmox[0].port", "proxmox[0].timeout",
		"proxmox[0]", "proxmox[1].name", "Both Proxmox token and token_file", "proxmox[1].token_file",
	} {
		requireFinding(t, r, field, SeverityError)
	}
}

func TestValidateProxmoxGuestErrors(t *testing.T) {
	path := writeConfig(t, `proxmox:
  - name: pve
    host: pve.example
    token: token
    token_id: monitor@pve!readonly
    guests:
      - node: pve1
        type: docker
        vmid: 0
`)
	r := Validate(path)
	if r.Valid {
		t.Fatal("invalid guest was accepted")
	}
	for _, finding := range r.Findings {
		if finding.Field == "proxmox[0].guests[0]" {
			return
		}
	}
	t.Fatalf("findings = %+v, want invalid guest finding", r.Findings)
}

func TestValidateProxmoxActionCredentialErrors(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: read-token
    action_token_id: monitoring@pve!action
    action_token: inline-action
    action_token_file: /does/not/exist
  - name: pve2
    host: pve2.example
    token_id: monitoring@pve!readonly
    token: read-token
    action_token_id: monitoring@pve!action
    action_token: inline-action
`)

	r := Validate(path)
	if r.Valid {
		t.Fatal("expected invalid config")
	}
	for _, field := range []string{
		"proxmox[0]", "proxmox[0].action_token_file",
	} {
		requireFinding(t, r, field, SeverityError)
	}
	if _, found := findingFor(r, "proxmox[1]"); found {
		t.Errorf("action_token_id with action_token alone should be valid: %+v", r.Findings)
	}
}

func TestValidateProxmoxActionTokenIDRequired(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: read-token
    action_token: inline-action-without-id
`)

	r := Validate(path)
	requireFinding(t, r, "proxmox[0].action_token_id", SeverityError)
}

func TestValidateProxmoxNoActionCredentialIsValid(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: read-token
`)

	r := Validate(path)
	if !r.Valid {
		t.Errorf("expected valid config without an action credential, got findings: %+v", r.Findings)
	}
}

func TestValidateProxmoxActionCredentialSameIDWarns(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: read-token
    action_token_id: monitoring@pve!readonly
    action_token: different-token
`)

	r := Validate(path)
	if !r.Valid {
		t.Fatalf("a warning should not invalidate the config: %+v", r.Findings)
	}
	requireFinding(t, r, "proxmox[0].action_token_id", SeverityWarning)
}

func TestValidateProxmoxActionCredentialSameTokenWarns(t *testing.T) {
	path := writeConfig(t, `
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: shared-token
    action_token_id: monitoring@pve!action
    action_token: shared-token
`)

	r := Validate(path)
	requireFinding(t, r, "proxmox[0].action_token", SeverityWarning)
}

func TestValidateProxmoxActionCredentialSameFileWarns(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "pve.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	path := writeConfig(t, fmt.Sprintf(`
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token_file: %s
    action_token_id: monitoring@pve!action
    action_token_file: %s
`, tokenPath, tokenPath))

	r := Validate(path)
	requireFinding(t, r, "proxmox[0].action_token", SeverityWarning)
}

func TestValidateProxmoxActionTokenFilePermissionsWarns(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "pve.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	actionTokenPath := filepath.Join(dir, "pve.action.token")
	if err := os.WriteFile(actionTokenPath, []byte("token"), 0644); err != nil {
		t.Fatalf("write action token: %v", err)
	}

	path := writeConfig(t, fmt.Sprintf(`
proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token_file: %s
    action_token_id: monitoring@pve!action
    action_token_file: %s
`, tokenPath, actionTokenPath))

	r := Validate(path)
	requireFinding(t, r, "proxmox[0].action_token_file", SeverityWarning)
}

func TestValidateProxmoxTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "pve.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("proxmox:\n  - name: pve\n    host: pve.example\n    token_id: monitoring@pve!readonly\n    token_file: "+tokenPath+"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	r := Validate(path)
	if !r.Valid {
		t.Errorf("expected valid config, got findings: %+v", r.Findings)
	}
}

// local: true means "the machine homebutler runs on", so host is optional.
func TestValidateLocalServerNeedsNoHost(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: here
    local: true
`)

	r := Validate(path)

	if _, found := findingFor(r, "host"); found {
		t.Errorf("local servers should not require a host: %+v", r.Findings)
	}
}

// A password under key auth is silently ignored at connection time.
func TestValidatePasswordIgnoredUnderKeyAuth(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
    host: 192.168.1.10
    password: "hunter2"
`)

	r := Validate(path)

	f := requireFinding(t, r, "servers[0].password", SeverityWarning)
	if !strings.Contains(f.Message, "ignored") {
		t.Errorf("expected the message to say the password is ignored, got %q", f.Message)
	}
}

func TestValidateWakeMAC(t *testing.T) {
	path := writeConfig(t, `
wake:
  - name: nas
    mac: "ZZ:GG:not-a-mac"
  - name: desktop
`)

	r := Validate(path)

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "wake[0].mac", SeverityError)
	requireFinding(t, r, "wake[1].mac", SeverityError) // missing entirely
}

func TestValidateAlertThresholds(t *testing.T) {
	path := writeConfig(t, `
alerts:
  cpu: 150
  memory: -1
  disk: 0
`)

	r := Validate(path)

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "alerts.cpu", SeverityError)
	requireFinding(t, r, "alerts.memory", SeverityError)
	requireFinding(t, r, "alerts.disk", SeverityWarning) // 0 alerts on everything
}

func TestValidateWatchSettings(t *testing.T) {
	path := writeConfig(t, `
watch:
  notify:
    notify_on: sometimes
    cooldown: 5minutes
`)

	r := Validate(path)

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "watch.notify_on", SeverityError)
	requireFinding(t, r, "watch.cooldown", SeverityError)
}

// The #31 shape: watch notifications on, nothing configured to deliver them.
func TestValidateWatchEnabledWithoutProvider(t *testing.T) {
	path := writeConfig(t, `
watch:
  notify:
    enabled: true
    notify_on: flapping
`)

	r := Validate(path)

	f := requireFinding(t, r, "watch.enabled", SeverityWarning)
	if !strings.Contains(f.Message, "no notify provider") {
		t.Errorf("unexpected message: %q", f.Message)
	}
}

func TestValidateWatchEnabledWithProviderIsClean(t *testing.T) {
	path := writeConfig(t, `
notify:
  telegram:
    bot_token: "token"
    chat_id: "123"
watch:
  notify:
    enabled: true
`)

	r := Validate(path)

	if _, found := findingFor(r, "no notify provider"); found {
		t.Errorf("provider is configured, should not warn: %+v", r.Findings)
	}
}

func TestValidateIncompleteNotifyProvider(t *testing.T) {
	path := writeConfig(t, `
notify:
  telegram:
    bot_token: "token"
  slack:
    webhook_url: "not-a-url"
`)

	r := Validate(path)

	requireFinding(t, r, "notify.telegram.chat_id", SeverityWarning)
	requireFinding(t, r, "notify.slack.webhook_url", SeverityWarning)
	if !r.Valid {
		t.Error("an incomplete provider is a warning, not an error")
	}
}

func TestValidateInvalidYAML(t *testing.T) {
	path := writeConfig(t, "servers: [unclosed\n")

	r := Validate(path)

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "Invalid YAML", SeverityError)
	if len(r.Sections) != 0 {
		t.Error("sections should be omitted when the file does not parse")
	}
}

func TestValidateSectionsReportPresenceAndContent(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
    host: 192.168.1.10
alerts:
  cpu: 95
`)

	r := Validate(path)

	byName := map[string]Section{}
	for _, s := range r.Sections {
		byName[s.Name] = s
	}

	for _, name := range topLevelKeys {
		if _, ok := byName[name]; !ok {
			t.Errorf("section %q missing from result", name)
		}
	}

	if s := byName["servers"]; !s.Present || !strings.Contains(s.Summary, "pve1") {
		t.Errorf("servers section = %+v, want present and naming pve1", s)
	}
	if s := byName["alerts"]; !s.Present || !strings.Contains(s.Summary, "95") {
		t.Errorf("alerts section = %+v, want present with cpu 95", s)
	}
	// A section the file never set must read as unset, which is what explains
	// "my notify config is being ignored".
	if s := byName["notify"]; s.Present || s.Summary != "not set" {
		t.Errorf("notify section = %+v, want absent and 'not set'", s)
	}
	if s := byName["watch"]; s.Present || !strings.Contains(s.Summary, "defaults") {
		t.Errorf("watch section = %+v, want absent and marked as defaults", s)
	}
}

func TestValidateIsADirectory(t *testing.T) {
	r := Validate(t.TempDir())

	if r.Valid {
		t.Fatal("expected invalid config")
	}
	requireFinding(t, r, "is a directory", SeverityError)
}

func TestResolveWithSource(t *testing.T) {
	isolateResolution(t)

	if path, src := ResolveWithSource("/explicit/path.yaml"); src != SourceFlag || path != "/explicit/path.yaml" {
		t.Errorf("explicit: got (%q, %q), want (/explicit/path.yaml, %q)", path, src, SourceFlag)
	}

	t.Setenv("HOMEBUTLER_CONFIG", "/from/env.yaml")
	if path, src := ResolveWithSource(""); src != SourceEnv || path != "/from/env.yaml" {
		t.Errorf("env: got (%q, %q), want (/from/env.yaml, %q)", path, src, SourceEnv)
	}
	// The flag still wins over the environment.
	if _, src := ResolveWithSource("/explicit/path.yaml"); src != SourceFlag {
		t.Errorf("flag should outrank env, got %q", src)
	}

	t.Setenv("HOMEBUTLER_CONFIG", "")
	if _, src := ResolveWithSource(""); src != SourceNone {
		t.Errorf("nothing configured: got %q, want %q", src, SourceNone)
	}

	// Resolve stays a thin wrapper, so existing callers are unaffected.
	if got := Resolve("/explicit/path.yaml"); got != "/explicit/path.yaml" {
		t.Errorf("Resolve() = %q", got)
	}
}

func TestResolveWithSourceFindsCwdConfig(t *testing.T) {
	isolateResolution(t)

	if err := os.WriteFile("homebutler.yaml", []byte("alerts:\n  cpu: 80\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, src := ResolveWithSource("")
	if src != SourceCwd || path != "homebutler.yaml" {
		t.Errorf("got (%q, %q), want (homebutler.yaml, %q)", path, src, SourceCwd)
	}
}

func TestFormatValidationOutput(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
`)

	out := FormatValidation(Validate(path))

	for _, want := range []string{"Config validation", "Sections", "Findings", "servers[0].host", "Config is invalid"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatValidationValidConfig(t *testing.T) {
	path := writeConfig(t, `
servers:
  - name: pve1
    host: 192.168.1.10
`)

	out := FormatValidation(Validate(path))

	if !strings.Contains(out, "Config is valid") {
		t.Errorf("expected a valid verdict:\n%s", out)
	}
	if strings.Contains(out, "Findings") {
		t.Errorf("clean config should print no findings section:\n%s", out)
	}
}

// WatchRuntimeConfig has a custom unmarshaler, which bypasses KnownFields —
// so the watch subtree needs its own unknown-key check.
func TestValidateWatchUnknownKeys(t *testing.T) {
	path := writeConfig(t, `
watch:
  enabld: true
  notify:
    cooldwn: 5m
  flapping:
    short_windo: 10m
`)

	r := Validate(path)

	f := requireFinding(t, r, "watch.enabld", SeverityWarning)
	if !strings.Contains(f.Hint, `"enabled"`) {
		t.Errorf("expected a did-you-mean hint, got %q", f.Hint)
	}
	requireFinding(t, r, "watch.notify.cooldwn", SeverityWarning)
	requireFinding(t, r, "watch.flapping.short_windo", SeverityWarning)
}

func TestValidateWatchAcceptsBothForms(t *testing.T) {
	for name, body := range map[string]string{
		"flat": `
watch:
  enabled: true
  notify_on: flapping
  cooldown: 5m
`,
		"nested": `
watch:
  notify:
    enabled: true
    notify_on: flapping
    cooldown: 5m
`,
	} {
		t.Run(name, func(t *testing.T) {
			r := Validate(writeConfig(t, body))

			if _, found := findingFor(r, "not found in watch"); found {
				t.Errorf("%s form should be accepted: %+v", name, r.Findings)
			}
			for _, s := range r.Sections {
				if s.Name == "watch" && !strings.Contains(s.Summary, "enabled=true") {
					t.Errorf("%s form did not reach the runtime config: %q", name, s.Summary)
				}
			}
		})
	}
}

func TestValidateWatchBothFormsWarns(t *testing.T) {
	path := writeConfig(t, `
watch:
  enabled: true
  cooldown: 9m
  notify:
    enabled: false
`)

	r := Validate(path)

	f := requireFinding(t, r, "Both watch.notify and the flat form", SeverityWarning)
	if !strings.Contains(f.Message, "enabled") || !strings.Contains(f.Message, "cooldown") {
		t.Errorf("message should name the flat keys in play, got %q", f.Message)
	}
}

func TestValidateAcceptsWatchRetention(t *testing.T) {
	path := writeConfig(t, `
watch:
  retention:
    max_incidents: 50
`)

	r := Validate(path)

	// watch has a custom unmarshaler, so a new subtree that nobody added to the
	// key list gets reported as unknown even though it parses fine.
	if _, ok := findingFor(r, "retention"); ok {
		t.Errorf("watch.retention should be a known key, got %+v", r.Findings)
	}
	if !r.Valid {
		t.Error("a config setting only watch.retention should be valid")
	}
}

func TestValidateUnknownWatchRetentionKey(t *testing.T) {
	path := writeConfig(t, `
watch:
  retention:
    max_incident: 50
`)

	r := Validate(path)

	requireFinding(t, r, "max_incident", SeverityWarning)
}
