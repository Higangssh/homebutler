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
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/style"
	"github.com/Higangssh/homebutler/internal/system"
)

// Snapshot persists inventory state for future comparison.
type Snapshot struct {
	Timestamp  string             `json:"timestamp"`
	ServerName string             `json:"server_name"`
	System     *system.StatusInfo `json:"system"`
	Containers []docker.Container `json:"containers"`
	Ports      []ports.PortInfo   `json:"ports"`
	Processes  []ProcessIdentity  `json:"processes,omitempty"`
	Warnings   []string           `json:"warnings,omitempty"`
	// Failed records which collectors did not answer when this snapshot was
	// taken. A diff that does not know this would report every container as
	// having disappeared when Docker was simply down at collection time.
	Failed          []string `json:"failed_collectors,omitempty"`
	RunningCount    int      `json:"running_count"`
	StoppedCount    int      `json:"stopped_count"`
	PublicPortCount int      `json:"public_port_count"`
}

// ChangeLine is one change as a caller acts on it: the kind to branch on, the
// thing it happened to, what happened, and the sentence a person reads.
//
// Text is what the terminal prints, unchanged. It is here so that a caller
// that only wants to show the line does not have to reassemble it, and so
// that adding structure took nothing away.
type ChangeLine struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Detail string `json:"detail,omitempty"`
	Text   string `json:"text"`
}

// Finding is one thing that needs attention. Kind says what it is about, so a
// caller can act on a stopped container differently from a full disk without
// matching on the sentence.
type Finding struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Text   string `json:"text"`
}

// Action is something to do about it. Command is the part that can be run, and
// Runner says who can run it — the classification doctor gained in #157, so an
// agent knows whether it can carry the action out or has to ask a person.
type Action struct {
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
	Runner  string `json:"runner,omitempty"`
	Tool    string `json:"tool,omitempty"`
}

// Report is the structured output of a report run.
//
// The three lists below carry their parts rather than only a sentence. An
// agent that wants to treat a `replaced` differently from a `disk` used to
// have to split the sentence on ": " and hunt for the name in prose that also
// held an em dash, an address and a port — which is reading prose with extra
// steps, and the kind column exists to avoid exactly that.
type Report struct {
	Timestamp      string       `json:"timestamp"`
	ServerName     string       `json:"server_name"`
	IsBaseline     bool         `json:"is_baseline"`
	SnapshotSaved  bool         `json:"snapshot_saved"`
	Status         []string     `json:"status"`
	NeedsAttention []Finding    `json:"needs_attention"`
	NotableChanges []ChangeLine `json:"notable_changes"`
	// ComparedTo is the timestamp of the snapshot this run was compared
	// against. Without it a reader cannot tell whether "what changed" covers
	// the last hour or the last three weeks.
	ComparedTo       string   `json:"compared_to,omitempty"`
	SuggestedActions []Action `json:"suggested_actions"`
	Warnings         []string `json:"warnings,omitempty"`

	// changes is the same set as NotableChanges, kept as the diff produced it
	// so the human renderer can group and align it. NotableChanges stays
	// complete and ungrouped.
	changes []Change
}

// action separates the part of a suggestion that can be run from the sentence
// around it, and says who can run it — the same classification doctor gives
// its findings, from the same function, so an agent is not told one thing by
// one command and another by the other.
func action(text string) Action {
	a := Action{Text: text}
	if i := strings.LastIndex(text, ": homebutler "); i >= 0 {
		a.Command = strings.TrimSuffix(text[i+2:], ".")
	}
	a.Runner, a.Tool = doctor.ClassifyCommand(a.Command)
	return a
}

// changeTexts is the sentences on their own, for the renderer that only ever
// wanted those.
func changeTexts(lines []ChangeLine) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}

// findingTexts and actionTexts are the same for the other two sections.
func findingTexts(findings []Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Text
	}
	return out
}

func actionTexts(actions []Action) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a.Text
	}
	return out
}

// note is a line with nothing to branch on: the baseline message, and "no
// significant changes". They are changes in the sense that they occupy the
// section, so they carry the kind that says there was nothing to compare.
func note(text string) ChangeLine {
	return ChangeLine{Kind: kindSkipped, Text: text}
}

// Options controls report behavior.
type Options struct {
	SnapshotDir string // Override snapshot directory (for testing)
	Keep        int    // Number of snapshots to retain
	NoSave      bool   // Skip writing snapshot
}

// CollectFuncs allows injecting data sources for testing.
type CollectFuncs = inventory.CollectFuncs

// DefaultCollectFuncs returns real system/docker/ports functions, plus the
// process collector: report is the only caller that compares runs, so it is
// the only one that asks for it.
func DefaultCollectFuncs() CollectFuncs {
	fns := inventory.DefaultCollectFuncs()
	fns.ProcessesFn = system.AllProcesses
	return fns
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
		r.NotableChanges = []ChangeLine{note("First inspection — no previous snapshot found. --no-save skipped baseline creation.")}
		return
	}
	r.NotableChanges = []ChangeLine{note("First inspection — baseline snapshot created.")}
}

func buildSnapshot(inv *inventory.Inventory) *Snapshot {
	snap := &Snapshot{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ServerName: inv.ServerName,
		System:     inv.System,
		Containers: inv.Containers,
		Ports:      inv.Ports,
		Processes:  processIdentities(inv.Processes),
		Warnings:   inv.Warnings,
		Failed:     inv.Failed,
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
		if ports.IsPublicBind(p.Address) {
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
	if prev != nil {
		r.ComparedTo = prev.Timestamp
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
			r.NeedsAttention = append(r.NeedsAttention, Finding{
				Kind: "memory",
				Text: fmt.Sprintf("Memory usage is high at %.0f%%", snap.System.Memory.Percent),
			})
		}
		for _, d := range snap.System.Disks {
			if d.Percent > 85 {
				r.NeedsAttention = append(r.NeedsAttention, Finding{
					Kind:   "disk",
					Target: d.Mount,
					Text:   fmt.Sprintf("Disk %s usage is high at %.0f%%", d.Mount, d.Percent),
				})
			}
		}
	}
	if snap.StoppedCount > 0 {
		r.NeedsAttention = append(r.NeedsAttention, Finding{
			Kind: "container",
			Text: fmt.Sprintf("%d container(s) stopped", snap.StoppedCount),
		})
	}
	// What moved is the other half of what needs attention. The checks above
	// answer "is something wrong right now"; a port that stopped being local
	// is wrong because of what changed, and no threshold can see it.
	if prev != nil {
		r.NeedsAttention = append(r.NeedsAttention, attentionFromChanges(prev, snap)...)
	}

	if prev == nil {
		r.IsBaseline = true
		r.NotableChanges = append(r.NotableChanges, note("First inspection — no previous snapshot found."))
		r.SuggestedActions = append(r.SuggestedActions, action("Run report again later to see changes over time."))
		return r
	}

	// Notable changes (diff against previous)
	// Compare identities rather than counts. A container replaced by a
	// different container leaves every count identical, and that is the case
	// this section used to answer with "no significant changes".
	var changes []Change
	if prev.System != nil && snap.System != nil {
		changes = append(changes, diffDisks(prev.System.Disks, snap.System.Disks)...)
	}
	// A skipped comparison is reported in the section itself, not only as a
	// trailing warning. "No significant changes since last report" handed to
	// an agent while the caveat sits in another field is an all-clear the
	// report cannot stand behind.
	if collectorAnswered(prev, snap, inventory.CollectorDocker) {
		changes = append(changes, diffContainers(prev.Containers, snap.Containers)...)
	} else {
		changes = append(changes, Change{
			Kind:    kindSkipped,
			Subject: nounContainers,
			Detail:  "not compared — Docker did not answer",
			noun:    nounContainers,
		})
	}
	switch {
	case !collectorAnswered(prev, snap, inventory.CollectorProcesses):
		changes = append(changes, Change{
			Kind:    kindSkipped,
			Subject: nounProcesses,
			Detail:  "not compared — the process collector did not answer",
			noun:    nounProcesses,
		})
	case len(prev.Processes) == 0:
		// A snapshot written before process tracking existed has no
		// processes at all. Diffing against it would report every process on
		// the machine as new on the first run after upgrading.
		changes = append(changes, Change{
			Kind:    kindSkipped,
			Subject: nounProcesses,
			Detail:  "not compared — the previous snapshot has none",
			noun:    nounProcesses,
		})
	default:
		changes = append(changes, diffProcesses(prev.Processes, snap.Processes)...)
	}
	if collectorAnswered(prev, snap, inventory.CollectorPorts) {
		changes = append(changes, diffPorts(prev.Ports, snap.Ports)...)
	} else {
		changes = append(changes, Change{
			Kind:    kindSkipped,
			Subject: nounPorts,
			Detail:  "not compared — the port collector did not answer",
			noun:    nounPorts,
		})
	}

	sortChanges(changes)
	changes = dedupeChanges(changes)
	r.changes = changes
	for _, c := range changes {
		r.NotableChanges = append(r.NotableChanges, c.line())
	}

	if len(r.NotableChanges) == 0 {
		r.NotableChanges = append(r.NotableChanges, note("No significant changes since last report."))
	}

	// Suggested actions, built from what changed rather than from counts. A
	// count cannot say which port or which container, and a port that changed
	// hands without changing the count produced no action at all.
	r.SuggestedActions = append(r.SuggestedActions, actionsFromChanges(prev, snap)...)
	if len(r.NeedsAttention) > 0 && len(r.SuggestedActions) == 0 {
		r.SuggestedActions = append(r.SuggestedActions,
			action("Address items in 'Needs attention' above."))
	}

	return r
}

// FormatHuman renders the report as a butler-style text summary.
func FormatHuman(r *Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "🏠 %s\n", style.Title.Render("Homebutler Report — "+r.ServerName))
	stamp := r.Timestamp
	if r.ComparedTo != "" {
		stamp += "  ·  compared with " + r.ComparedTo
	}
	fmt.Fprintf(&b, "   %s\n\n", style.Dim.Render(stamp))

	if r.IsBaseline {
		if r.SnapshotSaved {
			fmt.Fprintf(&b, "📋 This is the butler's first look around — baseline created.\n\n")
		} else {
			fmt.Fprintf(&b, "📋 This is the butler's first look around — baseline preview only (--no-save).\n\n")
		}
	}

	// What needs doing, then what moved, then the state that explains it.
	// Current Status used to lead, which put the reading every other tool
	// already shows ahead of the one only this one prints.
	if len(r.NeedsAttention) > 0 {
		fmt.Fprintf(&b, "%s\n", style.Section("Needs Attention"))
		for _, s := range r.NeedsAttention {
			fmt.Fprintf(&b, "   ⚠️  %s\n", style.Warn.Render(s.Text))
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "%s\n", style.Section("Notable Changes"))
	if len(r.changes) > 0 {
		b.WriteString(changeBlock(groupChanges(r.changes), "   "))
	} else {
		b.WriteString(style.LabelledBlock(changeTexts(r.NotableChanges), "   "))
	}
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "%s\n", style.Section("Current Status"))
	b.WriteString(style.LabelledBlock(r.Status, "   "))
	fmt.Fprintln(&b)

	if len(r.SuggestedActions) > 0 {
		fmt.Fprintf(&b, "%s\n", style.Section("Suggested Actions"))
		for _, s := range r.SuggestedActions {
			fmt.Fprintf(&b, "   %s %s\n", style.Accent.Render("→"), s.Text)
		}
		fmt.Fprintln(&b)
	}

	if len(r.Warnings) > 0 {
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "   ⚠️  %s\n", style.Warn.Render(w))
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
