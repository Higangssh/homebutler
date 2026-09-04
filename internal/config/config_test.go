package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/proxmox"
	"gopkg.in/yaml.v3"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("nonexistent.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Alerts.CPU != 90 {
		t.Errorf("expected CPU threshold 90, got %f", cfg.Alerts.CPU)
	}
	if cfg.Alerts.Memory != 85 {
		t.Errorf("expected Memory threshold 85, got %f", cfg.Alerts.Memory)
	}
	if cfg.Alerts.Disk != 90 {
		t.Errorf("expected Disk threshold 90, got %f", cfg.Alerts.Disk)
	}
	if !cfg.Watch.Notify.OnFlapping {
		t.Error("expected default watch notify on_flapping=true")
	}
	if cfg.Watch.Flapping.ShortThreshold != 3 {
		t.Errorf("expected default short flapping threshold 3, got %d", cfg.Watch.Flapping.ShortThreshold)
	}

}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := `
alerts:
  cpu: 80
  memory: 70
  disk: 95
notify:
  telegram:
    bot_token: "123:abc"
    chat_id: "999"
watch:
  notify:
    enabled: true
    notify_on: incident
    cooldown: 10m
  flapping:
    short_window: 5m
    short_threshold: 4
    long_window: 12h
    long_threshold: 6
wake:
  - name: nas
    mac: "AA:BB:CC:DD:EE:FF"
    ip: "192.168.1.255"
`
	// 0600 because this fixture holds a bot token. Written 0644 it used to
	// load without complaint, only because hasSecrets could not see notify
	// credentials. Load now refuses that, which is what
	// TestLoadRefusesAnOpenFileHoldingOnlyANotifyCredential covers.
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Alerts.CPU != 80 {
		t.Errorf("expected CPU threshold 80, got %f", cfg.Alerts.CPU)
	}
	if cfg.Alerts.Memory != 70 {
		t.Errorf("expected Memory threshold 70, got %f", cfg.Alerts.Memory)
	}
	if cfg.Alerts.Disk != 95 {
		t.Errorf("expected Disk threshold 95, got %f", cfg.Alerts.Disk)
	}
	if len(cfg.Wake) != 1 {
		t.Fatalf("expected 1 wake target, got %d", len(cfg.Wake))
	}
	if cfg.Wake[0].Name != "nas" {
		t.Errorf("expected wake target 'nas', got %q", cfg.Wake[0].Name)
	}
	if cfg.Notify.Telegram == nil || cfg.Notify.Telegram.ChatID != "999" {
		t.Fatalf("expected telegram notify config to load, got %+v", cfg.Notify.Telegram)
	}
	if !cfg.Watch.Notify.Enabled || !cfg.Watch.Notify.OnIncident || cfg.Watch.Notify.OnFlapping || cfg.Watch.Notify.NotifyOn != "incident" {
		t.Fatalf("unexpected watch notify config: %+v", cfg.Watch.Notify)
	}
	if cfg.Watch.Flapping.ShortThreshold != 4 || cfg.Watch.Flapping.LongThreshold != 6 {
		t.Fatalf("unexpected watch flapping config: %+v", cfg.Watch.Flapping)
	}
}

func TestFindWakeTarget(t *testing.T) {
	cfg := &Config{
		Wake: []WakeTarget{
			{Name: "nas", MAC: "AA:BB:CC:DD:EE:FF"},
			{Name: "desktop", MAC: "11:22:33:44:55:66"},
		},
	}

	target := cfg.FindWakeTarget("nas")
	if target == nil {
		t.Fatal("expected to find 'nas'")
	} else if target.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("expected MAC AA:BB:CC:DD:EE:FF, got %s", target.MAC)
	}

	target = cfg.FindWakeTarget("nonexistent")
	if target != nil {
		t.Error("expected nil for nonexistent target")
	}
}

func TestResolveExplicit(t *testing.T) {
	result := Resolve("/some/explicit/path.yaml")
	if result != "/some/explicit/path.yaml" {
		t.Errorf("expected explicit path, got %q", result)
	}
}

func TestResolveEnvVar(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONFIG", "/env/config.yaml")
	result := Resolve("")
	if result != "/env/config.yaml" {
		t.Errorf("expected env path, got %q", result)
	}
}

func TestResolveXDG(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	dir := filepath.Join(home, ".config", "homebutler")
	os.MkdirAll(dir, 0755)
	xdg := filepath.Join(dir, "config.yaml")

	// Only test if XDG config doesn't already exist (don't mess with real config)
	if _, err := os.Stat(xdg); err == nil {
		t.Setenv("HOMEBUTLER_CONFIG", "")
		result := Resolve("")
		if result != xdg {
			t.Errorf("expected XDG path %s, got %q", xdg, result)
		}
	}
}

func TestResolveNone(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONFIG", "")
	// Run from temp dir where no homebutler.yaml exists
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(t.TempDir())

	result := Resolve("")
	// If XDG config exists on this machine, it will be found — that's OK
	home, _ := os.UserHomeDir()
	xdg := filepath.Join(home, ".config", "homebutler", "config.yaml")
	if _, err := os.Stat(xdg); err == nil {
		if result != xdg {
			t.Errorf("expected XDG path %s, got %q", xdg, result)
		}
	} else {
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	}
}

func TestLoadInvalidYaml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid yaml")
	}
}

func TestFindServer(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "alpha", Host: "10.0.0.1", Local: true},
			{Name: "beta", Host: "10.0.0.2"},
		},
	}

	tests := []struct {
		name     string
		query    string
		wantNil  bool
		wantHost string
	}{
		{"found", "alpha", false, "10.0.0.1"},
		{"found-second", "beta", false, "10.0.0.2"},
		{"not-found", "gamma", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.FindServer(tt.query)
			if tt.wantNil && got != nil {
				t.Errorf("FindServer(%q) = %v, want nil", tt.query, got)
			}
			if !tt.wantNil {
				if got == nil {
					t.Fatalf("FindServer(%q) = nil, want non-nil", tt.query)
				} else if got.Host != tt.wantHost {
					t.Errorf("FindServer(%q).Host = %q, want %q", tt.query, got.Host, tt.wantHost)
				}
			}
		})
	}

	// Empty servers list
	emptyCfg := &Config{}
	if got := emptyCfg.FindServer("any"); got != nil {
		t.Error("FindServer on empty config should return nil")
	}
}

func TestProxmoxConfigTokenValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	path := filepath.Join(dir, "pve.token")
	if err := os.WriteFile(path, []byte("  from-file\n"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	fromFile := ProxmoxConfig{TokenFile: "~/pve.token"}
	if got := fromFile.TokenFilePath(); got != path {
		t.Errorf("TokenFilePath() = %q, want %q", got, path)
	}
	if got, err := fromFile.TokenValue(); err != nil || got != "from-file" {
		t.Errorf("TokenValue() = (%q, %v), want (from-file, nil)", got, err)
	}

	inline := ProxmoxConfig{Token: "inline-token"}
	if got, err := inline.TokenValue(); err != nil || got != "inline-token" {
		t.Errorf("TokenValue() = (%q, %v), want (inline-token, nil)", got, err)
	}
	if _, err := (ProxmoxConfig{}).TokenValue(); err == nil {
		t.Error("TokenValue() should fail without a token source")
	}

	if _, err := (ProxmoxConfig{TokenFile: filepath.Join(dir, "missing"), Token: "inline-secret"}).TokenValue(); err == nil {
		t.Error("TokenValue() should fail for a missing token file")
	} else if proxmox.Classify(err) != proxmox.FailureAuthentication || strings.Contains(err.Error(), "inline-secret") {
		t.Errorf("TokenValue() error = %v, want authentication error without inline token", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty"), []byte(" \n"), 0600); err != nil {
		t.Fatalf("write empty token: %v", err)
	}
	if _, err := (ProxmoxConfig{TokenFile: filepath.Join(dir, "empty"), Token: "inline-secret"}).TokenValue(); err == nil {
		t.Error("TokenValue() should fail for an empty token file")
	} else if proxmox.Classify(err) != proxmox.FailureAuthentication || strings.Contains(err.Error(), "inline-secret") {
		t.Errorf("TokenValue() error = %v, want authentication error without inline token", err)
	}
}

func TestProxmoxConfigActionTokenValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	path := filepath.Join(dir, "pve-action.token")
	if err := os.WriteFile(path, []byte("  from-file\n"), 0600); err != nil {
		t.Fatalf("write action token: %v", err)
	}

	fromFile := ProxmoxConfig{ActionTokenFile: "~/pve-action.token"}
	if got := fromFile.ActionTokenFilePath(); got != path {
		t.Errorf("ActionTokenFilePath() = %q, want %q", got, path)
	}
	if got, err := fromFile.ActionTokenValue(); err != nil || got != "from-file" {
		t.Errorf("ActionTokenValue() = (%q, %v), want (from-file, nil)", got, err)
	}

	inline := ProxmoxConfig{ActionToken: "inline-action-token"}
	if got, err := inline.ActionTokenValue(); err != nil || got != "inline-action-token" {
		t.Errorf("ActionTokenValue() = (%q, %v), want (inline-action-token, nil)", got, err)
	}
	if _, err := (ProxmoxConfig{}).ActionTokenValue(); err == nil {
		t.Error("ActionTokenValue() should fail without an action token source")
	}

	if _, err := (ProxmoxConfig{ActionTokenFile: filepath.Join(dir, "missing"), ActionToken: "inline-secret"}).ActionTokenValue(); err == nil {
		t.Error("ActionTokenValue() should fail for a missing token file")
	} else if proxmox.Classify(err) != proxmox.FailureAuthentication || strings.Contains(err.Error(), "inline-secret") {
		t.Errorf("ActionTokenValue() error = %v, want authentication error without inline token", err)
	}
}

func TestProxmoxConfigHasActionCredential(t *testing.T) {
	cases := []struct {
		name string
		cfg  ProxmoxConfig
		want bool
	}{
		{"none configured", ProxmoxConfig{}, false},
		{"read token only", ProxmoxConfig{Token: "read"}, false},
		{"action id without token", ProxmoxConfig{ActionTokenID: "monitoring@pve!action"}, false},
		{"action id with inline token", ProxmoxConfig{ActionTokenID: "monitoring@pve!action", ActionToken: "secret"}, true},
		{"action id with token file", ProxmoxConfig{ActionTokenID: "monitoring@pve!action", ActionTokenFile: "/etc/pve-action.token"}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.HasActionCredential(); got != tc.want {
			t.Errorf("%s: HasActionCredential() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestProxmoxConfigResolveCredential(t *testing.T) {
	readOnly := ProxmoxConfig{Name: "pve", TokenID: "monitoring@pve!readonly", Token: "read-token"}
	if tokenID, token, err := readOnly.ResolveCredential(false); err != nil || tokenID != "monitoring@pve!readonly" || token != "read-token" {
		t.Errorf("ResolveCredential(false) = (%q, %q, %v), want (monitoring@pve!readonly, read-token, nil)", tokenID, token, err)
	}
	if _, _, err := readOnly.ResolveCredential(true); err == nil || !strings.Contains(err.Error(), "no action credential configured") {
		t.Errorf("ResolveCredential(true) without an action credential = %v, want a 'no action credential configured' error", err)
	} else if class := proxmox.Classify(err); class != proxmox.FailureAuthentication {
		t.Errorf("Classify(ResolveCredential(true) error) = %q, want %q", class, proxmox.FailureAuthentication)
	}

	withAction := readOnly
	withAction.ActionTokenID = "monitoring@pve!action"
	withAction.ActionToken = "action-token"
	if tokenID, token, err := withAction.ResolveCredential(true); err != nil || tokenID != "monitoring@pve!action" || token != "action-token" {
		t.Errorf("ResolveCredential(true) = (%q, %q, %v), want (monitoring@pve!action, action-token, nil)", tokenID, token, err)
	}

	if _, _, err := (ProxmoxConfig{Name: "pve"}).ResolveCredential(false); err == nil {
		t.Error("ResolveCredential(false) should fail without a read token source")
	}
}

func TestProxmoxConfigDefaults(t *testing.T) {
	p := ProxmoxConfig{}
	if got := p.APIPort(); got != 8006 {
		t.Errorf("APIPort() = %d, want 8006", got)
	}
	if got := p.TimeoutDuration(); got.String() != "10s" {
		t.Errorf("TimeoutDuration() = %s, want 10s", got)
	}
}

func TestLoadProxmoxConfig(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "pve.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	content := "proxmox:\n  - name: pve\n    host: pve.example\n    port: 8007\n    token_id: monitoring@pve!readonly\n    token_file: " + tokenPath + "\n    fingerprint: AB:CD\n    ca_file: /etc/pve/ca.pem\n    insecure: true\n    timeout: 15s\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Proxmox) != 1 {
		t.Fatalf("Proxmox entries = %d, want 1", len(cfg.Proxmox))
	}
	p := cfg.Proxmox[0]
	if p.Name != "pve" || p.Host != "pve.example" || p.APIPort() != 8007 || p.TokenID != "monitoring@pve!readonly" || p.TokenFile != tokenPath || p.Fingerprint != "AB:CD" || p.CAFile != "/etc/pve/ca.pem" || !p.Insecure || p.TimeoutDuration().String() != "15s" {
		t.Errorf("unexpected Proxmox config: %+v", p)
	}
	if got := cfg.FindProxmox("pve"); got == nil || got.Host != "pve.example" {
		t.Errorf("FindProxmox(pve) = %+v", got)
	}
}

func TestLoadProxmoxConfigGuests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `proxmox:
  - name: pve
    host: pve.example
    token_id: monitoring@pve!readonly
    token: inline
    guests:
      - node: pve1
        type: qemu
        vmid: 100
      - node: pve1
        type: lxc
        vmid: 101
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	guests := cfg.Proxmox[0].Guests
	if len(guests) != 2 {
		t.Fatalf("Guests = %d, want 2", len(guests))
	}
	if guests[0].Node != "pve1" || guests[0].Type != "qemu" || guests[0].VMID != 100 {
		t.Errorf("guests[0] = %+v", guests[0])
	}
	if guests[1].Type != "lxc" || guests[1].VMID != 101 {
		t.Errorf("guests[1] = %+v", guests[1])
	}
}

func TestResolveBackupDir(t *testing.T) {
	tests := []struct {
		name      string
		backupDir string
		wantExact bool
		want      string
	}{
		{"explicit", "/custom/backups", true, "/custom/backups"},
		{"default", "", false, ".homebutler/backups"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{BackupDir: tt.backupDir}
			got := cfg.ResolveBackupDir()
			if tt.wantExact {
				if got != tt.want {
					t.Errorf("ResolveBackupDir() = %q, want %q", got, tt.want)
				}
			} else {
				if !filepath.IsAbs(got) && got != ".homebutler/backups" {
					t.Errorf("ResolveBackupDir() = %q, expected absolute path or fallback", got)
				}
				if !contains(got, tt.want) {
					t.Errorf("ResolveBackupDir() = %q, should contain %q", got, tt.want)
				}
			}
		})
	}
}

func TestSSHPort(t *testing.T) {
	tests := []struct {
		name string
		port int
		want int
	}{
		{"default", 0, 22},
		{"custom", 2222, 2222},
		{"negative", -1, 22},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerConfig{Port: tt.port}
			if got := s.SSHPort(); got != tt.want {
				t.Errorf("SSHPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSSHUser(t *testing.T) {
	tests := []struct {
		name string
		user string
		want string
	}{
		{"default", "", "root"},
		{"custom", "deploy", "deploy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerConfig{User: tt.user}
			if got := s.SSHUser(); got != tt.want {
				t.Errorf("SSHUser() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUseKeyAuth(t *testing.T) {
	tests := []struct {
		name     string
		authMode string
		want     bool
	}{
		{"default-empty", "", true},
		{"key", "key", true},
		{"password", "password", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerConfig{AuthMode: tt.authMode}
			if got := s.UseKeyAuth(); got != tt.want {
				t.Errorf("UseKeyAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSSHBinPath(t *testing.T) {
	tests := []struct {
		name    string
		binPath string
		want    string
	}{
		{"default", "", "homebutler"},
		{"custom", "/opt/homebutler", "/opt/homebutler"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerConfig{BinPath: tt.binPath}
			if got := s.SSHBinPath(); got != tt.want {
				t.Errorf("SSHBinPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasSecrets(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"no-servers", &Config{}, false},
		{"no-passwords", &Config{Servers: []ServerConfig{{Name: "a"}}}, false},
		{"with-password", &Config{Servers: []ServerConfig{{Name: "a", Password: "secret"}}}, true},
		{"mixed", &Config{Servers: []ServerConfig{{Name: "a"}, {Name: "b", Password: "secret"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasSecrets(tt.cfg); got != tt.want {
				t.Errorf("hasSecrets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadSetsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("alerts:\n  cpu: 80\n"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Path != path {
		t.Errorf("cfg.Path = %q, want %q", cfg.Path, path)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if cfg.Alerts.CPU != 90 {
		t.Errorf("CPU = %v, want 90", cfg.Alerts.CPU)
	}
}

func TestLoadWithServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
servers:
  - name: myserver
    host: 10.0.0.1
    user: admin
    port: 2222
    auth: password
    password: secret
    bin: /opt/homebutler
  - name: local
    host: 127.0.0.1
    local: true
`
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "myserver" {
		t.Errorf("Servers[0].Name = %q, want %q", cfg.Servers[0].Name, "myserver")
	}
	if cfg.Servers[0].Port != 2222 {
		t.Errorf("Servers[0].Port = %d, want 2222", cfg.Servers[0].Port)
	}
	if !cfg.Servers[1].Local {
		t.Error("Servers[1].Local should be true")
	}
}

func contains(s, substr string) bool {
	return filepath.Base(s) != "" && len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// The README documented the flat form long before watch.notify existed, so
// configs written from it must keep working. See WatchRuntimeConfig.UnmarshalYAML.
func TestLoadFlatWatchForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flat.yaml")
	content := `
watch:
  enabled: true
  notify_on: incident
  cooldown: 9m
  flapping:
    short_threshold: 7
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !cfg.Watch.Notify.Enabled {
		t.Error("watch.enabled should reach Notify.Enabled")
	}
	if cfg.Watch.Notify.NotifyOn != "incident" {
		t.Errorf("notify_on = %q, want incident", cfg.Watch.Notify.NotifyOn)
	}
	if cfg.Watch.Notify.Cooldown != "9m" {
		t.Errorf("cooldown = %q, want 9m", cfg.Watch.Notify.Cooldown)
	}
	if cfg.Watch.Flapping.ShortThreshold != 7 {
		t.Errorf("flapping short_threshold = %d, want 7", cfg.Watch.Flapping.ShortThreshold)
	}
	// Normalize still runs, so the derived booleans follow notify_on.
	if !cfg.Watch.Notify.OnIncident || cfg.Watch.Notify.OnFlapping {
		t.Errorf("notify_on=incident should set OnIncident only, got %+v", cfg.Watch.Notify)
	}
}

func TestLoadNestedWatchFormWinsOverFlat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "both.yaml")
	content := `
watch:
  enabled: true
  cooldown: 9m
  notify:
    enabled: false
    cooldown: 1m
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Watch.Notify.Enabled {
		t.Error("the nested block should win over the flat key")
	}
	if cfg.Watch.Notify.Cooldown != "1m" {
		t.Errorf("cooldown = %q, want 1m from the nested block", cfg.Watch.Notify.Cooldown)
	}
}

// Keys the file does not mention must keep the defaults Load seeded.
func TestLoadWatchDefaultsSurvivePartialSpec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")
	content := `
watch:
  flapping:
    short_threshold: 7
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Watch.Notify.Cooldown != "5m" {
		t.Errorf("cooldown = %q, want the 5m default", cfg.Watch.Notify.Cooldown)
	}
	if cfg.Watch.Flapping.LongThreshold != 5 {
		t.Errorf("long_threshold = %d, want the default 5", cfg.Watch.Flapping.LongThreshold)
	}
	if cfg.Watch.Flapping.ShortThreshold != 7 {
		t.Errorf("short_threshold = %d, want the configured 7", cfg.Watch.Flapping.ShortThreshold)
	}
}

func TestLoadWatchRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "retention.yaml")
	content := `
watch:
  retention:
    max_incidents: 25
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Watch.Retention.MaxIncidents != 25 {
		t.Errorf("max_incidents = %d, want the configured 25", cfg.Watch.Retention.MaxIncidents)
	}
	// Setting retention must not disturb the neighbouring watch defaults.
	if cfg.Watch.Notify.Cooldown != "5m" {
		t.Errorf("cooldown = %q, want the 5m default", cfg.Watch.Notify.Cooldown)
	}
}

func TestLoadWatchRetentionDefaultsWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-retention.yaml")
	content := `
watch:
  notify:
    enabled: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// A config that predates the setting must not read as unlimited history.
	if cfg.Watch.Retention.MaxIncidents != 200 {
		t.Errorf("max_incidents = %d, want the default 200", cfg.Watch.Retention.MaxIncidents)
	}
}

func TestLoadWatchRetentionNegativeMeansUnlimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unlimited.yaml")
	content := `
watch:
  retention:
    max_incidents: -1
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Normalize turns the explicit opt-out into the "keep everything" zero that
	// PruneIncidents understands.
	if cfg.Watch.Retention.MaxIncidents != 0 {
		t.Errorf("max_incidents = %d, want 0 (unlimited)", cfg.Watch.Retention.MaxIncidents)
	}
}

// docs/backup.md has documented `backup: dir:` all along while the schema only
// had the top-level `backup_dir`, so a config copied from the documentation
// parsed without complaint and then wrote to the home directory. Someone
// pointing backups at a NAS got neither the destination nor any sign of it.
func TestResolveBackupDir_ReadsBothSpellings(t *testing.T) {
	nested := &Config{Backup: BackupConfig{Dir: "/mnt/nas/backups"}}
	if got := nested.ResolveBackupDir(); got != "/mnt/nas/backups" {
		t.Errorf("backup.dir = %q, want /mnt/nas/backups — the documented spelling is ignored", got)
	}

	flat := &Config{BackupDir: "/srv/backups"}
	if got := flat.ResolveBackupDir(); got != "/srv/backups" {
		t.Errorf("backup_dir = %q, want /srv/backups", got)
	}

	// A file carrying both gets the nested one; the flat key is compatibility.
	both := &Config{Backup: BackupConfig{Dir: "/mnt/nas"}, BackupDir: "/srv/old"}
	if got := both.ResolveBackupDir(); got != "/mnt/nas" {
		t.Errorf("with both set, got %q, want the nested /mnt/nas", got)
	}

	empty := &Config{}
	if got := empty.ResolveBackupDir(); got == "" {
		t.Error("an unconfigured backup dir should still resolve to the default")
	}
}

func TestParseBackupBlockFromYAML(t *testing.T) {
	var cfg Config
	data := []byte("backup:\n  dir: /mnt/nas/backups\n  retention:\n    max_archives: 7\n    max_bytes: 20GB\n")
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.ResolveBackupDir() != "/mnt/nas/backups" {
		t.Errorf("dir = %q", cfg.ResolveBackupDir())
	}
	ret := cfg.ResolveBackupRetention()
	if ret.MaxArchives != 7 || ret.MaxBytes != "20GB" {
		t.Errorf("retention = %+v, want 7 / 20GB", ret)
	}
	if ret.IsZero() {
		t.Error("a configured retention should not read as unconfigured")
	}
}
