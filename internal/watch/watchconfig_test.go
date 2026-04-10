package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultWatchConfig(t *testing.T) {
	cfg := DefaultWatchConfig()

	if cfg.Notify.Enabled {
		t.Error("expected Enabled=false")
	}
	if cfg.Notify.NotifyOn != "flapping" {
		t.Errorf("expected NotifyOn=flapping, got %s", cfg.Notify.NotifyOn)
	}
	if cfg.Notify.OnIncident {
		t.Error("expected OnIncident=false")
	}
	if !cfg.Notify.OnFlapping {
		t.Error("expected OnFlapping=true")
	}
	if cfg.Notify.Cooldown != "5m" {
		t.Errorf("expected Cooldown=5m, got %s", cfg.Notify.Cooldown)
	}
}

func TestLoadWatchConfig_NoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadWatchConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := DefaultWatchConfig()
	if cfg.Notify.Enabled != want.Notify.Enabled {
		t.Error("expected default config when file missing")
	}
	if cfg.Notify.OnFlapping != want.Notify.OnFlapping {
		t.Error("expected OnFlapping=true from default")
	}
}

func TestLoadWatchConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
		"notify": {
			"enabled": true,
			"notify_on": "incident",
			"cooldown": "10m"
		},
		"flapping": {}
	}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWatchConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Notify.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.Notify.NotifyOn != "incident" {
		t.Errorf("expected NotifyOn=incident, got %s", cfg.Notify.NotifyOn)
	}
	if !cfg.Notify.OnIncident {
		t.Error("expected OnIncident=true")
	}
	if cfg.Notify.OnFlapping {
		t.Error("expected OnFlapping=false")
	}
	if cfg.Notify.Cooldown != "10m" {
		t.Errorf("expected Cooldown=10m, got %s", cfg.Notify.Cooldown)
	}
}

func TestLoadWatchConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{broken`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWatchConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveAndLoadWatchConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &WatchConfig{
		Notify: NotifySettings{
			Enabled:  true,
			NotifyOn: "incident",
			Cooldown: "2m",
		},
	}

	if err := SaveWatchConfig(dir, cfg); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := LoadWatchConfig(dir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if loaded.Notify.Enabled != cfg.Notify.Enabled {
		t.Error("Enabled mismatch")
	}
	if loaded.Notify.NotifyOn != cfg.Notify.NotifyOn {
		t.Error("NotifyOn mismatch")
	}
	if loaded.Notify.OnIncident != cfg.Notify.OnIncident {
		t.Error("OnIncident mismatch")
	}
	if loaded.Notify.OnFlapping != cfg.Notify.OnFlapping {
		t.Error("OnFlapping mismatch")
	}
	if loaded.Notify.Cooldown != cfg.Notify.Cooldown {
		t.Errorf("Cooldown mismatch: got %s, want %s", loaded.Notify.Cooldown, cfg.Notify.Cooldown)
	}
}
