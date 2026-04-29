package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

// Snapshot persists inventory state for future comparison.
type Snapshot struct {
	Timestamp       string             `json:"timestamp"`
	ServerName      string             `json:"server_name"`
	System          *system.StatusInfo `json:"system"`
	Containers      []docker.Container `json:"containers"`
	Ports           []ports.PortInfo   `json:"ports"`
	Warnings        []string           `json:"warnings,omitempty"`
	RunningCount    int                `json:"running_count"`
	StoppedCount    int                `json:"stopped_count"`
	PublicPortCount int                `json:"public_port_count"`
}

// Report is the structured output of a report run.
type Report struct {
	Timestamp        string   `json:"timestamp"`
	ServerName       string   `json:"server_name"`
	IsBaseline       bool     `json:"is_baseline"`
	SnapshotSaved    bool     `json:"snapshot_saved"`
	Status           []string `json:"status"`
	NeedsAttention   []string `json:"needs_attention"`
	NotableChanges   []string `json:"notable_changes"`
	SuggestedActions []string `json:"suggested_actions"`
	Warnings         []string `json:"warnings,omitempty"`
}

// Options controls report behavior.
type Options struct {
	SnapshotDir string // Override snapshot directory (for testing)
	Keep        int    // Number of snapshots to retain
	NoSave      bool   // Skip writing snapshot
}

// CollectFuncs allows injecting data sources for testing.
type CollectFuncs = inventory.CollectFuncs

// DefaultCollectFuncs returns real system/docker/ports functions.
func DefaultCollectFuncs() CollectFuncs {
	return inventory.DefaultCollectFuncs()
}

// Run collects current state, compares against previous snapshot, and produces a report.
func Run(cfg *config.Config, fns CollectFuncs, opts Options) (*Report, error) {
	inv, err := inventory.Collect(cfg, fns)
	if err != nil {
		return nil, fmt.Errorf("collecting inventory: %w", err)
	}

	snap := buildSnapshot(inv)

	snapshotDir := opts.SnapshotDir
	if snapshotDir == "" {
		snapshotDir = defaultSnapshotDir()
	}

	// Load the latest previous snapshot.
	prev, _ := loadLatest(snapshotDir)

	report := buildReport(snap, prev)

	if opts.Keep < 1 {
		opts.Keep = 1
	}

	if !opts.NoSave {
		if err := saveSnapshot(snapshotDir, snap); err != nil {
			return nil, fmt.Errorf("saving snapshot: %w", err)
		}
		report.SnapshotSaved = true
		if err := pruneSnapshots(snapshotDir, opts.Keep); err != nil {
			report.Warnings = append(report.Warnings, "retention cleanup: "+err.Error())
		}
	}
	updateBaselineWording(report, opts.NoSave)

	return report, nil
}

func updateBaselineWording(r *Report, noSave bool) {
	if !r.IsBaseline {
		return
	}
	if noSave {
		r.NotableChanges = []string{"First inspection — no previous snapshot found. --no-save skipped baseline creation."}
		return
	}
	r.NotableChanges = []string{"First inspection — baseline snapshot created."}
}

func buildSnapshot(inv *inventory.Inventory) *Snapshot {
	snap := &Snapshot{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ServerName: inv.ServerName,
		System:     inv.System,
		Containers: inv.Containers,
		Ports:      inv.Ports,
		Warnings:   inv.Warnings,
	}
	for _, c := range inv.Containers {
		switch c.State {
		case "running":
			snap.RunningCount++
		default:
			snap.StoppedCount++
		}
	}
	snap.PublicPortCount = countPublicPorts(inv.Ports)
	return snap
}

func countPublicPorts(pp []ports.PortInfo) int {
	count := 0
	for _, p := range pp {
		if p.Address == "*" || p.Address == "0.0.0.0" || p.Address == "::" {
			count++
		}
	}
	return count
}

func buildReport(snap *Snapshot, prev *Snapshot) *Report {
	r := &Report{
		Timestamp:  snap.Timestamp,
		ServerName: snap.ServerName,
		Warnings:   snap.Warnings,
	}

	// Status section
	if snap.System != nil {
		r.Status = append(r.Status,
			fmt.Sprintf("Host: %s (%s/%s), uptime %s", snap.System.Hostname, snap.System.OS, snap.System.Arch, snap.System.Uptime),
		)
		r.Status = append(r.Status,
			fmt.Sprintf("CPU: %.1f%% (%d cores), Memory: %.1f/%.1f GB (%.0f%%)",
				snap.System.CPU.UsagePercent, snap.System.CPU.Cores,
				snap.System.Memory.UsedGB, snap.System.Memory.TotalGB, snap.System.Memory.Percent),
		)
		for _, d := range snap.System.Disks {
			r.Status = append(r.Status,
				fmt.Sprintf("Disk %s: %.1f/%.1f GB (%.0f%%)", d.Mount, d.UsedGB, d.TotalGB, d.Percent),
			)
		}
	}
	r.Status = append(r.Status,
		fmt.Sprintf("Containers: %d running, %d stopped", snap.RunningCount, snap.StoppedCount),
	)
	r.Status = append(r.Status,
		fmt.Sprintf("Public ports: %d", snap.PublicPortCount),
	)

	// Needs attention
	if snap.System != nil {
		if snap.System.Memory.Percent > 85 {
			r.NeedsAttention = append(r.NeedsAttention,
				fmt.Sprintf("Memory usage is high at %.0f%%", snap.System.Memory.Percent))
		}
		for _, d := range snap.System.Disks {
			if d.Percent > 85 {
				r.NeedsAttention = append(r.NeedsAttention,
					fmt.Sprintf("Disk %s usage is high at %.0f%%", d.Mount, d.Percent))
			}
		}
	}
	if snap.StoppedCount > 0 {
		r.NeedsAttention = append(r.NeedsAttention,
			fmt.Sprintf("%d container(s) stopped", snap.StoppedCount))
	}

	if prev == nil {
		r.IsBaseline = true
		r.NotableChanges = append(r.NotableChanges, "First inspection — no previous snapshot found.")
		r.SuggestedActions = append(r.SuggestedActions, "Run report again later to see changes over time.")
		return r
	}

	// Notable changes (diff against previous)
	if prev.System != nil && snap.System != nil {
		for _, d := range snap.System.Disks {
			for _, pd := range prev.System.Disks {
				if d.Mount == pd.Mount {
					delta := d.UsedGB - pd.UsedGB
					if delta > 0.5 || delta < -0.5 {
						r.NotableChanges = append(r.NotableChanges,
							fmt.Sprintf("Disk %s: %+.1f GB since last report", d.Mount, delta))
					}
				}
			}
		}
	}

	if snap.RunningCount != prev.RunningCount {
		r.NotableChanges = append(r.NotableChanges,
			fmt.Sprintf("Running containers: %d → %d", prev.RunningCount, snap.RunningCount))
	}
	if snap.StoppedCount != prev.StoppedCount {
		r.NotableChanges = append(r.NotableChanges,
			fmt.Sprintf("Stopped containers: %d → %d", prev.StoppedCount, snap.StoppedCount))
	}
	if snap.PublicPortCount != prev.PublicPortCount {
		r.NotableChanges = append(r.NotableChanges,
			fmt.Sprintf("Public ports: %d → %d", prev.PublicPortCount, snap.PublicPortCount))
	}

	if len(r.NotableChanges) == 0 {
		r.NotableChanges = append(r.NotableChanges, "No significant changes since last report.")
	}

	// Suggested actions
	if snap.PublicPortCount > prev.PublicPortCount {
		r.SuggestedActions = append(r.SuggestedActions,
			"New public port(s) detected — verify these are intentional.")
	}
	if snap.StoppedCount > prev.StoppedCount {
		r.SuggestedActions = append(r.SuggestedActions,
			"Container(s) stopped since last report — check logs with 'homebutler docker logs'.")
	}
	if len(r.NeedsAttention) > 0 && len(r.SuggestedActions) == 0 {
		r.SuggestedActions = append(r.SuggestedActions,
			"Address items in 'Needs attention' above.")
	}

	return r
}

// FormatHuman renders the report as a butler-style text summary.
func FormatHuman(r *Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "🏠 Homebutler Report — %s\n", r.ServerName)
	fmt.Fprintf(&b, "   %s\n\n", r.Timestamp)

	if r.IsBaseline {
		if r.SnapshotSaved {
			fmt.Fprintf(&b, "📋 This is the butler's first look around — baseline created.\n\n")
		} else {
			fmt.Fprintf(&b, "📋 This is the butler's first look around — baseline preview only (--no-save).\n\n")
		}
	}

	fmt.Fprintf(&b, "── Current Status ──\n")
	for _, s := range r.Status {
		fmt.Fprintf(&b, "   %s\n", s)
	}
	fmt.Fprintln(&b)

	if len(r.NeedsAttention) > 0 {
		fmt.Fprintf(&b, "── Needs Attention ──\n")
		for _, s := range r.NeedsAttention {
			fmt.Fprintf(&b, "   ⚠️  %s\n", s)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "── Notable Changes ──\n")
	for _, s := range r.NotableChanges {
		fmt.Fprintf(&b, "   %s\n", s)
	}
	fmt.Fprintln(&b)

	if len(r.SuggestedActions) > 0 {
		fmt.Fprintf(&b, "── Suggested Actions ──\n")
		for _, s := range r.SuggestedActions {
			fmt.Fprintf(&b, "   → %s\n", s)
		}
		fmt.Fprintln(&b)
	}

	if len(r.Warnings) > 0 {
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "   ⚠️  %s\n", w)
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}

// --- Snapshot persistence ---

func defaultSnapshotDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".homebutler", "reports", "snapshots")
}

func saveSnapshot(dir string, snap *Snapshot) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	t, _ := time.Parse(time.RFC3339, snap.Timestamp)
	filename := fmt.Sprintf("snapshot_%s.json", t.Format("20060102T150405Z"))
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0o644)
}

func loadLatest(dir string) (*Snapshot, error) {
	files, err := listSnapshotFiles(dir)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no previous snapshots")
	}
	data, err := os.ReadFile(filepath.Join(dir, files[len(files)-1]))
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func listSnapshotFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "snapshot_") && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // lexicographic = chronological with our naming scheme
	return names, nil
}

// pruneSnapshots keeps only the most recent `keep` snapshot files.
func pruneSnapshots(dir string, keep int) error {
	files, err := listSnapshotFiles(dir)
	if err != nil {
		return err
	}
	if len(files) <= keep {
		return nil
	}
	toRemove := files[:len(files)-keep]
	for _, f := range toRemove {
		if err := os.Remove(filepath.Join(dir, f)); err != nil {
			return err
		}
	}
	return nil
}
