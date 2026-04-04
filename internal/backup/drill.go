package backup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// DrillOptions configures a drill run.
type DrillOptions struct {
	BackupDir string // directory containing backup archives
	Archive   string // explicit archive path (overrides BackupDir lookup)
}

// DrillResult holds the outcome of drilling a single app.
type DrillResult struct {
	App          string `json:"app"`
	Archive      string `json:"archive"`
	Size         string `json:"size"`
	FileCount    int    `json:"file_count"`
	Integrity    bool   `json:"integrity"`
	Booted       bool   `json:"booted"`
	BootSeconds  int    `json:"boot_seconds"`
	HealthStatus int    `json:"health_status"`
	HealthPort   string `json:"health_port"`
	Passed       bool   `json:"passed"`
	Error        string `json:"error,omitempty"`
	Logs         string `json:"logs,omitempty"`
	TotalSeconds int    `json:"total_seconds"`
}

// DrillReport holds the aggregated results of drilling multiple apps.
type DrillReport struct {
	Results []DrillResult `json:"results"`
	Total   int           `json:"total"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
}

// RunDrill executes a backup drill for a single app.
// Returns (nil, error) if the drill cannot start at all.
// Returns (*DrillResult, nil) once the drill runs — check result.Passed.
func RunDrill(appName string, opts DrillOptions) (result *DrillResult, err error) {
	start := time.Now()

	hc, ok := HealthChecks[appName]
	if !ok {
		return nil, fmt.Errorf("no health check defined for %q", appName)
	}

	archivePath, err := locateBackup(opts)
	if err != nil {
		return nil, err
	}

	result = &DrillResult{
		App:     appName,
		Archive: archivePath,
	}

	defer func() {
		result.TotalSeconds = int(time.Since(start).Seconds())
		if r := recover(); r != nil {
			result.Passed = false
			result.Error = fmt.Sprintf("panic: %v", r)
		}
	}()

	if info, statErr := os.Stat(archivePath); statErr == nil {
		result.Size = formatSize(info.Size())
	}

	// Stage 1 & 2: Verify archive integrity
	fileCount, err := verifyArchive(archivePath)
	if err != nil {
		result.Error = fmt.Sprintf("integrity check failed: %v", err)
		return result, nil
	}
	result.FileCount = fileCount
	result.Integrity = true

	// Extract and read manifest
	tmpDir, err := os.MkdirTemp("", "homebutler-drill-*")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create temp dir: %v", err)
		return result, nil
	}
	defer os.RemoveAll(tmpDir)

	manifest, extractedDir, err := extractAndReadManifest(archivePath, tmpDir)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	svc := findServiceInManifest(manifest, appName)
	if svc == nil {
		result.Error = fmt.Sprintf("service %q not found in backup", appName)
		return result, nil
	}

	// Stage 3: Isolate — temp network + random port
	iso, err := createIsolation(appName)
	if err != nil {
		result.Error = fmt.Sprintf("isolation failed: %v", err)
		return result, nil
	}
	defer iso.cleanup()

	result.HealthPort = iso.hostPort

	// Stage 4: Boot — run container with backup data
	bootStart := time.Now()
	if err := bootContainer(svc, hc, iso, extractedDir); err != nil {
		result.Error = fmt.Sprintf("boot failed: %v", err)
		result.Logs = containerLogs(iso.container)
		return result, nil
	}

	if err := waitForContainer(iso.container, hc.BootTimeout); err != nil {
		result.Error = fmt.Sprintf("container did not start: %v", err)
		result.Logs = containerLogs(iso.container)
		return result, nil
	}
	result.Booted = true
	result.BootSeconds = int(time.Since(bootStart).Seconds())

	// Stage 5: Prove — HTTP health check
	statusCode, err := proveHealth(hc, iso.hostPort)
	result.HealthStatus = statusCode
	if err != nil {
		result.Error = err.Error()
		result.Logs = containerLogs(iso.container)
		return result, nil
	}

	result.Passed = true
	return result, nil
}

// RunDrillAll executes a backup drill for every app found in the backup
// that has a defined health check.
func RunDrillAll(opts DrillOptions) (*DrillReport, error) {
	archivePath, err := locateBackup(opts)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "homebutler-drill-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, _, err := extractAndReadManifest(archivePath, tmpDir)
	if err != nil {
		return nil, err
	}

	var apps []string
	for _, svc := range manifest.Services {
		if _, ok := HealthChecks[svc.Name]; ok {
			apps = append(apps, svc.Name)
		}
	}
	sort.Strings(apps)

	if len(apps) == 0 {
		return nil, fmt.Errorf("no drillable apps found in backup\n\n  💡 Backup may not contain apps with health checks defined")
	}

	report := &DrillReport{Total: len(apps)}

	for _, app := range apps {
		appOpts := DrillOptions{Archive: archivePath}
		result, runErr := RunDrill(app, appOpts)
		if runErr != nil {
			// Fatal error — couldn't even start the drill
			report.Results = append(report.Results, DrillResult{
				App:   app,
				Error: runErr.Error(),
			})
			report.Failed++
			continue
		}
		report.Results = append(report.Results, *result)
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}

	return report, nil
}

// --- Pipeline helpers ---

func locateBackup(opts DrillOptions) (string, error) {
	if opts.Archive != "" {
		if _, err := os.Stat(opts.Archive); err != nil {
			return "", fmt.Errorf("archive not found: %s", opts.Archive)
		}
		return opts.Archive, nil
	}
	return findLatestBackup(opts.BackupDir)
}

func findLatestBackup(backupDir string) (string, error) {
	if backupDir == "" {
		return "", fmt.Errorf("no backup directory configured")
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("backup directory does not exist: %s\n\n  💡 Run: homebutler backup", backupDir)
		}
		return "", fmt.Errorf("failed to read backup dir: %w", err)
	}

	var archives []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			archives = append(archives, e.Name())
		}
	}

	if len(archives) == 0 {
		return "", fmt.Errorf("no backup archives found in %s\n\n  💡 Run: homebutler backup", backupDir)
	}

	// Timestamped names (backup_2026-04-04_1630.tar.gz) sort lexically
	sort.Strings(archives)
	return filepath.Join(backupDir, archives[len(archives)-1]), nil
}

func verifyArchive(archivePath string) (int, error) {
	out, err := util.RunCmd("tar", "tzf", archivePath)
	if err != nil {
		return 0, fmt.Errorf("tar integrity check failed: %w", err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			count++
		}
	}
	return count, nil
}

func extractAndReadManifest(archivePath, tmpDir string) (*Manifest, string, error) {
	if _, err := util.RunCmd("tar", "xzf", archivePath, "-C", tmpDir); err != nil {
		return nil, "", fmt.Errorf("failed to extract archive: %w", err)
	}

	extractedDir, err := findExtractedDir(tmpDir)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(filepath.Join(extractedDir, "manifest.json"))
	if err != nil {
		return nil, "", fmt.Errorf("manifest.json not found in archive: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, extractedDir, nil
}

func findServiceInManifest(m *Manifest, appName string) *ServiceInfo {
	for i, svc := range m.Services {
		if svc.Name == appName {
			return &m.Services[i]
		}
	}
	return nil
}

// --- Isolation ---

type drillIsolation struct {
	network   string
	container string
	hostPort  string
	mountDir  string
}

func createIsolation(appName string) (*drillIsolation, error) {
	suffix := randomSuffix()
	networkName := "drill-net-" + suffix
	containerName := "drill-" + appName + "-" + suffix

	if _, err := util.DockerCmd("network", "create", networkName); err != nil {
		return nil, fmt.Errorf("failed to create drill network: %w", err)
	}

	port, err := findFreePort()
	if err != nil {
		util.DockerCmd("network", "rm", networkName)
		return nil, fmt.Errorf("failed to find free port: %w", err)
	}

	mountDir, err := os.MkdirTemp("", "homebutler-drill-mounts-*")
	if err != nil {
		util.DockerCmd("network", "rm", networkName)
		return nil, fmt.Errorf("failed to create mount dir: %w", err)
	}

	return &drillIsolation{
		network:   networkName,
		container: containerName,
		hostPort:  fmt.Sprintf("%d", port),
		mountDir:  mountDir,
	}, nil
}

func (iso *drillIsolation) cleanup() {
	util.DockerCmd("rm", "-f", iso.container)
	util.DockerCmd("network", "rm", iso.network)
	if iso.mountDir != "" {
		os.RemoveAll(iso.mountDir)
	}
}

// --- Boot ---

func bootContainer(svc *ServiceInfo, hc HealthCheck, iso *drillIsolation, extractedDir string) error {
	volDir := filepath.Join(extractedDir, "volumes")

	var volumeArgs []string
	for _, m := range svc.Mounts {
		safeName := sanitizeName(m.Name)
		mountPoint := filepath.Join(iso.mountDir, safeName)
		if err := os.MkdirAll(mountPoint, 0o755); err != nil {
			return fmt.Errorf("failed to create mount point: %w", err)
		}

		archivePath := filepath.Join(volDir, safeName+".tar.gz")
		if _, err := os.Stat(archivePath); err == nil {
			if _, err := util.RunCmd("tar", "xzf", archivePath, "-C", mountPoint); err != nil {
				return fmt.Errorf("failed to extract volume %s: %w", m.Name, err)
			}
		}

		volumeArgs = append(volumeArgs, "-v", mountPoint+":"+m.Destination)
	}

	args := []string{
		"run", "-d",
		"--name", iso.container,
		"--network", iso.network,
		"-p", iso.hostPort + ":" + hc.ContainerPort,
	}
	args = append(args, volumeArgs...)
	args = append(args, svc.Image)

	if _, err := util.DockerCmd(args...); err != nil {
		return fmt.Errorf("docker run failed: %w", err)
	}
	return nil
}

func waitForContainer(containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := util.DockerCmd("inspect", "--format", "{{.State.Running}}", containerName)
		if err == nil && strings.TrimSpace(out) == "true" {
			return nil
		}

		out, err = util.DockerCmd("inspect", "--format", "{{.State.Status}}", containerName)
		if err == nil && strings.TrimSpace(out) == "exited" {
			return fmt.Errorf("container exited prematurely")
		}

		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("container did not start within %s", timeout)
}

// --- Prove ---

func proveHealth(hc HealthCheck, hostPort string) (int, error) {
	url := fmt.Sprintf("http://localhost:%s%s", hostPort, hc.Path)
	client := &http.Client{Timeout: 5 * time.Second}

	deadline := time.Now().Add(hc.HealthTimeout)
	var lastErr error
	var lastStatus int

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		resp.Body.Close()
		lastStatus = resp.StatusCode

		for _, code := range hc.ExpectCodes {
			if resp.StatusCode == code {
				return resp.StatusCode, nil
			}
		}

		lastErr = fmt.Errorf("HTTP %d on port %s", resp.StatusCode, hostPort)
		time.Sleep(2 * time.Second)
	}

	if lastStatus > 0 {
		return lastStatus, fmt.Errorf("HTTP %d on port %s", lastStatus, hostPort)
	}
	return 0, fmt.Errorf("health check timed out: %v", lastErr)
}

func containerLogs(containerName string) string {
	out, err := util.DockerCmd("logs", "--tail", "20", containerName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// --- Shared helpers ---

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}

// --- Formatting ---

// String returns human-readable output for a single drill result.
func (r *DrillResult) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "🔍 Backup Drill — %s\n\n", r.App)
	fmt.Fprintf(&b, "  📦 Backup: %s\n", r.Archive)
	fmt.Fprintf(&b, "  📏 Size: %s\n", r.Size)

	if r.Integrity {
		fmt.Fprintf(&b, "  🔐 Integrity: ✅ tar valid (%s files)\n", formatCount(r.FileCount))
	} else {
		b.WriteString("  🔐 Integrity: ❌ tar invalid\n")
	}

	b.WriteString("\n")

	if r.Booted {
		fmt.Fprintf(&b, "  🚀 Boot: ✅ container started in %ds\n", r.BootSeconds)
	} else if r.Integrity {
		b.WriteString("  🚀 Boot: ❌ container failed to start\n")
	}

	if r.HealthStatus > 0 {
		if r.Passed {
			fmt.Fprintf(&b, "  🌐 Health: ✅ HTTP %d on port %s\n", r.HealthStatus, r.HealthPort)
		} else {
			fmt.Fprintf(&b, "  🌐 Health: ❌ HTTP %d on port %s\n", r.HealthStatus, r.HealthPort)
		}
	} else if r.Booted {
		b.WriteString("  🌐 Health: ❌ no response\n")
	}

	if r.Logs != "" {
		lines := strings.SplitN(r.Logs, "\n", 2)
		fmt.Fprintf(&b, "  📋 Logs: %q\n", strings.TrimSpace(lines[0]))
	}

	fmt.Fprintf(&b, "  ⏱️  Total: %ds\n\n", r.TotalSeconds)

	if r.Passed {
		b.WriteString("  ✅ DRILL PASSED\n")
	} else {
		b.WriteString("  ❌ DRILL FAILED\n")
		if r.Error != "" {
			fmt.Fprintf(&b, "  💡 %s\n", drillHint(r.App, r.Error))
		}
	}

	return b.String()
}

// String returns human-readable output for a drill report.
func (r *DrillReport) String() string {
	var b strings.Builder

	b.WriteString("🔍 Backup Drill — all apps\n\n")

	maxLen := 0
	for _, res := range r.Results {
		if len(res.App) > maxLen {
			maxLen = len(res.App)
		}
	}

	for _, res := range r.Results {
		if res.Passed {
			fmt.Fprintf(&b, "  %-*s  ✅ PASSED  (%ds)\n", maxLen, res.App, res.TotalSeconds)
		} else {
			hint := ""
			if res.Error != "" {
				hint = fmt.Sprintf("  (%s)", truncate(res.Error, 40))
			}
			fmt.Fprintf(&b, "  %-*s  ❌ FAILED%s\n", maxLen, res.App, hint)
		}
	}

	fmt.Fprintf(&b, "\n  📊 Result: %d/%d passed\n", r.Passed, r.Total)

	for _, res := range r.Results {
		if !res.Passed {
			fmt.Fprintf(&b, "  ❌ Failed: %s", res.App)
			if res.Error != "" {
				fmt.Fprintf(&b, " — %q", truncate(res.Error, 60))
			}
			b.WriteString("\n")
			fmt.Fprintf(&b, "  💡 Run: homebutler backup --service %s\n", res.App)
		}
	}

	return b.String()
}

func formatCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1000000, (n/1000)%1000, n%1000)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func drillHint(app, errMsg string) string {
	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "database") || strings.Contains(lower, "db") {
		return "DB file may be corrupted. Try: homebutler backup --service " + app
	}
	return "Run: homebutler backup --service " + app
}
