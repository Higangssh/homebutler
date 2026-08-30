package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

var fixedNow = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

func doctorFuncs(status *system.StatusInfo, containers []docker.Container, pp []ports.PortInfo, warnings []string, backups []backup.ListEntry) CollectFuncs {
	return CollectFuncs{
		InventoryFns: inventory.CollectFuncs{
			StatusFn: func() (*system.StatusInfo, error) {
				return status, nil
			},
			DockerListFn: func() ([]docker.Container, error) {
				if warnings != nil {
					return nil, errors.New("docker unavailable")
				}
				return containers, nil
			},
			PortsListFn: func() (*ports.Result, error) {
				return &ports.Result{Ports: pp}, nil
			},
		},
		BackupListFn: func(string) ([]backup.ListEntry, error) {
			return backups, nil
		},
		SnapshotDir: "definitely-missing-snapshots",
	}
}

func healthyStatus() *system.StatusInfo {
	return &system.StatusInfo{
		Hostname: "testhost",
		OS:       "linux",
		Arch:     "amd64",
		Uptime:   "2d 3h",
		CPU:      system.CPUInfo{UsagePercent: 10, Cores: 4},
		Memory:   system.MemInfo{TotalGB: 16, UsedGB: 4, Percent: 25},
		Disks:    []system.DiskInfo{{Mount: "/", TotalGB: 100, UsedGB: 30, Percent: 30}},
	}
}

func TestRunPassesWhenNoFindings(t *testing.T) {
	fns := doctorFuncs(
		healthyStatus(),
		[]docker.Container{{Name: "web", State: "running"}},
		[]ports.PortInfo{{Address: "127.0.0.1", Port: "3000", Protocol: "tcp"}},
		nil,
		[]backup.ListEntry{{Name: "backup.tar.gz", CreatedAt: fixedNow.Add(-time.Hour).Format(time.RFC3339)}},
	)
	fns.SnapshotDir = t.TempDir()
	writeSnapshotMarker(t, fns.SnapshotDir)

	cfg := &config.Config{}
	cfg.Notify.Webhook = &notify.WebhookConfig{URL: "https://example.test/webhook"}

	r, err := Run(cfg, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if r.Status != SeverityPass {
		t.Fatalf("expected pass, got %s: %#v", r.Status, r.Findings)
	}
	if r.Summary.Pass != 1 || r.Summary.Warn != 0 || r.Summary.Fail != 0 {
		t.Fatalf("unexpected summary: %#v", r.Summary)
	}
}

func TestRunFindsUserRelevantRisks(t *testing.T) {
	status := healthyStatus()
	status.Memory.Percent = 91
	status.Disks[0].Percent = 95

	r, err := Run(&config.Config{}, doctorFuncs(
		status,
		[]docker.Container{{Name: "db", State: "exited"}},
		[]ports.PortInfo{{Address: "0.0.0.0", Port: "8080", Protocol: "tcp", Process: "app"}},
		nil,
		[]backup.ListEntry{{Name: "old.tar.gz", CreatedAt: fixedNow.Add(-10 * 24 * time.Hour).Format(time.RFC3339)}},
	), Options{Now: fixedNow, BackupMaxAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if r.Status != SeverityFail {
		t.Fatalf("expected fail, got %s", r.Status)
	}

	joined := findingsText(r.Findings)
	for _, want := range []string{"Memory is almost full", "Disk is almost full", "container(s) are stopped", "listening on all interfaces", "Latest backup is older than expected", "No notification channel configured", "No report baseline yet"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing finding %q in:\n%s", want, joined)
		}
	}
}

func TestNoBackupsWarnsWithActionableCommand(t *testing.T) {
	r, err := Run(nil, doctorFuncs(healthyStatus(), nil, nil, nil, nil), Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	f := findTitle(r.Findings, "No backups found")
	if f == nil {
		t.Fatalf("expected no backup finding: %#v", r.Findings)
	}
	if f.Severity != SeverityWarn {
		t.Fatalf("expected warn, got %s", f.Severity)
	}
	if f.Command != "homebutler backup" {
		t.Fatalf("expected simple backup command, got %q", f.Command)
	}
}

func TestBackupListErrorIsWarning(t *testing.T) {
	fns := doctorFuncs(healthyStatus(), nil, nil, nil, nil)
	fns.BackupListFn = func(string) ([]backup.ListEntry, error) {
		return nil, errors.New("permission denied")
	}

	r, err := Run(nil, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	f := findTitle(r.Findings, "Could not check backups")
	if f == nil || f.Severity != SeverityWarn {
		t.Fatalf("expected backup warning, got %#v", r.Findings)
	}
}

func TestInvalidBackupTimestampsWarn(t *testing.T) {
	r, err := Run(nil, doctorFuncs(
		healthyStatus(), nil, nil, nil,
		[]backup.ListEntry{{Name: "bad.tar.gz", CreatedAt: "not-a-time"}},
	), Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	f := findTitle(r.Findings, "Could not read backup timestamps")
	if f == nil || f.Severity != SeverityWarn {
		t.Fatalf("expected timestamp warning, got %#v", r.Findings)
	}
	if f.Command != "homebutler backup list" {
		t.Fatalf("expected backup list command, got %q", f.Command)
	}
}

func TestFormatHumanIncludesCommands(t *testing.T) {
	r := &Result{
		Timestamp:  fixedNow.Format(time.RFC3339),
		ServerName: "testhost",
		Status:     SeverityWarn,
		Summary:    Summary{Warn: 1},
		Findings: []Finding{{
			Severity: SeverityWarn,
			Category: "backup",
			Title:    "No backups found",
			Action:   "Create a backup.",
			Command:  "homebutler backup",
		}},
	}
	out := FormatHuman(r)
	if !strings.Contains(out, "Homebutler Doctor") || !strings.Contains(out, "$ homebutler backup") {
		t.Fatalf("unexpected human output:\n%s", out)
	}
}

func writeSnapshotMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "snapshot_20260510T120000Z.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write snapshot marker: %v", err)
	}
}

func findingsText(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.Title)
		b.WriteString("\n")
		b.WriteString(f.Detail)
		b.WriteString("\n")
	}
	return b.String()
}

func findTitle(findings []Finding, title string) *Finding {
	for i := range findings {
		if findings[i].Title == title {
			return &findings[i]
		}
	}
	return nil
}

func seedBackupDir(t *testing.T, sizes []int) string {
	t.Helper()
	dir := t.TempDir()
	for i, size := range sizes {
		name := filepath.Join(dir, fmt.Sprintf("backup_%d.tar.gz", i))
		if err := os.WriteFile(name, make([]byte, size), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func hasFinding(r *Result, title string) bool {
	for _, f := range r.Findings {
		if f.Title == title {
			return true
		}
	}
	return false
}

const unboundedTitle = "Backups have no retention limit and the directory is large"

// The default keeps every backup, so something has to say when the directory
// has outgrown what the operator expected. Nothing did before this.
func TestCheckBackupSize_WarnsWhenUnboundedAndLarge(t *testing.T) {
	dir := seedBackupDir(t, []int{4000, 4000, 4000})
	r := &Result{}
	checkBackupSize(r, &config.Config{}, dir, 3, Options{BackupMaxTotal: 10000})

	if !hasFinding(r, unboundedTitle) {
		t.Fatalf("no warning for an unbounded directory over the threshold: %+v", r.Findings)
	}
}

// A directory nobody needs to think about is not a finding.
func TestCheckBackupSize_SaysNothingWhenSmall(t *testing.T) {
	dir := seedBackupDir(t, []int{10, 10})
	r := &Result{}
	checkBackupSize(r, &config.Config{}, dir, 2, Options{BackupMaxTotal: 10000})

	if len(r.Findings) != 0 {
		t.Errorf("a small directory produced findings: %+v", r.Findings)
	}
}

// Once retention is configured the size is the operator's decision, and doctor
// repeating it back is noise.
func TestCheckBackupSize_SilentOnceRetentionIsConfigured(t *testing.T) {
	dir := seedBackupDir(t, []int{4000, 4000, 4000})
	cfg := &config.Config{Backup: config.BackupConfig{
		Retention: backup.RetentionConfig{MaxArchives: 5},
	}}
	r := &Result{}
	checkBackupSize(r, cfg, dir, 3, Options{BackupMaxTotal: 10000})

	if len(r.Findings) != 0 {
		t.Errorf("warned about a directory the operator has already bounded: %+v", r.Findings)
	}
}
