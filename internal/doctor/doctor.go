package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/Higangssh/homebutler/internal/service"
	"github.com/Higangssh/homebutler/internal/style"
	"github.com/Higangssh/homebutler/internal/watch"
	"github.com/charmbracelet/lipgloss"
)

const (
	SeverityPass = "pass"
	SeverityWarn = "warn"
	SeverityFail = "fail"
)

// Runner says who can carry a finding's Command out.
//
// #157 is the reason this exists: doctor hands an agent a command in a field
// named command, and for two of them no MCP tool can run it — so the agent's
// only honest move was to ask the operator to open a terminal, which is the
// situation homebutler exists to remove. Naming the runner lets it branch
// without reading the prose, and lets the one case that genuinely needs a
// human say so.
const (
	// RunnerMCP: an MCP tool runs this, and Tool names it.
	RunnerMCP = "mcp"
	// RunnerCLI: homebutler can do it, and no tool exposes it.
	RunnerCLI = "cli"
	// RunnerShell: not a homebutler command at all.
	RunnerShell = "shell"
)

// commandTools maps each command doctor prints to the MCP tool that runs it.
// Keys are the fixed part of the command; anything the finding appends, such
// as a container or endpoint name, follows.
var commandTools = map[string]string{
	"homebutler backup":                    "backup_create",
	"homebutler backup drill":              "backup_drill",
	"homebutler backup list":               "backup_list",
	"homebutler doctor":                    "doctor",
	"homebutler docker inspect":            "docker_inspect",
	"homebutler docker logs":               "docker_logs",
	"homebutler inventory scan":            "inventory_scan",
	"homebutler proxmox status --endpoint": "proxmox_status",
	"homebutler ps --sort cpu":             "processes",
	"homebutler ps --sort mem":             "processes",
	"homebutler report":                    "report",
	"homebutler status":                    "system_status",
	"homebutler watch history":             "watch_history",
	"homebutler notify test":               "notify_test",
}

// cliOnly are homebutler commands that no tool runs, each with the reason.
// A command here is a decision; a command in neither map is an oversight, and
// TestEveryDoctorCommandIsClassified is what turns that into a failure.
var cliOnly = map[string]string{
	// The unit records the path of the binary that installed it, so installing
	// through an agent running homebutler via npx or a container writes a unit
	// pointing at a cache path that later disappears. The service then dies
	// quietly and doctor reports the same finding again, with nothing to show
	// that anything was installed (#157).
	"homebutler watch install": "installs a service whose unit would record the wrong binary path when run through anything but the installed binary",
	// Same unit, same reason, and one more: reinstalling opens a port. An
	// agent deciding on its own which address the dashboard should answer on
	// is the decision serve install exists to make a person take.
	"homebutler serve install": "installs a service whose unit would record the wrong binary path when run through anything but the installed binary, and chooses what address a dashboard answers on",
}

// classifyCommand reports who can run command, and the tool when one can.
// ClassifyCommand says who can run a command: an MCP tool (and which), the
// CLI only, or a shell. Exported because report suggests commands too, and a
// second opinion about which tool runs `homebutler docker logs` is how the two
// start disagreeing in front of an agent.
func ClassifyCommand(command string) (runner, tool string) {
	return classifyCommand(command)
}

func classifyCommand(command string) (runner, tool string) {
	if command == "" {
		return "", ""
	}
	if !strings.HasPrefix(command, "homebutler ") {
		return RunnerShell, ""
	}
	// Longest key first, so "homebutler backup list" is not answered by
	// "homebutler backup".
	best := ""
	for prefix := range commandTools {
		if commandMatches(command, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best != "" {
		return RunnerMCP, commandTools[best]
	}
	for prefix := range cliOnly {
		if commandMatches(command, prefix) {
			return RunnerCLI, ""
		}
	}
	return "", ""
}

// commandMatches reports whether command is prefix, or prefix followed by an
// argument. It is not a plain HasPrefix so that a future "homebutler reporting"
// is not answered by the entry for "homebutler report".
func commandMatches(command, prefix string) bool {
	return command == prefix || strings.HasPrefix(command, prefix+" ")
}

// Finding is one actionable doctor result.
type Finding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Action   string `json:"action,omitempty"`
	Command  string `json:"command,omitempty"`
	// Runner and Tool say how Command can be carried out. Runner is always set
	// when Command is, so a caller never has to read an empty Tool as either
	// "no tool" or "not classified" — the ambiguity #108 settled for exit
	// codes, in a different field.
	Runner string `json:"runner,omitempty"`
	Tool   string `json:"tool,omitempty"`
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

	// BackupMaxTotal is the size at which an unbounded backup directory is
	// worth mentioning. Zero takes the default.
	BackupMaxTotal int64

	Strict bool
	Now    time.Time

	// DrillRecordsFn reads the drill history. Empty takes the real one.
	DrillRecordsFn func(string) ([]backup.DrillRecord, error)
}

func (o Options) drillRecords(backupDir string) ([]backup.DrillRecord, error) {
	if o.DrillRecordsFn != nil {
		return o.DrillRecordsFn(backupDir)
	}
	return backup.ListDrillRecords(backupDir)
}

// CollectFuncs allows tests to inject data sources.
type CollectFuncs struct {
	InventoryFns  inventory.CollectFuncs
	BackupListFn  func(string) ([]backup.ListEntry, error)
	SnapshotDir   string
	ProxmoxOpenFn func(config.ProxmoxConfig) (*proxmox.Client, error)

	// WatchDir holds the watch list. Empty takes the real one.
	WatchDir string
	// InspectFn returns a container's details. Injected so the socket check
	// is testable without a docker daemon.
	InspectFn func(string) (*docker.InspectResult, error)
	// WatchServiceFn reports whether a supervisor unit for watch is installed
	// and where it is. Injected so the check is testable without a systemd or
	// launchd on the machine running the tests.
	WatchServiceFn func() (bool, string)
	// ServeServiceFn reports the installed dashboard, for the same reason.
	ServeServiceFn func() InstalledDashboard
}

// InstalledDashboard is what a supervised `serve` says about itself, read out
// of the unit the supervisor runs rather than from a record kept beside it.
type InstalledDashboard struct {
	Installed bool
	Unit      string
	Host      string
	Port      int
	// Addressed is false for a unit that names no --host and --port. A unit
	// written by hand, or by a version that did not put them there, is one
	// this check cannot judge rather than one it should judge as loopback.
	Addressed bool
}

// DefaultCollectFuncs returns real doctor data sources.
func DefaultCollectFuncs() CollectFuncs {
	return CollectFuncs{
		InventoryFns:  inventory.DefaultCollectFuncs(),
		BackupListFn:  backup.List,
		SnapshotDir:   defaultSnapshotDir(),
		ProxmoxOpenFn: openProxmoxEndpoint,
	}
}

// openProxmoxEndpoint resolves the token and builds a client the same way the
// proxmox commands do, so doctor and proxmox status never disagree about what
// a configured endpoint accepts.
func openProxmoxEndpoint(endpoint config.ProxmoxConfig) (*proxmox.Client, error) {
	token, err := endpoint.TokenValue()
	if err != nil {
		return nil, err
	}
	return proxmox.New(proxmox.Options{
		Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: endpoint.TokenID, Token: token,
		Fingerprint: endpoint.Fingerprint, CAFile: endpoint.CAFile, Insecure: endpoint.Insecure, Timeout: endpoint.TimeoutDuration(),
	})
}

// Categories is every word a finding's Category can hold.
//
// A caller filtering on `category == "backup"` needs to know what else can
// arrive, and a vocabulary nobody wrote down is a set of strings that grows.
// The README documents these in the same shape as report's eight kinds, and
// TestCategoriesMatchTheReadme reads that table rather than a copy kept here.
func Categories() []string {
	return []string{
		"backup", "config", "docker", "exposure", "incomplete",
		"notifications", "overall", "proxmox", "report", "system", "watch",
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
	if fns.InspectFn == nil {
		fns.InspectFn = docker.Inspect
	}
	if fns.ProxmoxOpenFn == nil {
		fns.ProxmoxOpenFn = openProxmoxEndpoint
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
	checkDockerSocketMounts(r, inv, fns.InspectFn)
	checkProxmoxTrust(r, cfg)
	checkIncidentRetention(r, cfg, fns.WatchDir)
	checkWatching(r, fns.WatchDir, fns.WatchServiceFn)
	checkWatchNotifications(r, cfg, fns.WatchDir)
	checkConfigPermissions(r, cfg)
	checkDashboard(r, cfg, fns.ServeServiceFn)
	checkNotifications(r, cfg)
	checkReportBaseline(r, fns.SnapshotDir)
	checkProxmox(r, cfg, fns.ProxmoxOpenFn)

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
		r.add(SeverityWarn, "incomplete", "Doctor could not check everything", w, "Fix this first so doctor can give a complete answer.", "homebutler doctor")
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
		if !ports.IsPublicBind(p.Address) {
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

	checkDrills(r, backupDir, latest, opts)
	checkBackupSize(r, cfg, backupDir, len(entries), opts)
}

// checkDrills asks the one backup question the rest of this file cannot: has
// any of this ever been restored. Three of the findings above end by telling
// the operator to run a drill, and until now nothing looked at whether the
// advice was taken — a backup nobody has ever drilled looked exactly like one
// that passed an hour ago.
//
// Both findings are warnings. Never having drilled is the state every install
// starts in, and `--strict` in a cron job should not go red on day one for it.
func checkDrills(r *Result, backupDir string, latestArchive time.Time, opts Options) {
	records, err := opts.drillRecords(backupDir)
	if err != nil {
		r.add(SeverityWarn, "backup", "Could not check drill history", err.Error(),
			"Fix access to the backup directory, then run doctor again.", "homebutler backup list")
		return
	}

	latest, ok := backup.LatestDrill(records)
	if !ok {
		r.add(SeverityWarn, "backup", "No backup has ever been drilled",
			"Backups exist and none has been restored to see whether it comes back.",
			"Drill one app to find out whether the archive is worth having.", "homebutler backup drill --all")
		return
	}

	drilledAt, ok := backup.ParseDrillTime(latest)
	if !ok {
		r.add(SeverityWarn, "backup", "Could not read the drill history",
			"Drill records exist, but none had a valid timestamp.",
			"Run a drill to write a fresh record.", "homebutler backup drill --all")
		return
	}

	// The sharper of the two: what passed is not what you would restore.
	if drilledAt.Before(latestArchive) {
		r.add(SeverityWarn, "backup", "The newest backup has never been drilled",
			fmt.Sprintf("The last drill was %s before the newest archive was taken, so what passed is not what you would restore from.",
				roundDuration(latestArchive.Sub(drilledAt))),
			"Drill again so the verdict is about the archive you actually have.", "homebutler backup drill --all")
		return
	}

	if !latest.Passed {
		r.add(SeverityWarn, "backup", "The last drill did not come back",
			fmt.Sprintf("%s failed its drill %s ago.", latest.App, roundDuration(opts.Now.Sub(drilledAt))),
			"Take a fresh backup of that app and drill it again.", "homebutler backup")
	}
}

// checkBackupSize warns when the backup directory has no bound and has grown.
//
// Retention defaults to keeping everything, because a pruned backup can be the
// last copy of data that no longer exists. That default is only defensible if
// something says when the directory has outgrown what the operator expected,
// and nothing did: doctor checked that a backup was recent, which says nothing
// about a directory that has been growing since the day it was created.
//
// Nothing is reported while retention is configured, or while the directory is
// small. doctor's findings are things to look at, and a bounded directory is
// not one.
func checkBackupSize(r *Result, cfg *config.Config, backupDir string, count int, opts Options) {
	if cfg != nil && !cfg.ResolveBackupRetention().IsZero() {
		return
	}
	_, total, err := backup.DirUsage(backupDir)
	if err != nil {
		return
	}
	warnAt := opts.BackupMaxTotal
	if warnAt <= 0 {
		warnAt = defaultBackupWarnBytes
	}
	if total < warnAt {
		return
	}
	r.add(SeverityWarn, "backup", "Backups have no retention limit and the directory is large",
		fmt.Sprintf("%d archive(s) in %s, %s in total, and nothing will ever remove one.", count, backupDir, formatBytes(total)),
		"Set backup.retention.max_bytes or max_archives. homebutler keeps every backup by default, because one of them may be the last copy of something.",
		"homebutler backup list")
}

// defaultBackupWarnBytes is where an unbounded backup directory stops being
// something nobody needs to think about.
const defaultBackupWarnBytes = 20 << 30 // 20GB

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGT"[exp])
}

// dockerSocket is the path that turns a container into a host-root shell.
const dockerSocket = "/var/run/docker.sock"

// checkDockerSocketMounts reports a running container that can create
// containers on the host as root.
//
// homebutler installs one of these itself — the README says `install portainer`
// mounts the socket — and then never mentions it again. This costs one
// `docker inspect` per running container, which is why it stops at the first
// collector failure rather than retrying per container.
func checkDockerSocketMounts(r *Result, inv *inventory.Inventory, inspectFn func(string) (*docker.InspectResult, error)) {
	if inv == nil || inspectFn == nil {
		return
	}
	for _, c := range inv.Containers {
		if c.State != "running" {
			continue
		}
		details, err := inspectFn(c.Name)
		if err != nil {
			return
		}
		for _, m := range details.Mounts {
			if m.Source != dockerSocket {
				continue
			}
			r.add(SeverityWarn, "docker",
				fmt.Sprintf("%s mounts the Docker socket", c.Name),
				"A container with "+dockerSocket+" mounted can create containers on the host as root, "+
					"so it holds host root however unprivileged it looks. Some images need it; most do not.",
				"Confirm this container is one that needs it, and remove the mount if it is not.",
				"homebutler docker inspect "+c.Name)
			break
		}
	}
}

// checkProxmoxTrust reports an endpoint left on insecure: true.
//
// #62 settled that insecure should be "last and loud". It is last — the trust
// order is fingerprint, then CA file, then insecure — and it was loud nowhere:
// no runtime warning, no config validate finding, and until now no doctor
// check. A debugging session that was never undone is exactly what a periodic
// check is for.
func checkProxmoxTrust(r *Result, cfg *config.Config) {
	if cfg == nil {
		return
	}
	for _, p := range cfg.Proxmox {
		if !p.Insecure {
			continue
		}
		r.add(SeverityWarn, "proxmox",
			fmt.Sprintf("Proxmox endpoint %q accepts any certificate", p.Name),
			"insecure: true disables certificate verification entirely, so anything on the network path "+
				"can read the API token and answer for the endpoint.",
			"Pin the certificate with fingerprint, or trust its issuer with ca_file, and remove insecure.",
			"homebutler proxmox status --endpoint "+p.Name)
	}
}

// incidentDirNearCap is the fraction of the retention limit at which the
// directory is worth mentioning. Pruning is not a failure — it is the setting
// working — but history disappearing is worth knowing about before it is the
// history someone wanted.
const incidentDirNearCap = 0.8

func checkIncidentRetention(r *Result, cfg *config.Config, dir string) {
	if cfg == nil {
		return
	}
	keep := cfg.Watch.Retention.MaxIncidents
	if keep <= 0 {
		return // unlimited by explicit request
	}
	if dir == "" {
		d, err := watch.WatchDir()
		if err != nil {
			return
		}
		dir = d
	}
	refs, err := watch.ListIncidentRefs(dir)
	if err != nil || len(refs) < int(float64(keep)*incidentDirNearCap) {
		return
	}
	r.add(SeverityWarn, "watch",
		fmt.Sprintf("%d of %d incidents kept", len(refs), keep),
		"The oldest incidents are discarded once the limit is reached, so history from before that point is gone.",
		"Raise the limit if you want to keep more, or leave it if the recent history is enough.",
		"homebutler watch history")
}

// checkWatching reports a watch list that nothing is installed to poll.
//
// A list with entries and no supervisor is the state that makes every other
// monitoring feature silent: no incidents recorded, no notifications sent,
// nothing to compare a report against. It is also the only place a user finds
// out that `watch install` exists.
//
// It checks whether a unit is installed, not whether it is running. A stopped
// unit is a state this cannot see, and claiming otherwise would be worse than
// saying less — asking systemd or launchd on every doctor run is a side
// effect this command should not grow quietly.
func checkWatching(r *Result, dir string, installedFn func() (bool, string)) {
	if installedFn == nil {
		installedFn = func() (bool, string) { return service.InstalledUnit(service.Watch) }
	}
	if dir == "" {
		d, err := watch.WatchDir()
		if err != nil {
			return
		}
		dir = d
	}

	targets, err := watch.LoadTargets(dir)
	if err != nil || len(targets) == 0 {
		return
	}

	installed, where := installedFn()
	if installed {
		r.add(SeverityPass, "watch",
			fmt.Sprintf("%d target(s) watched by an installed service", len(targets)),
			"Unit: "+where, "", "")
		return
	}

	r.add(SeverityWarn, "watch",
		fmt.Sprintf("%d target(s) on the watch list and no service installed to check them", len(targets)),
		"Nothing is polling them, so a restart records no incident and sends no notification. "+
			"This checks whether a service is installed, not whether it is running.",
		"Install the watch service so monitoring survives logout and reboot.",
		"homebutler watch install")
}

// checkWatchNotifications reports a watch list that will tell nobody.
//
// Three gates stand between an incident and a message and two are closed by
// default: watch.notify.enabled is false, and notify_on is "flapping", under
// which a single restart is not reported. checkNotifications covers the third
// — no channel configured at all — so this stays quiet in that case rather
// than making one gap into two findings.
//
// The person this fails is the one who did the work: configured a channel,
// tested it, installed the service, and is covered except for a flag they
// were never told about.
func checkWatchNotifications(r *Result, cfg *config.Config, dir string) {
	if cfg == nil || len(cfg.Notify.EnabledChannels()) == 0 {
		return
	}
	if dir == "" {
		d, err := watch.WatchDir()
		if err != nil {
			return
		}
		dir = d
	}
	targets, err := watch.LoadTargets(dir)
	if err != nil || len(targets) == 0 {
		return
	}

	notify := cfg.Watch.Notify
	if !notify.Enabled {
		r.add(SeverityWarn, "notifications",
			"Notifications are configured and switched off for watch",
			"A channel is set up and reachable, but watch.notify.enabled is false, so an incident is written "+
				"to disk and printed to the terminal and nothing is sent anywhere.",
			"Turn it on if you want to hear about incidents away from this machine.",
			"homebutler notify test")
		return
	}

	r.add(SeverityPass, "notifications",
		"Watch notifications are on",
		notifyOnMeans(notify.NotifyOn), "", "")
}

// notifyOnMeans says what the setting will and will not send, because the
// value alone does not tell an operator which incidents reach them.
func notifyOnMeans(mode string) string {
	switch mode {
	case "all":
		return "notify_on: all — every incident is sent, including a single restart."
	case "incident":
		return "notify_on: incident — every incident is sent; repeated restarts are not sent again as flapping."
	case "off":
		return "notify_on: off — nothing is sent, despite notifications being enabled."
	default:
		return "notify_on: " + mode + " — repeated restarts are sent as flapping. A single restart is recorded but not sent."
	}
}

// checkConfigPermissions surfaces the refusal Load would make, before the
// user meets it as homebutler appearing to be broken.
func checkConfigPermissions(r *Result, cfg *config.Config) {
	if cfg == nil || cfg.Path == "" {
		return
	}
	perm, tooOpen := config.PermissionProblem(cfg.Path, cfg)
	if !tooOpen {
		return
	}
	r.add(SeverityFail, "config",
		fmt.Sprintf("Config holds plaintext secrets and is readable by others (%04o)", perm),
		cfg.Path+" can be read by any user on this machine, and homebutler will refuse to load it.",
		"Restrict it to your account.",
		"chmod 600 "+cfg.Path)
}

// checkDashboard answers the question `serve install` cannot.
//
// Installing refuses to bind past loopback with no token configured, and that
// is a decision made once. The config file outlives it: remove web.token
// afterwards — by editing it, or by restoring an older copy — and a dashboard
// installed on 0.0.0.0 with authentication goes on running without it. Nothing
// fails. The service is not restarted, the write routes stop being registered
// rather than start refusing, and from outside it is indistinguishable from a
// dashboard that was always read-only.
//
// What it deliberately does not do is ask whether the service is up. That
// would mean opening a socket, and doctor is the command people run when
// something is already wrong — the same reason checkWatching answers about
// the unit file rather than about systemd.
func checkDashboard(r *Result, cfg *config.Config, serveFn func() InstalledDashboard) {
	if serveFn == nil {
		serveFn = defaultInstalledDashboard
	}
	dashboard := serveFn()
	if !dashboard.Installed {
		return
	}

	// A unit that states no address is one this cannot judge. Guessing
	// loopback would be the reassuring guess, and reassuring is the wrong
	// direction to guess in.
	if !dashboard.Addressed {
		r.add(SeverityWarn, "exposure",
			"An installed dashboard does not say what it binds",
			dashboard.Unit+" names no --host and --port, so homebutler cannot tell whether it is reachable from the network.",
			"Reinstall it so the unit records its address.",
			"homebutler serve install --force")
		return
	}

	address := fmt.Sprintf("%s:%d", dashboard.Host, dashboard.Port)
	token := ""
	if cfg != nil {
		token = cfg.Web.Token
	}

	if token == "" && !loopbackHost(dashboard.Host) {
		r.add(SeverityFail, "exposure",
			"The installed dashboard answers the network with no token",
			fmt.Sprintf("It is running on %s and %s has no web.token, so anyone who can reach this machine gets the dashboard. serve install refuses this, which means the token was removed after it was installed.", address, configPathFor(cfg)),
			"Set web.token, or reinstall it on 127.0.0.1 — which one is right depends on whether it is meant to be reachable.",
			"homebutler serve install --host 127.0.0.1 --force")
		return
	}

	detail := "Reachable only from this machine."
	if !loopbackHost(dashboard.Host) {
		detail = "Reachable from the network, and web.token is set."
	}
	r.add(SeverityPass, "exposure",
		"Dashboard installed on "+address, detail, "", "")
}

// loopbackHost reports whether binding host exposes the dashboard only to the
// machine it runs on. The same four spellings cmd/serve.go refuses on.
func loopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

func configPathFor(cfg *config.Config) string {
	if cfg == nil || cfg.Path == "" {
		return "the config file"
	}
	return cfg.Path
}

// defaultInstalledDashboard reads the unit the supervisor actually runs. The
// address comes out of its arguments rather than from anything written
// alongside, so this cannot report a port nothing is bound to.
func defaultInstalledDashboard() InstalledDashboard {
	unit := service.Serve("", 0)
	installed, path := service.InstalledUnit(unit)
	if !installed {
		return InstalledDashboard{}
	}
	kind, err := service.Detect()
	if err != nil {
		return InstalledDashboard{Installed: true, Unit: path}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return InstalledDashboard{Installed: true, Unit: path}
	}
	host, port, ok := service.Address(service.UnitArgs(kind, string(content)))
	return InstalledDashboard{Installed: true, Unit: path, Host: host, Port: port, Addressed: ok}
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

// checkProxmox diagnoses each configured Proxmox endpoint with read-only
// requests. It reports one finding per endpoint and keeps configuration, TLS,
// authentication, authorization, response, and transport failures distinguishable rather than
// collapsing them into a single "unavailable" result, per #105 and #111.
func checkProxmox(r *Result, cfg *config.Config, openFn func(config.ProxmoxConfig) (*proxmox.Client, error)) {
	if cfg == nil || len(cfg.Proxmox) == 0 {
		return
	}
	for _, endpoint := range cfg.Proxmox {
		command := fmt.Sprintf("homebutler proxmox status --endpoint %q", endpoint.Name)
		client, err := openFn(endpoint)
		if err != nil {
			r.add(SeverityFail, "proxmox", fmt.Sprintf("Proxmox endpoint %q could not be configured", endpoint.Name), err.Error(), proxmoxFailureAction(err), command)
			continue
		}
		checkProxmoxEndpoint(r, endpoint.Name, client, command)
	}
}

// checkProxmoxEndpoint calls the same DefaultView the proxmox status command
// renders, so the two never disagree about what a token can reach: an empty
// resources collector is a failed collector here exactly as it is there,
// not a doctor-only pass.
func checkProxmoxEndpoint(r *Result, name string, client *proxmox.Client, command string) {
	view, _ := client.DefaultView(context.Background())

	allFailed := view.CollectorFailed(proxmox.CollectorVersion) &&
		view.CollectorFailed(proxmox.CollectorCluster) &&
		view.CollectorFailed(proxmox.CollectorResources)

	switch {
	case allFailed:
		r.add(SeverityFail, "proxmox", fmt.Sprintf("Proxmox endpoint %q has no readable collectors", name), strings.Join(view.Warnings, "; "), proxmoxFailureAction(view.FirstErr), command)
	case len(view.Failed) > 0:
		r.add(SeverityWarn, "proxmox", fmt.Sprintf("Proxmox endpoint %q is only partially readable", name), strings.Join(view.Warnings, "; "), proxmoxFailureAction(view.FirstErr), command)
	default:
		r.add(SeverityPass, "proxmox", fmt.Sprintf("Proxmox endpoint %q is fully readable", name), "Version, cluster status, and resources all responded.", "", "")
	}
}

// proxmoxFailureAction turns a classified Proxmox error into what the
// operator can safely inspect or correct. It must never suggest widening a
// token to Administrator just to make a check pass (#105).
func proxmoxFailureAction(err error) string {
	switch proxmox.Classify(err) {
	case proxmox.FailureConfiguration:
		return "Check the endpoint configuration fields: host, port, timeout, token ID and token, fingerprint, and CA file."
	case proxmox.FailureTLS:
		return "Check the configured certificate trust: the fingerprint or CA file against the endpoint's actual certificate."
	case proxmox.FailureAuthentication:
		return "Check the token ID, the token value, and the token file's ownership and permissions; the token may be missing, unreadable, or revoked."
	case proxmox.FailureAuthorization:
		return "Check that the read-only PVEAuditor role is applied to both the API user and the privilege-separated token. Do not grant Administrator to make this pass."
	case proxmox.FailureResponse:
		return "Check that the configured host and port serve the Proxmox API, not a reverse-proxy error page."
	case proxmox.FailureTransport:
		return "Check network reachability to the configured host and port, and any firewall rules in between."
	default:
		return "Run homebutler proxmox status for the full error."
	}
}

func (r *Result) add(severity, category, title, detail, action, command string) {
	runner, tool := classifyCommand(command)
	r.Findings = append(r.Findings, Finding{
		Severity: severity, Category: category, Title: title, Detail: detail,
		Action: action, Command: command, Runner: runner, Tool: tool,
	})
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
// severityStyle maps a severity onto the shared palette. Unknown values fall
// back to the plain label style rather than picking an arbitrary colour.
func severityStyle(severity string) lipgloss.Style {
	switch severity {
	case SeverityPass:
		return style.OK
	case SeverityWarn:
		return style.Warn
	case SeverityFail:
		return style.Fail
	default:
		return style.Label
	}
}

func FormatHuman(r *Result) string {
	var b strings.Builder
	statusIcon := map[string]string{SeverityPass: "✅", SeverityWarn: "⚠️", SeverityFail: "❌"}[r.Status]
	if statusIcon == "" {
		statusIcon = "•"
	}

	fmt.Fprintf(&b, "🩺 %s\n", style.Title.Render("Homebutler Doctor — "+r.ServerName))
	fmt.Fprintf(&b, "   %s\n\n", style.Dim.Render(r.Timestamp))
	fmt.Fprintf(&b, "%s %s  %s\n\n",
		statusIcon,
		severityStyle(r.Status).Bold(true).Render("Status: "+strings.ToUpper(r.Status)),
		style.Dim.Render(fmt.Sprintf("· pass %d / warn %d / fail %d", r.Summary.Pass, r.Summary.Warn, r.Summary.Fail)),
	)

	for _, f := range r.Findings {
		icon := map[string]string{SeverityPass: "✅", SeverityWarn: "⚠️", SeverityFail: "❌"}[f.Severity]
		fmt.Fprintf(&b, "%s %s %s\n",
			icon,
			style.Accent.Render("["+f.Category+"]"),
			style.Title.Render(f.Title))
		if f.Detail != "" {
			fmt.Fprintf(&b, "   %s\n", style.Dim.Render(f.Detail))
		}
		if f.Action != "" {
			fmt.Fprintf(&b, "   %s %s\n", style.Accent.Render("→"), f.Action)
		}
		if f.Command != "" {
			fmt.Fprintf(&b, "   %s\n", style.Label.Render("$ "+f.Command))
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}
