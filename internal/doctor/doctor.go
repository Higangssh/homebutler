package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
)

const (
	SeverityPass = "pass"
	SeverityWarn = "warn"
	SeverityFail = "fail"
)

// Finding is one actionable doctor result.
type Finding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Action   string `json:"action,omitempty"`
	Command  string `json:"command,omitempty"`
}

// Result is the structured output of a doctor run.
type Result struct {
	Timestamp  string    `json:"timestamp"`
	ServerName string    `json:"server_name"`
	Status     string    `json:"status"`
	Summary    Summary   `json:"summary"`
	Findings   []Finding `json:"findings"`
}

// Summary counts findings by severity.
type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// Options controls doctor behavior.
type Options struct {
	BackupMaxAge time.Duration
	Strict       bool
	Now          time.Time
}

// CollectFuncs allows tests to inject data sources.
type CollectFuncs struct {
	InventoryFns inventory.CollectFuncs
	BackupListFn func(string) ([]backup.ListEntry, error)
	SnapshotDir  string
}

// DefaultCollectFuncs returns real doctor data sources.
func DefaultCollectFuncs() CollectFuncs {
	return CollectFuncs{
		InventoryFns: inventory.DefaultCollectFuncs(),
		BackupListFn: backup.List,
		SnapshotDir:  defaultSnapshotDir(),
	}
}

// Run performs a read-only health and readiness diagnosis.
func Run(cfg *config.Config, fns CollectFuncs, opts Options) (*Result, error) {
	if opts.BackupMaxAge == 0 {
		opts.BackupMaxAge = 7 * 24 * time.Hour
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if fns.BackupListFn == nil {
		fns.BackupListFn = backup.List
	}
	if fns.SnapshotDir == "" {
		fns.SnapshotDir = defaultSnapshotDir()
	}

	inv, err := inventory.Collect(cfg, fns.InventoryFns)
	if err != nil {
		return nil, fmt.Errorf("collecting inventory: %w", err)
	}

	r := &Result{
		Timestamp:  opts.Now.UTC().Format(time.RFC3339),
		ServerName: inv.ServerName,
		Findings:   []Finding{},
	}

	checkCollectionWarnings(r, inv)
	checkSystem(r, cfg, inv)
	checkContainers(r, inv)
	checkPublicPorts(r, inv.Ports)
	checkBackups(r, cfg, fns.BackupListFn, opts)
	checkNotifications(r, cfg)
	checkReportBaseline(r, fns.SnapshotDir)

	if len(r.Findings) == 0 {
		r.Findings = append(r.Findings, Finding{
			Severity: SeverityPass,
			Category: "overall",
			Title:    "No obvious issues found",
			Detail:   "System resources, Docker state, exposed ports, backups, and notification readiness look acceptable.",
		})
	}

	r.Summary = summarize(r.Findings)
	r.Status = overallStatus(r.Summary)
	return r, nil
}

func checkCollectionWarnings(r *Result, inv *inventory.Inventory) {
	for _, w := range inv.Warnings {
		r.add(SeverityWarn, "collection", "Doctor could not check everything", w, "Fix this first so doctor can give a complete answer.", "homebutler doctor")
	}
}

func checkSystem(r *Result, cfg *config.Config, inv *inventory.Inventory) {
	if inv.System == nil {
		r.add(SeverityWarn, "system", "Could not read system health", "Doctor could not inspect CPU, memory, or disks.", "Run status to see the underlying error before trusting this server.", "homebutler status")
		return
	}

	limits := config.AlertConfig{CPU: 90, Memory: 85, Disk: 90}
	if cfg != nil {
		limits = cfg.Alerts
	}
	if limits.CPU <= 0 {
		limits.CPU = 90
	}
	if limits.Memory <= 0 {
		limits.Memory = 85
	}
	if limits.Disk <= 0 {
		limits.Disk = 90
	}

	if inv.System.CPU.UsagePercent >= limits.CPU {
		r.add(SeverityWarn, "system", "CPU is unusually busy", fmt.Sprintf("CPU is %.0f%%, threshold is %.0f%%.", inv.System.CPU.UsagePercent, limits.CPU), "Check what is using CPU before restarting random services.", "homebutler ps --sort cpu")
	}
	if inv.System.Memory.Percent >= limits.Memory {
		r.add(SeverityFail, "system", "Memory is almost full", fmt.Sprintf("Memory is %.0f%%, threshold is %.0f%%.", inv.System.Memory.Percent, limits.Memory), "Find memory-heavy processes now; otherwise containers may be killed unexpectedly.", "homebutler ps --sort mem")
	}
	for _, d := range inv.System.Disks {
		if d.Percent >= limits.Disk {
			r.add(SeverityFail, "system", "Disk is almost full", fmt.Sprintf("Disk %s is %.0f%% full, threshold is %.0f%%.", d.Mount, d.Percent, limits.Disk), "Free space before apps, databases, or backups start failing.", "homebutler status")
		}
	}
}

func checkContainers(r *Result, inv *inventory.Inventory) {
	var stopped []string
	for _, c := range inv.Containers {
		if c.State != "running" {
			stopped = append(stopped, c.Name)
		}
	}
	if len(stopped) == 0 {
		return
	}
	sort.Strings(stopped)
	command := "homebutler docker logs " + stopped[0]
	r.add(SeverityWarn, "docker", fmt.Sprintf("%d container(s) are stopped", len(stopped)), strings.Join(stopped, ", "), "Check the logs before restarting; some stopped containers may be intentional.", command)
}

func checkPublicPorts(r *Result, pp []ports.PortInfo) {
	var exposed []string
	seen := map[string]bool{}
	for _, p := range pp {
		if !isPublicBind(p.Address) {
			continue
		}
		label := strings.TrimSpace(fmt.Sprintf("%s/%s %s", p.Port, p.Protocol, p.Process))
		if label == "/" || label == "" {
			label = p.Port
		}
		if !seen[label] {
			exposed = append(exposed, label)
			seen[label] = true
		}
	}
	if len(exposed) == 0 {
		return
	}
	sort.Strings(exposed)
	r.add(SeverityWarn, "exposure", fmt.Sprintf("%d port(s) are listening on all interfaces", len(exposed)), strings.Join(exposed, ", "), "Make sure each one is intentional and protected by firewall, reverse proxy, or login where needed.", "homebutler inventory scan")
}

func checkBackups(r *Result, cfg *config.Config, listFn func(string) ([]backup.ListEntry, error), opts Options) {
	backupDir := ""
	if cfg != nil {
		backupDir = cfg.ResolveBackupDir()
	}
	if backupDir == "" {
		backupDir = defaultBackupDir()
	}

	entries, err := listFn(backupDir)
	if err != nil {
		r.add(SeverityWarn, "backup", "Could not check backups", err.Error(), "Fix backup directory access, then run doctor again.", "homebutler backup list")
		return
	}
	if len(entries) == 0 {
		r.add(SeverityWarn, "backup", "No backups found", fmt.Sprintf("No .tar.gz backups found in %s.", backupDir), "Create your first backup, then verify at least one important app with a drill.", "homebutler backup")
		return
	}

	latest, ok := latestBackup(entries)
	if !ok {
		r.add(SeverityWarn, "backup", "Could not read backup timestamps", "Backups exist, but none had a valid created_at timestamp.", "Run backup list to inspect the files, then create a fresh backup if needed.", "homebutler backup list")
		return
	}
	age := opts.Now.Sub(latest)
	if age > opts.BackupMaxAge {
		r.add(SeverityWarn, "backup", "Latest backup is older than expected", fmt.Sprintf("Latest backup is %s old; expected within %s.", roundDuration(age), roundDuration(opts.BackupMaxAge)), "Run a fresh backup. If this app matters, follow up with a backup drill.", "homebutler backup")
	}
}

func checkNotifications(r *Result, cfg *config.Config) {
	if cfg == nil || len(cfg.Notify.EnabledChannels()) == 0 {
		r.add(SeverityWarn, "notifications", "No notification channel configured", "If something crashes, homebutler can only report it locally.", "Configure Telegram, Slack, Discord, or webhook notifications if you want alerts away from the terminal.", "homebutler notify test")
	}
}

func checkReportBaseline(r *Result, snapshotDir string) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			r.add(SeverityWarn, "report", "No report baseline yet", "Doctor did not find previous report snapshots.", "Run report once so homebutler can notice what changes later.", "homebutler report")
		}
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "snapshot_") && strings.HasSuffix(e.Name(), ".json") {
			return
		}
	}
	r.add(SeverityWarn, "report", "No report baseline yet", "Snapshot directory exists, but no report snapshots were found.", "Run report once so homebutler can notice what changes later.", "homebutler report")
}

func (r *Result) add(severity, category, title, detail, action, command string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Category: category, Title: title, Detail: detail, Action: action, Command: command})
}

func summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Severity {
		case SeverityFail:
			s.Fail++
		case SeverityWarn:
			s.Warn++
		case SeverityPass:
			s.Pass++
		}
	}
	return s
}

func overallStatus(s Summary) string {
	if s.Fail > 0 {
		return SeverityFail
	}
	if s.Warn > 0 {
		return SeverityWarn
	}
	return SeverityPass
}

func isPublicBind(addr string) bool {
	addr = strings.Trim(addr, "[]")
	switch addr {
	case "*", "0.0.0.0", "::", "":
		return true
	default:
		return false
	}
}

func latestBackup(entries []backup.ListEntry) (time.Time, bool) {
	var latest time.Time
	for _, e := range entries {
		t, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err != nil {
			continue
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}
	return latest, !latest.IsZero()
}

func roundDuration(d time.Duration) string {
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	d = d.Round(time.Hour)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 && hours > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	return d.String()
}

func defaultBackupDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".homebutler", "backups")
	}
	return filepath.Join(home, ".homebutler", "backups")
}

func defaultSnapshotDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".homebutler", "reports", "snapshots")
	}
	return filepath.Join(home, ".homebutler", "reports", "snapshots")
}

// FormatHuman renders the doctor result as a concise CLI report.
func FormatHuman(r *Result) string {
	var b strings.Builder
	statusIcon := map[string]string{SeverityPass: "✅", SeverityWarn: "⚠️", SeverityFail: "❌"}[r.Status]
	if statusIcon == "" {
		statusIcon = "•"
	}

	fmt.Fprintf(&b, "🩺 Homebutler Doctor — %s\n", r.ServerName)
	fmt.Fprintf(&b, "   %s\n\n", r.Timestamp)
	fmt.Fprintf(&b, "%s Status: %s  ·  pass %d / warn %d / fail %d\n\n", statusIcon, strings.ToUpper(r.Status), r.Summary.Pass, r.Summary.Warn, r.Summary.Fail)

	for _, f := range r.Findings {
		icon := map[string]string{SeverityPass: "✅", SeverityWarn: "⚠️", SeverityFail: "❌"}[f.Severity]
		fmt.Fprintf(&b, "%s [%s] %s\n", icon, f.Category, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, "   %s\n", f.Detail)
		}
		if f.Action != "" {
			fmt.Fprintf(&b, "   → %s\n", f.Action)
		}
		if f.Command != "" {
			fmt.Fprintf(&b, "   $ %s\n", f.Command)
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}
