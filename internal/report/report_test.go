package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

func fakeFuncs(containers []docker.Container, pp []ports.PortInfo) inventory.CollectFuncs {
	return inventory.CollectFuncs{
		StatusFn: func() (*system.StatusInfo, error) {
			return &system.StatusInfo{
				Hostname: "testhost",
				OS:       "linux",
				Arch:     "amd64",
				Uptime:   "5d 3h",
				CPU:      system.CPUInfo{UsagePercent: 12.5, Cores: 4},
				Memory:   system.MemInfo{TotalGB: 16, UsedGB: 8, Percent: 50},
				Disks: []system.DiskInfo{
					{Mount: "/", TotalGB: 100, UsedGB: 60, Percent: 60},
				},
			}, nil
		},
		DockerListFn: func() ([]docker.Container, error) {
			return containers, nil
		},
		PortsListFn: func() (*ports.Result, error) {
			return &ports.Result{Ports: pp}, nil
		},
	}
}

func TestBaselineReport(t *testing.T) {
	dir := t.TempDir()

	r, err := Run(nil, fakeFuncs(
		[]docker.Container{
			{Name: "web", State: "running"},
			{Name: "db", State: "running"},
		},
		[]ports.PortInfo{
			{Address: "0.0.0.0", Port: "80"},
		},
	), Options{SnapshotDir: dir, Keep: 30})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !r.IsBaseline {
		t.Error("expected IsBaseline=true on first run")
	}
	if !r.SnapshotSaved {
		t.Error("expected SnapshotSaved=true on first run")
	}
	if r.ServerName == "" {
		t.Error("expected non-empty ServerName")
	}

	// Verify snapshot was written.
	files, err := listSnapshotFiles(dir)
	if err != nil {
		t.Fatalf("listing snapshots: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 snapshot file, got %d", len(files))
	}

	// Verify human output mentions baseline.
	text := FormatHuman(r)
	if !containsStr(text, "first look around") {
		t.Error("human output should mention first look around")
	}
}

func TestDiffReport(t *testing.T) {
	dir := t.TempDir()

	// Write a previous snapshot manually.
	prev := &Snapshot{
		Timestamp:  time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
		ServerName: "testhost",
		System: &system.StatusInfo{
			Hostname: "testhost",
			OS:       "linux",
			Arch:     "amd64",
			Uptime:   "4d 3h",
			CPU:      system.CPUInfo{UsagePercent: 10, Cores: 4},
			Memory:   system.MemInfo{TotalGB: 16, UsedGB: 7, Percent: 43.75},
			Disks: []system.DiskInfo{
				{Mount: "/", TotalGB: 100, UsedGB: 55, Percent: 55},
			},
		},
		Containers:      []docker.Container{{Name: "web", State: "running"}},
		RunningCount:    1,
		StoppedCount:    0,
		PublicPortCount: 1,
	}
	writeTestSnapshot(t, dir, prev)

	// Run report with changed state: +1 running container, +5 GB disk, +1 public port.
	r, err := Run(nil, fakeFuncs(
		[]docker.Container{
			{Name: "web", State: "running"},
			{Name: "db", State: "running"},
		},
		[]ports.PortInfo{
			{Address: "0.0.0.0", Port: "80"},
			{Address: "0.0.0.0", Port: "443"},
		},
	), Options{SnapshotDir: dir, Keep: 30})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if r.IsBaseline {
		t.Error("expected IsBaseline=false on second run")
	}

	changes := join(r.NotableChanges)
	if !containsStr(changes, "new: db — now running") {
		t.Errorf("expected the new container named, got: %s", changes)
	}
	if !containsStr(changes, "new: :443") {
		t.Errorf("expected the new listener named, got: %s", changes)
	}
	if !containsStr(changes, "disk: / — +5.0 GB since last report") {
		t.Errorf("expected disk delta, got: %s", changes)
	}
	if containsStr(changes, "Running containers:") || containsStr(changes, "Public ports:") {
		t.Errorf("counts should have been replaced by identities, got: %s", changes)
	}
}

func TestRetentionPruning(t *testing.T) {
	dir := t.TempDir()

	// Create 5 fake snapshot files.
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * 24 * time.Hour)
		snap := &Snapshot{
			Timestamp:  ts.Format(time.RFC3339),
			ServerName: "test",
			System: &system.StatusInfo{
				Hostname: "test",
				Disks:    []system.DiskInfo{},
			},
		}
		writeTestSnapshot(t, dir, snap)
	}

	files, _ := listSnapshotFiles(dir)
	if len(files) != 5 {
		t.Fatalf("expected 5 snapshots, got %d", len(files))
	}

	// Prune to keep 3.
	if err := pruneSnapshots(dir, 3); err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	files, _ = listSnapshotFiles(dir)
	if len(files) != 3 {
		t.Fatalf("expected 3 snapshots after prune, got %d", len(files))
	}

	// The remaining should be the 3 most recent.
	if files[0] != "snapshot_20250103T000000Z.json" {
		t.Errorf("unexpected oldest remaining: %s", files[0])
	}
}

func TestNoSave(t *testing.T) {
	dir := t.TempDir()

	r, err := Run(nil, fakeFuncs(nil, nil), Options{SnapshotDir: dir, NoSave: true})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !r.IsBaseline {
		t.Error("expected baseline when no previous snapshot exists")
	}
	if r.SnapshotSaved {
		t.Error("expected SnapshotSaved=false with --no-save")
	}

	// No snapshot should be written.
	files, _ := listSnapshotFiles(dir)
	if len(files) != 0 {
		t.Errorf("expected 0 snapshots with --no-save, got %d", len(files))
	}

	text := FormatHuman(r)
	if !containsStr(text, "baseline preview only") {
		t.Error("human output should say baseline was only previewed")
	}
}

func TestKeepMinimumOne(t *testing.T) {
	dir := t.TempDir()

	// Create 3 snapshots first.
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		writeTestSnapshot(t, dir, &Snapshot{
			Timestamp:  base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
			ServerName: "test",
		})
	}

	// Run with Keep=0 — should clamp to minimum 1 (not panic or delete all before saving).
	_, err := Run(nil, fakeFuncs(nil, nil), Options{SnapshotDir: dir, Keep: 0})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	files, _ := listSnapshotFiles(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 snapshot after minimum retention clamp, got %d", len(files))
	}
}

// --- helpers ---

func writeTestSnapshot(t *testing.T, dir string, snap *Snapshot) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts, _ := time.Parse(time.RFC3339, snap.Timestamp)
	filename := filepath.Join(dir, "snapshot_"+ts.Format("20060102T150405Z")+".json")
	data, _ := json.MarshalIndent(snap, "", "  ")
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func join(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += " | "
		}
		result += s
	}
	return result
}

// countPublicPorts must agree with ports.IsPublicBind. It used to compare
// addresses inline, missing bracketed IPv6 and the empty address, so report
// and doctor could disagree about the same port.
func TestCountPublicPortsMatchesIsPublicBind(t *testing.T) {
	pp := []ports.PortInfo{
		{Port: "80", Address: "0.0.0.0"},
		{Port: "443", Address: "::"},
		{Port: "8080", Address: "*"},
		{Port: "8443", Address: "[::]"},
		{Port: "9000", Address: ""},
		{Port: "5432", Address: "127.0.0.1"},
		{Port: "6379", Address: "192.168.1.10"},
	}

	want := 0
	for _, p := range pp {
		if ports.IsPublicBind(p.Address) {
			want++
		}
	}
	if want != 5 {
		t.Fatalf("fixture no longer exercises the cases it was written for: %d public binds", want)
	}

	if got := countPublicPorts(pp); got != want {
		t.Errorf("countPublicPorts = %d, ports.IsPublicBind classifies %d as public", got, want)
	}
}

// report takes the config and writes snapshots to disk that persist for as
// long as retention keeps them. Neither the report nor the snapshot has any
// reason to carry a credential, and the config fields are tagged json:"-" so
// they cannot. This is the behavioural end of that: give Run a config full of
// secrets and check that nothing it produces contains one.
func TestReportAndSnapshotLeakNoCredentials(t *testing.T) {
	const (
		pw    = "sup3r-secret-password"
		token = "9876543:AAH-telegram-bot-token"
		slack = "https://hooks.slack.com/services/T00/B00/leaked"
	)

	cfg := &config.Config{
		Servers: []config.ServerConfig{{Name: "pve1", Host: "10.0.0.1", Local: true, Password: pw}},
		Notify: notify.ProviderConfig{
			Telegram: &notify.TelegramConfig{BotToken: token, ChatID: "42"},
			Slack:    &notify.SlackConfig{WebhookURL: slack},
		},
	}

	dir := t.TempDir()
	rep, err := Run(cfg, fakeFuncs(nil, nil), Options{SnapshotDir: dir, Keep: 3})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	reportJSON, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading snapshot dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no snapshot was written, so this test is not checking what it claims to")
	}

	haystacks := map[string]string{"report --json": string(reportJSON)}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", f.Name(), err)
		}
		haystacks[f.Name()] = string(data)
	}

	for where, body := range haystacks {
		for _, secret := range []string{pw, token, slack} {
			if strings.Contains(body, secret) {
				t.Errorf("%s contains the credential %q", where, secret)
			}
		}
	}
}
