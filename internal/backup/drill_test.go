package backup

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	} else if svc.Name != "uptime-kuma" {
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

func createDrillArchive(t *testing.T, services []ServiceInfo) string {
	t.Helper()

	dir := t.TempDir()
	root := filepath.Join(dir, "backup_2026-04-05_1200")
	if err := os.MkdirAll(filepath.Join(root, "volumes"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Version:   "1",
		CreatedAt: time.Now().Format(time.RFC3339),
		Services:  services,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "backup_2026-04-05_1200.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archivePath, "-C", dir, filepath.Base(root)); err != nil {
		t.Skipf("tar not available: %v", err)
	}
	return archivePath
}

func TestRunDrillUnknownApp(t *testing.T) {
	archive := createDrillArchive(t, []ServiceInfo{{Name: "uptime-kuma", Image: "louislam/uptime-kuma:1"}})

	result, err := RunDrill("does-not-exist", DrillOptions{Archive: archive})
	if err == nil {
		t.Fatal("expected error for unknown app")
	}
	if result != nil {
		t.Fatal("expected nil result for unknown app")
	}
}

func TestRunDrillIntegrityFailure(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.tar.gz")
	if err := os.WriteFile(archive, []byte("not a tar"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := RunDrill("uptime-kuma", DrillOptions{Archive: archive})
	if err != nil {
		t.Fatalf("RunDrill returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	} else {
		if result.Passed {
			t.Fatal("expected failed result")
		}
		if !strings.Contains(result.Error, "integrity check failed") {
			t.Fatalf("unexpected error: %s", result.Error)
		}
	}
}

func TestRunDrillServiceNotFoundInBackup(t *testing.T) {
	archive := createDrillArchive(t, []ServiceInfo{{Name: "vaultwarden", Image: "vaultwarden/server:latest"}})

	result, err := RunDrill("uptime-kuma", DrillOptions{Archive: archive})
	if err != nil {
		t.Fatalf("RunDrill returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	} else if !strings.Contains(result.Error, `service "uptime-kuma" not found in backup`) {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestRunDrillAllNoDrillableApps(t *testing.T) {
	archive := createDrillArchive(t, []ServiceInfo{{Name: "custom-app", Image: "example/custom:latest"}})

	_, err := RunDrillAll(DrillOptions{Archive: archive})
	if err == nil {
		t.Fatal("expected error when no drillable apps exist")
	}
	if !strings.Contains(err.Error(), "no drillable apps found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreSuccessWithFilterAndMissingArchive(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "backup_2026-04-05_1200")
	if err := os.MkdirAll(filepath.Join(root, "volumes"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Version:   "1",
		CreatedAt: time.Now().Format(time.RFC3339),
		Services: []ServiceInfo{{
			Name:  "uptime-kuma",
			Image: "louislam/uptime-kuma:1",
			Mounts: []Mount{{
				Type:        "bind",
				Name:        "data",
				Source:      filepath.Join(dir, "restore-target"),
				Destination: "/app/data",
			}},
		}},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, "restore.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archive, "-C", dir, filepath.Base(root)); err != nil {
		t.Skipf("tar not available: %v", err)
	}

	result, err := Restore(archive, "uptime-kuma")
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(result.Services) != 1 || result.Services[0] != "uptime-kuma" {
		t.Fatalf("unexpected restored services: %#v", result.Services)
	}
	if result.Volumes != 1 {
		t.Fatalf("expected 1 volume, got %d", result.Volumes)
	}
}

func TestRestoreFilterNotFound(t *testing.T) {
	archive := createDrillArchive(t, []ServiceInfo{{Name: "vaultwarden", Image: "vaultwarden/server:latest"}})

	_, err := Restore(archive, "uptime-kuma")
	if err == nil {
		t.Fatal("expected filter not found error")
	}
	if !strings.Contains(err.Error(), `service "uptime-kuma" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreMountBindExtractsArchive(t *testing.T) {
	dir := t.TempDir()
	volDir := filepath.Join(dir, "volumes")
	if err := os.MkdirAll(volDir, 0o755); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(volDir, "data.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archivePath, "-C", srcDir, "."); err != nil {
		t.Skipf("tar not available: %v", err)
	}

	target := filepath.Join(dir, "target")
	err := restoreMount(Mount{Type: "bind", Name: "data", Source: target}, volDir)
	if err != nil {
		t.Fatalf("restoreMount() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(target, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Fatalf("unexpected restored content: %q", data)
	}
}

func TestProveHealthSuccessAndFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	status := http.StatusOK
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, "ok")
	})}
	defer server.Close()
	go server.Serve(ln)

	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)

	hc := HealthCheck{
		Path:          "/health",
		ExpectCodes:   []int{200},
		ContainerPort: "80",
		HealthTimeout: 3 * time.Second,
	}

	code, err := proveHealth(hc, port)
	if err != nil || code != 200 {
		t.Fatalf("proveHealth() = (%d, %v), want (200, nil)", code, err)
	}

	status = http.StatusServiceUnavailable
	hc.HealthTimeout = 2 * time.Second
	code, err = proveHealth(hc, port)
	if err == nil {
		t.Fatal("expected error for non-matching status")
	}
	if code != 503 {
		t.Fatalf("expected last status 503, got %d", code)
	}
}

func TestContainerLogsNoCrash(t *testing.T) {
	_ = containerLogs("definitely-not-a-real-container")
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
	tests := []struct {
		name    string
		app     string
		errMsg  string
		wantSub string
	}{
		{"database-error", "vaultwarden", "database disk image is malformed", "DB file"},
		{"db-error", "gitea", "db connection refused", "DB file"},
		{"generic-error", "uptime-kuma", "HTTP 503", "homebutler backup"},
		{"empty-error", "nginx", "", "homebutler backup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint := drillHint(tt.app, tt.errMsg)
			if !strings.Contains(hint, tt.wantSub) {
				t.Errorf("drillHint(%q, %q) = %q, want to contain %q", tt.app, tt.errMsg, hint, tt.wantSub)
			}
		})
	}
}

func TestExtractAndReadManifest(t *testing.T) {
	// Create a valid tar.gz with a manifest.json inside a subdirectory
	dir := t.TempDir()

	// Create the structure: backup_test/manifest.json
	backupDir := filepath.Join(dir, "backup_test")
	os.MkdirAll(backupDir, 0o755)
	manifest := Manifest{
		Version:   "1",
		CreatedAt: "2026-04-05T10:00:00Z",
		Services: []ServiceInfo{
			{Name: "nginx", Container: "abc123", Image: "nginx:latest"},
		},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(filepath.Join(backupDir, "manifest.json"), data, 0o644)
	os.MkdirAll(filepath.Join(backupDir, "volumes"), 0o755)

	// Create tar.gz
	archivePath := filepath.Join(dir, "test.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archivePath, "-C", dir, "backup_test"); err != nil {
		t.Skipf("tar not available: %v", err)
	}

	tmpDir := t.TempDir()
	m, extractedDir, err := extractAndReadManifest(archivePath, tmpDir)
	if err != nil {
		t.Fatalf("extractAndReadManifest() error = %v", err)
	}
	if m == nil {
		t.Fatal("manifest should not be nil")
	} else {
		if m.Version != "1" {
			t.Errorf("Version = %q, want %q", m.Version, "1")
		}
		if len(m.Services) != 1 {
			t.Fatalf("Services count = %d, want 1", len(m.Services))
		}
		if m.Services[0].Name != "nginx" {
			t.Errorf("Service name = %q, want %q", m.Services[0].Name, "nginx")
		}
	}
	if extractedDir == "" {
		t.Error("extractedDir should not be empty")
	}
}

func TestExtractAndReadManifest_InvalidArchive(t *testing.T) {
	dir := t.TempDir()
	badArchive := filepath.Join(dir, "bad.tar.gz")
	os.WriteFile(badArchive, []byte("not a tar file"), 0o644)

	_, _, err := extractAndReadManifest(badArchive, t.TempDir())
	if err == nil {
		t.Error("expected error for invalid archive")
	}
}

func TestFindExtractedDirError(t *testing.T) {
	// Empty dir with no subdirs and no manifest
	dir := t.TempDir()
	_, err := findExtractedDir(dir)
	if err == nil {
		t.Error("expected error for empty dir with no manifest")
	}
}

func TestDrillResultStringEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		result  DrillResult
		wantSub string
	}{
		{
			"integrity-failed",
			DrillResult{App: "test", Integrity: false, Passed: false, Error: "tar invalid"},
			"tar invalid",
		},
		{
			"booted-no-health",
			DrillResult{App: "test", Integrity: true, Booted: true, HealthStatus: 0, Passed: false},
			"no response",
		},
		{
			"health-failed",
			DrillResult{App: "test", Integrity: true, Booted: true, HealthStatus: 503, HealthPort: "8080", Passed: false, Error: "bad"},
			"503",
		},
		{
			"not-booted-with-integrity",
			DrillResult{App: "test", Integrity: true, Booted: false, Passed: false},
			"container failed",
		},
		{
			"with-logs",
			DrillResult{App: "test", Integrity: true, Booted: true, HealthStatus: 200, HealthPort: "8080", Passed: true, Logs: "starting up\nsecond line"},
			"starting up",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.result.String()
			if !strings.Contains(out, tt.wantSub) {
				t.Errorf("String() should contain %q, got:\n%s", tt.wantSub, out)
			}
		})
	}
}

func TestDrillReportStringEmpty(t *testing.T) {
	report := DrillReport{Total: 0, Passed: 0, Failed: 0}
	out := report.String()
	if !strings.Contains(out, "0/0 passed") {
		t.Errorf("String() should contain '0/0 passed', got:\n%s", out)
	}
}

func TestRestoreResultJSON(t *testing.T) {
	r := RestoreResult{
		Archive:  "/backups/test.tar.gz",
		Services: []string{"nginx", "postgres"},
		Volumes:  3,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed RestoreResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Archive != r.Archive {
		t.Errorf("Archive = %q, want %q", parsed.Archive, r.Archive)
	}
	if len(parsed.Services) != 2 {
		t.Errorf("Services count = %d, want 2", len(parsed.Services))
	}
	if parsed.Volumes != 3 {
		t.Errorf("Volumes = %d, want 3", parsed.Volumes)
	}
}

func TestBackupResultJSON(t *testing.T) {
	r := BackupResult{
		Archive:  "/backups/backup.tar.gz",
		Services: []string{"nginx"},
		Volumes:  2,
		Size:     "10.5 MB",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed BackupResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Archive != r.Archive {
		t.Errorf("Archive = %q, want %q", parsed.Archive, r.Archive)
	}
	if parsed.Size != "10.5 MB" {
		t.Errorf("Size = %q, want %q", parsed.Size, "10.5 MB")
	}
}

func TestComposeProjectJSON(t *testing.T) {
	p := ComposeProject{
		Name:       "myproject",
		Status:     "running(2)",
		ConfigFile: "/home/user/docker-compose.yml",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed ComposeProject
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Name != "myproject" {
		t.Errorf("Name = %q, want %q", parsed.Name, "myproject")
	}
}

func TestCopyComposeFilesEdgeCases(t *testing.T) {
	destDir := t.TempDir()

	// Empty config files string
	if err := copyComposeFiles("", destDir); err != nil {
		t.Errorf("empty configFiles should not error: %v", err)
	}

	// Non-existent source file (should be skipped, not error)
	if err := copyComposeFiles("/nonexistent/docker-compose.yml", destDir); err != nil {
		t.Errorf("nonexistent source should not error: %v", err)
	}

	// Multiple comma-separated files (only existing ones copied)
	srcDir := t.TempDir()
	f1 := filepath.Join(srcDir, "compose1.yml")
	f2 := filepath.Join(srcDir, "compose2.yml")
	os.WriteFile(f1, []byte("version: '3'"), 0o644)
	os.WriteFile(f2, []byte("version: '3'"), 0o644)

	dest2 := t.TempDir()
	if err := copyComposeFiles(f1+", "+f2, dest2); err != nil {
		t.Errorf("multiple files should not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest2, "compose1.yml")); err != nil {
		t.Error("compose1.yml should be copied")
	}
	if _, err := os.Stat(filepath.Join(dest2, "compose2.yml")); err != nil {
		t.Error("compose2.yml should be copied")
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcdef", 5, "ab..."},
		{"exactly", 7, "exactly"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestSanitizeNameEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal-name", "normal-name"},
		{"with spaces", "with_spaces"},
		{"a:b:c", "a_b_c"},
		{"_leading", "leading"},
		{"___multi", "multi"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListEntryJSON(t *testing.T) {
	e := ListEntry{
		Name:      "backup_2026-04-05.tar.gz",
		Path:      "/backups/backup_2026-04-05.tar.gz",
		Size:      "15.2 MB",
		CreatedAt: "2026-04-05T10:00:00Z",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed ListEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Name != e.Name {
		t.Errorf("Name = %q, want %q", parsed.Name, e.Name)
	}
}

func TestHealthCheckPaths(t *testing.T) {
	// Verify specific health check paths for known apps
	checks := map[string]string{
		"vaultwarden": "/alive",
		"pi-hole":     "/admin",
		"jellyfin":    "/health",
		"plex":        "/web",
	}
	for app, path := range checks {
		hc, ok := HealthChecks[app]
		if !ok {
			t.Errorf("HealthChecks missing %q", app)
			continue
		}
		if hc.Path != path {
			t.Errorf("HealthChecks[%q].Path = %q, want %q", app, hc.Path, path)
		}
	}
}

func TestFormatSizeEdgeCases(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{1023, "1023 B"},
		{1025, "1.0 KB"},
		{1048575, "1024.0 KB"},
		{1048577, "1.0 MB"},
	}
	for _, tt := range tests {
		got := formatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
