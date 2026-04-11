package install

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func TestRegistryPlex(t *testing.T) {
	app, ok := Registry["plex"]
	if !ok {
		t.Fatal("plex should be in registry")
	}
	if app.DefaultPort != "32400" {
		t.Errorf("expected port 32400, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "32400" {
		t.Errorf("expected container port 32400, got %s", app.ContainerPort)
	}

	// Verify compose template contains key elements
	if !strings.Contains(app.ComposeFile, "plexinc/pms-docker:latest") {
		t.Error("compose should use plexinc/pms-docker:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/config") {
		t.Error("compose should mount /config volume")
	}
	if !strings.Contains(app.ComposeFile, "/transcode") {
		t.Error("compose should mount /transcode volume")
	}
	if !strings.Contains(app.ComposeFile, ":/data:ro") {
		t.Error("compose should mount media volume as read-only (:ro)")
	}
	if !strings.Contains(app.ComposeFile, "PUID") || !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PUID/PGID")
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

func TestRegistryHomepage(t *testing.T) {
	app, ok := Registry["homepage"]
	if !ok {
		t.Fatal("homepage should be in registry")
	}
	if app.DefaultPort != "3010" {
		t.Errorf("expected port 3010, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "3000" {
		t.Errorf("expected container port 3000, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("homepage should have a description")
	}
	if !strings.Contains(app.ComposeFile, "ghcr.io/gethomepage/homepage:latest") {
		t.Error("compose should use ghcr.io/gethomepage/homepage:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/app/config") {
		t.Error("compose should mount /app/config volume")
	}
	if !strings.Contains(app.ComposeFile, "PUID") || !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PUID/PGID")
	}
}

func TestRegistryStirlingPdf(t *testing.T) {
	app, ok := Registry["stirling-pdf"]
	if !ok {
		t.Fatal("stirling-pdf should be in registry")
	}
	if app.DefaultPort != "8083" {
		t.Errorf("expected port 8083, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "8080" {
		t.Errorf("expected container port 8080, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("stirling-pdf should have a description")
	}
	if !strings.Contains(app.ComposeFile, "frooodle/s-pdf:latest") {
		t.Error("compose should use frooodle/s-pdf:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/configs") {
		t.Error("compose should mount /configs volume")
	}
	if !strings.Contains(app.ComposeFile, "PUID") || !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PUID/PGID")
	}
}

func TestRegistrySpeedtestTracker(t *testing.T) {
	app, ok := Registry["speedtest-tracker"]
	if !ok {
		t.Fatal("speedtest-tracker should be in registry")
	}
	if app.DefaultPort != "8084" {
		t.Errorf("expected port 8084, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "80" {
		t.Errorf("expected container port 80, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("speedtest-tracker should have a description")
	}
	if !strings.Contains(app.ComposeFile, "lscr.io/linuxserver/speedtest-tracker:latest") {
		t.Error("compose should use lscr.io/linuxserver/speedtest-tracker:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/config") {
		t.Error("compose should mount /config volume")
	}
	if !strings.Contains(app.ComposeFile, "PUID") || !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PUID/PGID")
	}
}

func TestRegistryMealie(t *testing.T) {
	app, ok := Registry["mealie"]
	if !ok {
		t.Fatal("mealie should be in registry")
	}
	if app.DefaultPort != "9925" {
		t.Errorf("expected port 9925, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "9000" {
		t.Errorf("expected container port 9000, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("mealie should have a description")
	}
	if !strings.Contains(app.ComposeFile, "ghcr.io/mealie-recipes/mealie:latest") {
		t.Error("compose should use ghcr.io/mealie-recipes/mealie:latest image")
	}
	if !strings.Contains(app.ComposeFile, "/app/data") {
		t.Error("compose should mount /app/data volume")
	}
	if !strings.Contains(app.ComposeFile, "PUID") || !strings.Contains(app.ComposeFile, "PGID") {
		t.Error("compose should support PUID/PGID")
	}
}

func TestRegistryPiHole(t *testing.T) {
	app, ok := Registry["pi-hole"]
	if !ok {
		t.Fatal("pi-hole should be in registry")
	}
	if app.DefaultPort != "8088" {
		t.Errorf("expected port 8088, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "80" {
		t.Errorf("expected container port 80, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("pi-hole should have a description")
	}
	if !strings.Contains(app.ComposeFile, "pihole/pihole:latest") {
		t.Error("compose should use pihole/pihole:latest image")
	}
	if !strings.Contains(app.ComposeFile, "53:53/tcp") {
		t.Error("compose should expose DNS port 53/tcp")
	}
	if !strings.Contains(app.ComposeFile, "53:53/udp") {
		t.Error("compose should expose DNS port 53/udp")
	}
	if !strings.Contains(app.ComposeFile, "cap_add") {
		t.Error("compose should include cap_add")
	}
	if !strings.Contains(app.ComposeFile, "NET_ADMIN") {
		t.Error("compose should include NET_ADMIN capability")
	}
	// pi-hole does NOT use PUID/PGID
	if strings.Contains(app.ComposeFile, "PUID") || strings.Contains(app.ComposeFile, "PGID") {
		t.Error("pi-hole compose should NOT use PUID/PGID")
	}
}

func TestRegistryAdguardHome(t *testing.T) {
	app, ok := Registry["adguard-home"]
	if !ok {
		t.Fatal("adguard-home should be in registry")
	}
	if app.DefaultPort != "3000" {
		t.Errorf("expected port 3000, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "3000" {
		t.Errorf("expected container port 3000, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("adguard-home should have a description")
	}
	if !strings.Contains(app.ComposeFile, "adguard/adguardhome:latest") {
		t.Error("compose should use adguard/adguardhome:latest image")
	}
	if !strings.Contains(app.ComposeFile, "53:53/tcp") {
		t.Error("compose should expose DNS port 53/tcp")
	}
	if !strings.Contains(app.ComposeFile, "53:53/udp") {
		t.Error("compose should expose DNS port 53/udp")
	}
	if !strings.Contains(app.ComposeFile, "/opt/adguardhome/work") {
		t.Error("compose should mount /opt/adguardhome/work")
	}
	if !strings.Contains(app.ComposeFile, "/opt/adguardhome/conf") {
		t.Error("compose should mount /opt/adguardhome/conf")
	}
	// adguard-home does NOT use PUID/PGID
	if strings.Contains(app.ComposeFile, "PUID") || strings.Contains(app.ComposeFile, "PGID") {
		t.Error("adguard-home compose should NOT use PUID/PGID")
	}
}

func TestRegistryPortainer(t *testing.T) {
	app, ok := Registry["portainer"]
	if !ok {
		t.Fatal("portainer should be in registry")
	}
	if app.DefaultPort != "9443" {
		t.Errorf("expected port 9443, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "9443" {
		t.Errorf("expected container port 9443, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("portainer should have a description")
	}
	if !strings.Contains(app.ComposeFile, "portainer/portainer-ce:latest") {
		t.Error("compose should use portainer/portainer-ce:latest image")
	}
	if !strings.Contains(app.ComposeFile, "docker.sock") {
		t.Error("compose should mount docker.sock")
	}
	if !strings.Contains(app.ComposeFile, "/data") {
		t.Error("compose should mount /data volume")
	}
	// portainer does NOT use PUID/PGID
	if strings.Contains(app.ComposeFile, "PUID") || strings.Contains(app.ComposeFile, "PGID") {
		t.Error("portainer compose should NOT use PUID/PGID")
	}
}

func TestRegistryNginxProxyManager(t *testing.T) {
	app, ok := Registry["nginx-proxy-manager"]
	if !ok {
		t.Fatal("nginx-proxy-manager should be in registry")
	}
	if app.DefaultPort != "81" {
		t.Errorf("expected port 81, got %s", app.DefaultPort)
	}
	if app.ContainerPort != "81" {
		t.Errorf("expected container port 81, got %s", app.ContainerPort)
	}
	if app.Description == "" {
		t.Error("nginx-proxy-manager should have a description")
	}
	if !strings.Contains(app.ComposeFile, "jc21/nginx-proxy-manager:latest") {
		t.Error("compose should use jc21/nginx-proxy-manager:latest image")
	}
	if !strings.Contains(app.ComposeFile, "80:80") {
		t.Error("compose should expose HTTP port 80")
	}
	if !strings.Contains(app.ComposeFile, "443:443") {
		t.Error("compose should expose HTTPS port 443")
	}
	if !strings.Contains(app.ComposeFile, "/etc/letsencrypt") {
		t.Error("compose should mount /etc/letsencrypt")
	}
	// nginx-proxy-manager does NOT use PUID/PGID
	if strings.Contains(app.ComposeFile, "PUID") || strings.Contains(app.ComposeFile, "PGID") {
		t.Error("nginx-proxy-manager compose should NOT use PUID/PGID")
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
			Port:         app.DefaultPort,
			DataDir:      "/tmp/test-data",
			UID:          1000,
			GID:          1000,
			DockerSocket: "/var/run/docker.sock",
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
	if portInUseBy("1") != "" {
		t.Error("port 1 should not be in use")
	}
}

func TestIsPortInUseHighPort(t *testing.T) {
	if portInUseBy("59999") != "" {
		t.Error("port 59999 should not be in use")
	}
}

// --- Special app pre-check tests (with mocks) ---

// withMockPortCheck temporarily overrides checkPortInUse for testing.
func withMockPortCheck(t *testing.T, mock func(string) string) {
	t.Helper()
	orig := checkPortInUse
	checkPortInUse = mock
	t.Cleanup(func() { checkPortInUse = orig })
}

// withMockInstalled temporarily overrides getInstalled for testing.
func withMockInstalled(t *testing.T, apps map[string]installedApp) {
	t.Helper()
	orig := getInstalled
	getInstalled = func() map[string]installedApp { return apps }
	t.Cleanup(func() { getInstalled = orig })
}

func TestPreCheckDNSPort53InUse(t *testing.T) {
	// Port 53 is busy → should warn for both pi-hole and adguard-home
	withMockPortCheck(t, func(port string) string {
		if port == "53" {
			return "systemd-resolved (PID 123)"
		}
		return ""
	})
	withMockInstalled(t, map[string]installedApp{})

	for _, appName := range []string{"pi-hole", "adguard-home"} {
		app := Registry[appName]
		issues := PreCheck(app, app.DefaultPort)
		found := false
		for _, issue := range issues {
			if strings.Contains(issue, "Port 53 is in use") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: expected port 53 warning, got %v", appName, issues)
		}
	}

	// Non-DNS app should NOT get port 53 warning
	app := Registry["uptime-kuma"]
	issues := PreCheck(app, app.DefaultPort)
	for _, issue := range issues {
		if strings.Contains(issue, "Port 53") {
			t.Errorf("uptime-kuma should not get port 53 warning, got: %s", issue)
		}
	}
}

func TestPreCheckDNSMutualConflict(t *testing.T) {
	withMockPortCheck(t, func(port string) string { return "" })

	// pi-hole installed → installing adguard-home should warn
	withMockInstalled(t, map[string]installedApp{
		"pi-hole": {Name: "pi-hole", Path: "/tmp/pi-hole", Port: "8088"},
	})
	app := Registry["adguard-home"]
	issues := PreCheck(app, app.DefaultPort)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "pi-hole is already installed") &&
			strings.Contains(issue, "two DNS servers") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected pi-hole conflict warning when installing adguard-home, got %v", issues)
	}

	// adguard-home installed → installing pi-hole should warn
	withMockInstalled(t, map[string]installedApp{
		"adguard-home": {Name: "adguard-home", Path: "/tmp/adguard", Port: "3000"},
	})
	app = Registry["pi-hole"]
	issues = PreCheck(app, app.DefaultPort)
	found = false
	for _, issue := range issues {
		if strings.Contains(issue, "adguard-home is already installed") &&
			strings.Contains(issue, "two DNS servers") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected adguard-home conflict warning when installing pi-hole, got %v", issues)
	}
}

func TestPreCheckNginxProxyManager80443(t *testing.T) {
	withMockInstalled(t, map[string]installedApp{})

	// Both 80 and 443 busy
	withMockPortCheck(t, func(port string) string {
		if port == "80" || port == "443" {
			return "nginx (PID 456)"
		}
		return ""
	})
	app := Registry["nginx-proxy-manager"]
	issues := PreCheck(app, app.DefaultPort)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "80/443") &&
			strings.Contains(issue, "nginx-proxy-manager needs these ports") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 80/443 warning for nginx-proxy-manager, got %v", issues)
	}

	// Only 80 busy
	withMockPortCheck(t, func(port string) string {
		if port == "80" {
			return "apache (PID 789)"
		}
		return ""
	})
	issues = PreCheck(app, app.DefaultPort)
	found = false
	for _, issue := range issues {
		if strings.Contains(issue, "Port 80 is in use") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected port 80 warning, got %v", issues)
	}

	// Neither busy → no port warning
	withMockPortCheck(t, func(port string) string { return "" })
	issues = PreCheck(app, app.DefaultPort)
	for _, issue := range issues {
		if strings.Contains(issue, "nginx-proxy-manager needs these ports") {
			t.Errorf("should not warn when ports are free, got: %s", issue)
		}
	}
}

func TestIsSpecialWarningPortainer(t *testing.T) {
	warning := IsSpecialWarning("portainer")
	if warning == "" {
		t.Fatal("expected warning for portainer")
	}
	if !strings.Contains(warning, "Docker socket access") {
		t.Errorf("expected Docker socket warning, got: %s", warning)
	}
	if !strings.Contains(warning, "full control") {
		t.Errorf("expected 'full control' in warning, got: %s", warning)
	}
}

func TestIsSpecialWarningOtherApps(t *testing.T) {
	for _, name := range []string{"uptime-kuma", "pi-hole", "nginx-proxy-manager", "jellyfin"} {
		if w := IsSpecialWarning(name); w != "" {
			t.Errorf("expected no warning for %s, got: %s", name, w)
		}
	}
}

func TestPreCheckDNSPort53WarningOSSpecific(t *testing.T) {
	// Verify the DNS port 53 warning message is OS-appropriate
	withMockPortCheck(t, func(port string) string {
		if port == "53" {
			return "some-process (PID 999)"
		}
		return ""
	})
	withMockInstalled(t, map[string]installedApp{})

	app := Registry["pi-hole"]
	issues := PreCheck(app, app.DefaultPort)

	var port53Issue string
	for _, issue := range issues {
		if strings.Contains(issue, "Port 53 is in use") {
			port53Issue = issue
			break
		}
	}
	if port53Issue == "" {
		t.Fatal("expected port 53 warning")
	}

	switch goos := runtime.GOOS; goos {
	case "darwin":
		if !strings.Contains(port53Issue, "sudo lsof -i :53") {
			t.Errorf("on macOS expected lsof hint, got: %s", port53Issue)
		}
		if strings.Contains(port53Issue, "systemd-resolved") {
			t.Errorf("on macOS should not mention systemd-resolved, got: %s", port53Issue)
		}
	case "linux":
		if !strings.Contains(port53Issue, "systemd-resolved") {
			t.Errorf("on Linux expected systemd-resolved hint, got: %s", port53Issue)
		}
	}
}

func TestPortainerComposeUsesDynamicSocket(t *testing.T) {
	app := Registry["portainer"]

	// Template should contain the DockerSocket placeholder
	if !strings.Contains(app.ComposeFile, "{{.DockerSocket}}") {
		t.Fatal("portainer compose should use {{.DockerSocket}} template variable")
	}

	// Render with a custom socket path
	tmpl, err := template.New("portainer").Parse(app.ComposeFile)
	if err != nil {
		t.Fatalf("invalid compose template: %v", err)
	}

	customSocket := "/home/user/.colima/default/docker.sock"
	ctx := composeContext{
		Port:         "9443",
		DataDir:      "/tmp/test-data",
		UID:          1000,
		GID:          1000,
		DockerSocket: customSocket,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, ctx); err != nil {
		t.Fatalf("template execute failed: %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, customSocket+":/var/run/docker.sock") {
		t.Errorf("rendered compose should map custom socket to /var/run/docker.sock, got:\n%s", rendered)
	}
	// Should NOT contain hardcoded /var/run/docker.sock on the host side
	if strings.Contains(rendered, "\"/var/run/docker.sock:/var/run/docker.sock\"") {
		t.Error("rendered compose should not hardcode /var/run/docker.sock as host path when custom socket is used")
	}
}

func TestPostInstallMessage(t *testing.T) {
	tests := []struct {
		app      string
		port     string
		contains string
	}{
		{"pi-hole", "8088", "DNS to this server"},
		{"adguard-home", "3000", "http://localhost:3000"},
		{"portainer", "9443", "https://localhost:9443"},
		{"nginx-proxy-manager", "81", "admin@example.com"},
		{"uptime-kuma", "3001", ""},
	}

	for _, tt := range tests {
		msg := PostInstallMessage(tt.app, tt.port)
		if tt.contains == "" {
			if msg != "" {
				t.Errorf("%s: expected empty message, got %q", tt.app, msg)
			}
			continue
		}
		if !strings.Contains(msg, tt.contains) {
			t.Errorf("%s: expected message containing %q, got %q", tt.app, tt.contains, msg)
		}
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		port    string
		wantErr bool
	}{
		{"8080", false},
		{"1", false},
		{"65535", false},
		{"0", true},
		{"65536", true},
		{"99999", true},
		{"-1", true},
		{"abc", true},
		{";rm -rf /", true},
		{"8080; cat /etc/passwd", true},
		{"$(whoami)", true},
		{"'; drop table; --", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.port, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePort(%q) error=%v, wantErr=%v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAppName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"uptime-kuma", false},
		{"my-app", false},
		{"app123", false},
		{"", true},
		{"../../etc", true},
		{"../foo", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"..", true},
		{"foo/../bar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAppName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAppName(%q) error=%v, wantErr=%v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	// Uninstall, Purge, Status should reject path traversal
	traversalNames := []string{"../../etc/passwd", "../foo", "foo/../../bar"}
	for _, name := range traversalNames {
		if _, err := Status(name); err == nil {
			t.Errorf("Status(%q) should return error", name)
		}
		if err := Uninstall(name); err == nil {
			t.Errorf("Uninstall(%q) should return error", name)
		}
		if err := Purge(name); err == nil {
			t.Errorf("Purge(%q) should return error", name)
		}
	}
}

func TestPortInUseByRejectsNonNumeric(t *testing.T) {
	// Non-numeric port strings should return empty (no injection)
	injections := []string{
		";rm -rf /",
		"$(whoami)",
		"80; cat /etc/passwd",
		"80`id`",
	}
	for _, p := range injections {
		result := portInUseBy(p)
		if result != "" {
			t.Errorf("portInUseBy(%q) should return empty for non-numeric input, got %q", p, result)
		}
	}
}

func TestInstallDryRun(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "homebutler-dryrun-*")
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	app := Registry["uptime-kuma"]
	opts := InstallOptions{DryRun: true}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Install(app, opts)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("Install dry-run failed: %v", err)
	}

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "[Dry Run]") {
		t.Error("Dry run output should contain '[Dry Run]'")
	}
	if !strings.Contains(output, "image: louislam/uptime-kuma:1") {
		t.Error("Dry run output should contain rendered compose content")
	}

	// Verify no files/directories created
	appDir := AppDir(app.Name)
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Errorf("App directory %s should not exist after dry-run", appDir)
	}
}
