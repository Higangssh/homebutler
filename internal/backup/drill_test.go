package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

func TestHealthCheckRegistryComplete(t *testing.T) {
	required := []string{
		"nginx-proxy-manager", "vaultwarden", "uptime-kuma",
		"pi-hole", "gitea", "jellyfin", "plex",
		"portainer", "homepage", "adguard-home",
	}
	for _, app := range required {
		hc, ok := HealthChecks[app]
		if !ok {
			t.Errorf("HealthChecks missing app %q", app)
			continue
		}
		if hc.Path == "" {
			t.Errorf("HealthChecks[%q].Path is empty", app)
		}
		if len(hc.ExpectCodes) == 0 {
			t.Errorf("HealthChecks[%q].ExpectCodes is empty", app)
		}
		if hc.ContainerPort == "" {
			t.Errorf("HealthChecks[%q].ContainerPort is empty", app)
		}
		if hc.BootTimeout == 0 {
			t.Errorf("HealthChecks[%q].BootTimeout is zero", app)
		}
		if hc.HealthTimeout == 0 {
			t.Errorf("HealthChecks[%q].HealthTimeout is zero", app)
		}
	}
}

func TestHealthCheckTimeouts(t *testing.T) {
	if DefaultBootTimeout != 60*time.Second {
		t.Errorf("DefaultBootTimeout = %v, want 60s", DefaultBootTimeout)
	}
	if DefaultHealthTimeout != 30*time.Second {
		t.Errorf("DefaultHealthTimeout = %v, want 30s", DefaultHealthTimeout)
	}
}

func TestFindLatestBackup(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "backup_2026-04-01_0900.tar.gz"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "backup_2026-04-04_1630.tar.gz"), []byte("latest"), 0o644)
	os.WriteFile(filepath.Join(dir, "backup_2026-04-02_1200.tar.gz"), []byte("mid"), 0o644)
	os.WriteFile(filepath.Join(dir, "not-a-backup.txt"), []byte("ignore"), 0o644)

	got, err := findLatestBackup(dir)
	if err != nil {
		t.Fatalf("findLatestBackup() error = %v", err)
	}
	want := filepath.Join(dir, "backup_2026-04-04_1630.tar.gz")
	if got != want {
		t.Errorf("findLatestBackup() = %q, want %q", got, want)
	}
}

func TestFindLatestBackupEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := findLatestBackup(dir)
	if err == nil {
		t.Error("findLatestBackup() should error on empty dir")
	}
}

func TestFindLatestBackupNotExist(t *testing.T) {
	_, err := findLatestBackup("/nonexistent/path/does/not/exist")
	if err == nil {
		t.Error("findLatestBackup() should error on nonexistent dir")
	}
}

func TestFindLatestBackupNoDir(t *testing.T) {
	_, err := findLatestBackup("")
	if err == nil {
		t.Error("findLatestBackup() should error on empty dir path")
	}
}

func TestLocateBackupExplicit(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "my-backup.tar.gz")
	os.WriteFile(archive, []byte("test"), 0o644)

	got, err := locateBackup(DrillOptions{Archive: archive})
	if err != nil {
		t.Fatalf("locateBackup() error = %v", err)
	}
	if got != archive {
		t.Errorf("locateBackup() = %q, want %q", got, archive)
	}
}

func TestLocateBackupExplicitNotFound(t *testing.T) {
	_, err := locateBackup(DrillOptions{Archive: "/nonexistent/file.tar.gz"})
	if err == nil {
		t.Error("locateBackup() should error on nonexistent archive")
	}
}

func TestVerifyArchive(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testdata")
	os.MkdirAll(dataDir, 0o755)
	os.WriteFile(filepath.Join(dataDir, "file1.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dataDir, "file2.txt"), []byte("world"), 0o644)
	os.MkdirAll(filepath.Join(dataDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dataDir, "sub", "file3.txt"), []byte("nested"), 0o644)

	archivePath := filepath.Join(dir, "test.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archivePath, "-C", dataDir, "."); err != nil {
		t.Skipf("tar not available: %v", err)
	}

	count, err := verifyArchive(archivePath)
	if err != nil {
		t.Fatalf("verifyArchive() error = %v", err)
	}
	if count < 3 {
		t.Errorf("verifyArchive() count = %d, want >= 3", count)
	}
}

func TestVerifyArchiveInvalid(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.tar.gz")
	os.WriteFile(bad, []byte("not a tar file"), 0o644)

	_, err := verifyArchive(bad)
	if err == nil {
		t.Error("verifyArchive() should error on invalid archive")
	}
}

func TestFindServiceInManifest(t *testing.T) {
	manifest := &Manifest{
		Services: []ServiceInfo{
			{Name: "uptime-kuma", Image: "louislam/uptime-kuma:1"},
			{Name: "vaultwarden", Image: "vaultwarden/server:latest"},
		},
	}

	svc := findServiceInManifest(manifest, "uptime-kuma")
	if svc == nil {
		t.Fatal("findServiceInManifest() returned nil for existing service")
	}
	if svc.Name != "uptime-kuma" {
		t.Errorf("Name = %q, want %q", svc.Name, "uptime-kuma")
	}

	svc = findServiceInManifest(manifest, "nonexistent")
	if svc != nil {
		t.Error("findServiceInManifest() should return nil for nonexistent service")
	}
}

func TestDrillResultJSON(t *testing.T) {
	r := DrillResult{
		App:          "uptime-kuma",
		Archive:      "/backups/backup_2026-04-04.tar.gz",
		Size:         "12.3 MB",
		FileCount:    847,
		Integrity:    true,
		Booted:       true,
		BootSeconds:  8,
		HealthStatus: 200,
		HealthPort:   "49152",
		Passed:       true,
		TotalSeconds: 23,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var parsed DrillResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if parsed.App != "uptime-kuma" {
		t.Errorf("App = %q, want %q", parsed.App, "uptime-kuma")
	}
	if !parsed.Passed {
		t.Error("Passed should be true")
	}
	if parsed.HealthStatus != 200 {
		t.Errorf("HealthStatus = %d, want 200", parsed.HealthStatus)
	}
	if parsed.FileCount != 847 {
		t.Errorf("FileCount = %d, want 847", parsed.FileCount)
	}
}

func TestDrillReportJSON(t *testing.T) {
	report := DrillReport{
		Total:  3,
		Passed: 2,
		Failed: 1,
		Results: []DrillResult{
			{App: "uptime-kuma", Passed: true, TotalSeconds: 15},
			{App: "vaultwarden", Passed: true, TotalSeconds: 18},
			{App: "pi-hole", Passed: false, Error: "HTTP 503", TotalSeconds: 35},
		},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var parsed DrillReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if parsed.Total != 3 {
		t.Errorf("Total = %d, want 3", parsed.Total)
	}
	if parsed.Passed != 2 {
		t.Errorf("Passed = %d, want 2", parsed.Passed)
	}
	if len(parsed.Results) != 3 {
		t.Errorf("Results count = %d, want 3", len(parsed.Results))
	}
}

func TestDrillResultFormat(t *testing.T) {
	r := DrillResult{
		App:          "nginx-proxy-manager",
		Archive:      "/backups/backup_2026-04-04.tar.gz",
		Size:         "12.3 MB",
		FileCount:    847,
		Integrity:    true,
		Booted:       true,
		BootSeconds:  8,
		HealthStatus: 200,
		HealthPort:   "49152",
		Passed:       true,
		TotalSeconds: 23,
	}

	out := r.String()
	if !strings.Contains(out, "nginx-proxy-manager") {
		t.Error("output should contain app name")
	}
	if !strings.Contains(out, "DRILL PASSED") {
		t.Error("output should contain DRILL PASSED")
	}
	if !strings.Contains(out, "847") {
		t.Error("output should contain file count")
	}
	if !strings.Contains(out, "12.3 MB") {
		t.Error("output should contain size")
	}
}

func TestDrillResultFormatFailed(t *testing.T) {
	r := DrillResult{
		App:          "vaultwarden",
		Archive:      "/backups/backup_2026-04-04.tar.gz",
		Size:         "45.1 MB",
		FileCount:    1203,
		Integrity:    true,
		Booted:       true,
		BootSeconds:  12,
		HealthStatus: 503,
		HealthPort:   "49200",
		Passed:       false,
		Error:        "HTTP 503 on port 49200",
		Logs:         "database disk image is malformed",
		TotalSeconds: 35,
	}

	out := r.String()
	if !strings.Contains(out, "DRILL FAILED") {
		t.Error("output should contain DRILL FAILED")
	}
	if !strings.Contains(out, "503") {
		t.Error("output should contain status code")
	}
	if !strings.Contains(out, "database disk image is malformed") {
		t.Error("output should contain log excerpt")
	}
}

func TestDrillReportFormat(t *testing.T) {
	report := DrillReport{
		Total:  2,
		Passed: 1,
		Failed: 1,
		Results: []DrillResult{
			{App: "uptime-kuma", Passed: true, TotalSeconds: 15},
			{App: "pi-hole", Passed: false, Error: "HTTP 503", TotalSeconds: 35},
		},
	}

	out := report.String()
	if !strings.Contains(out, "1/2 passed") {
		t.Error("output should contain pass ratio")
	}
	if !strings.Contains(out, "uptime-kuma") {
		t.Error("output should contain app names")
	}
	if !strings.Contains(out, "pi-hole") {
		t.Error("output should contain failed app")
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1203, "1,203"},
		{12345, "12,345"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
	}
	for _, tt := range tests {
		got := formatCount(tt.input)
		if got != tt.want {
			t.Errorf("formatCount(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFindFreePort(t *testing.T) {
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort() error = %v", err)
	}
	if port < 1024 || port > 65535 {
		t.Errorf("findFreePort() = %d, want between 1024 and 65535", port)
	}
}

func TestRandomSuffix(t *testing.T) {
	a := randomSuffix()
	b := randomSuffix()
	if a == "" {
		t.Error("randomSuffix() returned empty string")
	}
	if len(a) != 8 {
		t.Errorf("randomSuffix() length = %d, want 8", len(a))
	}
	if a == b {
		t.Error("randomSuffix() returned same value twice")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q, want %q", got, "short")
	}
	if got := truncate("this is a long string", 10); got != "this is..." {
		t.Errorf("truncate(long, 10) = %q, want %q", got, "this is...")
	}
}

func TestDrillHint(t *testing.T) {
	hint := drillHint("vaultwarden", "database disk image is malformed")
	if !strings.Contains(hint, "DB file") {
		t.Errorf("drillHint() = %q, want DB-related hint", hint)
	}

	hint = drillHint("uptime-kuma", "HTTP 503")
	if !strings.Contains(hint, "homebutler backup") {
		t.Errorf("drillHint() = %q, want generic hint", hint)
	}
}
