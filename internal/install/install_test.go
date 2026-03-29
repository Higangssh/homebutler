package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

func TestRegistryHasApps(t *testing.T) {
	if len(Registry) < 2 {
		t.Fatalf("registry should have at least 2 apps, got %d", len(Registry))
	}

	for name, app := range Registry {
		if app.Name == "" {
			t.Errorf("app %s has empty name", name)
		}
		if app.DefaultPort == "" {
			t.Errorf("app %s has empty default port", name)
		}
		if app.ComposeFile == "" {
			t.Errorf("app %s has empty compose template", name)
		}
		if app.Description == "" {
			t.Errorf("app %s has empty description", name)
		}
		if app.ContainerPort == "" {
			t.Errorf("app %s has empty container port", name)
		}
	}
}

func TestRegistryUptimeKuma(t *testing.T) {
	app, ok := Registry["uptime-kuma"]
	if !ok {
		t.Fatal("uptime-kuma should be in registry")
	}
	if app.DefaultPort != "3001" {
		t.Errorf("expected port 3001, got %s", app.DefaultPort)
	}
}

func TestRegistryJellyfin(t *testing.T) {
	app, ok := Registry["jellyfin"]
	if !ok {
		t.Fatal("jellyfin should be in registry")
	}
	if app.DefaultPort != "8096" {
		t.Errorf("expected port 8096, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "8096" {
		t.Errorf("expected container port 8096, got %s", app.ContainerPort)
	}
	if app.DataPath != "/config" {
		t.Errorf("expected data path /config, got %s", app.DataPath)
	}
	if app.Description == "" {
		t.Error("jellyfin should have a description")
	}

	// Verify compose template contains key elements
	if !strings.Contains(app.ComposeFile, "jellyfin/jellyfin:latest") {
		t.Error("compose should use jellyfin/jellyfin:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/config") {
		t.Error("compose should mount /config volume")
	}
	if !strings.Contains(app.ComposeFile, "/cache") {
		t.Error("compose should mount /cache volume")
	}
	if !strings.Contains(app.ComposeFile, "PUID") {
		t.Error("compose should support PUID")
	}
	if !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PGID")
	}
}

func TestRegistryVaultwarden(t *testing.T) {
	app, ok := Registry["vaultwarden"]
	if !ok {
		t.Fatal("vaultwarden should be in registry")
	}
	if app.DefaultPort != "8080" {
		t.Errorf("expected port 8080, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "80" {
		t.Errorf("expected container port 80, got %s", app.ContainerPort)
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
	if !strings.Contains(dir, ".homebutler/apps") {
		t.Errorf("expected dir to contain .homebutler/apps, got %s", dir)
	}
}

func TestBaseDir(t *testing.T) {
	base := BaseDir()
	if !filepath.IsAbs(base) {
		t.Errorf("expected absolute path, got %s", base)
	}
	if !strings.HasSuffix(base, filepath.Join(".homebutler", "apps")) {
		t.Errorf("expected dir to end with .homebutler/apps, got %s", base)
	}
}

func TestInstalledRegistryCRUD(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "homebutler-test-*")
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create → should not exist
	all := loadInstalled()
	if len(all) != 0 {
		t.Errorf("expected empty registry, got %d", len(all))
	}

	// Save first app
	err := saveInstalled(installedApp{Name: "app1", Path: "/tmp/app1", Port: "8080"})
	if err != nil {
		t.Fatalf("saveInstalled app1 failed: %v", err)
	}

	// Save second app
	err = saveInstalled(installedApp{Name: "app2", Path: "/tmp/app2", Port: "9090"})
	if err != nil {
		t.Fatalf("saveInstalled app2 failed: %v", err)
	}

	// Load → both exist
	all = loadInstalled()
	if len(all) != 2 {
		t.Errorf("expected 2 apps, got %d", len(all))
	}

	// GetInstalledPath
	path := GetInstalledPath("app1")
	if path != "/tmp/app1" {
		t.Errorf("expected /tmp/app1, got %s", path)
	}

	// GetInstalledPath for unknown app → fallback
	unknown := GetInstalledPath("unknown")
	if !strings.Contains(unknown, "unknown") {
		t.Errorf("expected fallback path containing 'unknown', got %s", unknown)
	}

	// Remove app1
	err = removeInstalled("app1")
	if err != nil {
		t.Fatalf("removeInstalled app1 failed: %v", err)
	}

	all = loadInstalled()
	if len(all) != 1 {
		t.Errorf("expected 1 app after remove, got %d", len(all))
	}
	if _, ok := all["app1"]; ok {
		t.Error("app1 should be removed")
	}
	if _, ok := all["app2"]; !ok {
		t.Error("app2 should still exist")
	}
}

func TestComposeTemplateRendering(t *testing.T) {
	for name, app := range Registry {
		tmpl, err := template.New(name).Parse(app.ComposeFile)
		if err != nil {
			t.Errorf("app %s: invalid compose template: %v", name, err)
			continue
		}

		ctx := composeContext{
			Port:    app.DefaultPort,
			DataDir: "/tmp/test-data",
			UID:     1000,
			GID:     1000,
		}

		var buf strings.Builder
		err = tmpl.Execute(&buf, ctx)
		if err != nil {
			t.Errorf("app %s: template execute failed: %v", name, err)
			continue
		}

		rendered := buf.String()
		if !strings.Contains(rendered, "image:") {
			t.Errorf("app %s: rendered compose missing 'image:'", name)
		}
		if !strings.Contains(rendered, app.DefaultPort) {
			t.Errorf("app %s: rendered compose missing port %s", name, app.DefaultPort)
		}
		if strings.Contains(app.ComposeFile, "{{.DataDir}}") && !strings.Contains(rendered, "/tmp/test-data") {
			t.Errorf("app %s: rendered compose missing data dir", name)
		}
	}
}

func TestPreCheckNoDocker(t *testing.T) {
	app := Registry["uptime-kuma"]
	issues := PreCheck(app, app.DefaultPort)
	_ = issues
}

func TestIsPortInUse(t *testing.T) {
	if isPortInUse("1") {
		t.Error("port 1 should not be in use")
	}
}

func TestIsPortInUseHighPort(t *testing.T) {
	if isPortInUse("59999") {
		t.Error("port 59999 should not be in use")
	}
}
