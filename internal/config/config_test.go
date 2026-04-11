package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
