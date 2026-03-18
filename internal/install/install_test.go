package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryHasApps(t *testing.T) {
	if len(Registry) == 0 {
		t.Fatal("registry should have at least one app")
	}

	app, ok := Registry["uptime-kuma"]
	if !ok {
		t.Fatal("uptime-kuma should be in registry")
	}

	if app.Name != "uptime-kuma" {
		t.Errorf("expected name uptime-kuma, got %s", app.Name)
	}
	if app.DefaultPort == "" {
		t.Error("default port should not be empty")
	}
	if app.ComposeFile == "" {
		t.Error("compose file template should not be empty")
	}
}

func TestListReturnsAllApps(t *testing.T) {
	apps := List()
	if len(apps) != len(Registry) {
		t.Errorf("expected %d apps, got %d", len(Registry), len(apps))
	}
}

func TestAppDir(t *testing.T) {
	dir := AppDir("test-app")
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %s", dir)
	}
	if filepath.Base(dir) != "test-app" {
		t.Errorf("expected dir to end with test-app, got %s", dir)
	}
}

func TestInstalledRegistry(t *testing.T) {
	// Use temp dir
	origBase := BaseDir()
	tmpDir, _ := os.MkdirTemp("", "homebutler-test-*")
	defer os.RemoveAll(tmpDir)

	// Override base dir for test
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", filepath.Dir(filepath.Dir(origBase)))

	// Save
	err := saveInstalled(installedApp{
		Name: "test-app",
		Path: "/tmp/test-app",
		Port: "8080",
	})
	if err != nil {
		t.Fatalf("saveInstalled failed: %v", err)
	}

	// Load
	all := loadInstalled()
	app, ok := all["test-app"]
	if !ok {
		t.Fatal("test-app should be in installed registry")
	}
	if app.Path != "/tmp/test-app" {
		t.Errorf("expected path /tmp/test-app, got %s", app.Path)
	}
	if app.Port != "8080" {
		t.Errorf("expected port 8080, got %s", app.Port)
	}

	// Remove
	err = removeInstalled("test-app")
	if err != nil {
		t.Fatalf("removeInstalled failed: %v", err)
	}

	all = loadInstalled()
	if _, ok := all["test-app"]; ok {
		t.Error("test-app should be removed from registry")
	}
}

func TestPreCheckNoDocker(t *testing.T) {
	// This test only verifies the check runs without panic.
	// Actual docker availability depends on the test environment.
	app := Registry["uptime-kuma"]
	issues := PreCheck(app, app.DefaultPort)
	// We don't assert specific results since docker may or may not be available
	_ = issues
}

func TestIsPortInUse(t *testing.T) {
	// Port 1 should not be in use (privileged)
	if isPortInUse("1") {
		t.Error("port 1 should not be in use")
	}
}
