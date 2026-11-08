package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// The hint has to name the key the YAML actually has and the file that was
// actually read. It used to say key_file, which config ignores, and
// ~/.config/homebutler/config.yaml, which in the container image is not the
// config: HOMEBUTLER_CONFIG points at /config/config.yaml there.
func TestSSHHintNamesTheKeyAndTheLoadedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mounted.yaml")
	yaml := "servers:\n  - name: pi\n    host: 127.0.0.1\n    port: 1\n    key: " + filepath.Join(dir, "missing-key") + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(cfg.FindServer("pi"), "status", "--json")
	if err == nil {
		t.Fatal("connecting with a missing key succeeded")
	}
	msg := err.Error()
	if strings.Contains(msg, "key_file") {
		t.Errorf("hint names key_file, which is not a config key: %s", msg)
	}
	if !strings.Contains(msg, "'key'") {
		t.Errorf("hint does not name the 'key' setting: %s", msg)
	}
	if !strings.Contains(msg, path) {
		t.Errorf("hint does not name the file that was loaded (%s): %s", path, msg)
	}
}

// An entry that did not come from a file falls back to what the resolution
// rules pick, which is HOMEBUTLER_CONFIG when it is set.
func TestSSHHintFallsBackToHomebutlerConfig(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONFIG", "/config/config.yaml")
	srv := &config.ServerConfig{Name: "pi", Host: "127.0.0.1", Port: 1, AuthMode: "password"}
	_, err := Run(srv, "status", "--json")
	if err == nil {
		t.Fatal("connecting without credentials succeeded")
	}
	if msg := err.Error(); !strings.Contains(msg, "/config/config.yaml") || strings.Contains(msg, "key_file") {
		t.Errorf("hint = %s, want it to name /config/config.yaml and not key_file", msg)
	}
}
