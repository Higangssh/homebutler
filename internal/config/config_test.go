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

}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := `
alerts:
  cpu: 80
  memory: 70
  disk: 95
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
	}
	if target.MAC != "AA:BB:CC:DD:EE:FF" {
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

func TestValidateValid(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "web", Host: "192.168.1.10", User: "admin"},
		},
		Wake: []WakeTarget{
			{Name: "nas", MAC: "AA:BB:CC:DD:EE:FF"},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateLocalServerNoHost(t *testing.T) {
	// Local servers don't need a host
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "local", Local: true},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 0 {
		t.Errorf("expected no errors for local server without host, got %d: %v", len(errs), errs)
	}
}

func TestValidateMissingServerName(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Host: "192.168.1.10"},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "servers[0].name" {
		t.Errorf("expected field servers[0].name, got %q", errs[0].Field)
	}
}

func TestValidateMissingRemoteHost(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "web"},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "servers[0].host" {
		t.Errorf("expected field servers[0].host, got %q", errs[0].Field)
	}
}

func TestValidateInvalidPort(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "web", Host: "10.0.0.1", Port: 99999},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "servers[0].port" {
		t.Errorf("expected field servers[0].port, got %q", errs[0].Field)
	}
}

func TestValidateMissingKeyFile(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{Name: "web", Host: "10.0.0.1", KeyFile: "/nonexistent/key.pem"},
		},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "servers[0].key" {
		t.Errorf("expected field servers[0].key, got %q", errs[0].Field)
	}
}

func TestValidateMissingWakeFields(t *testing.T) {
	cfg := &Config{
		Wake: []WakeTarget{
			{Name: "nas"},    // missing MAC
			{MAC: "AA:BB:CC:DD:EE:FF"}, // missing Name
		},
	}
	errs := cfg.Validate()
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	cfg := &Config{
		Servers: []ServerConfig{
			{},               // missing name and host
			{Name: "valid", Host: "10.0.0.1"}, // fine
		},
	}
	errs := cfg.Validate()
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors (name+host missing), got %d: %v", len(errs), errs)
	}
}
