package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/util"
	"github.com/spf13/cobra"
)

// fanOut is one command that --all can run on every server.
type fanOut struct {
	// local answers with what `homebutler <command> --json` prints on this
	// machine, for a server marked local: true.
	local func(c *config.Config, cmd *cobra.Command) (any, error)
	// summary is the rest of that server's line in the human output, read
	// from its JSON answer. A field the answer did not carry is shown as
	// missing, never as zero: a machine that was not measured is not idle.
	summary func(data []byte) (string, error)
	// refuse, when set, says why this invocation of the command cannot fan
	// out even though the command can.
	refuse func(cmd *cobra.Command) error
	// remoteDrop names flags kept off the remote command line, for a flag
	// whose effect --all applies itself.
	remoteDrop []string
	// verdict, when set, is the run's error once every line is printed.
	verdict func(cmd *cobra.Command, results []serverResult) error
	// describesHost marks a command whose local answer is a reading of the
	// machine this binary runs on. Inside a container that machine is the
	// container, so a local: true server is refused rather than given its
	// numbers under the server's name. The docker commands reach whatever
	// daemon the socket leads to and backup list reads the volume, so neither
	// is marked; the set is the one the MCP server refuses.
	describesHost bool
	icon          string
}

// fanOuts is every command --all runs, keyed by its path below the root.
//
// Anything else is refused before a server is asked. The rest either change
// the machine — `docker restart` or `backup` on every server at once is not
// something to reach by adding a flag — or read one named thing on one
// server, and a container name means nothing on the others.
var fanOuts = map[string]fanOut{
	"status": {
		describesHost: true,
		local: func(*config.Config, *cobra.Command) (any, error) {
			info, err := system.Status()
			if err != nil {
				return nil, err
			}
			return system.Document(info), nil
		},
		summary: summarizeStatus,
	},
	"alerts": {
		describesHost: true,
		local: func(c *config.Config, _ *cobra.Command) (any, error) {
			return alerts.Check(&config.AlertConfig{CPU: c.Alerts.CPU, Memory: c.Alerts.Memory, Disk: c.Alerts.Disk})
		},
		summary: summarizeAlerts,
		refuse: func(cmd *cobra.Command) error {
			if watch, _ := cmd.Flags().GetBool("watch"); watch {
				return errors.New("alerts --watch does not run with --all: it never returns, so no server would ever answer. Use --server <name>")
			}
			return nil
		},
	},
	"docker list": {
		local:   func(*config.Config, *cobra.Command) (any, error) { return docker.List() },
		summary: summarizeDockerList,
	},
	"docker stats": {
		local:   func(*config.Config, *cobra.Command) (any, error) { return docker.Stats() },
		summary: summarizeDockerStats,
	},
	"ports": {
		describesHost: true,
		local: func(*config.Config, *cobra.Command) (any, error) {
			result, err := ports.List()
			if err != nil {
				return nil, err
			}
			// The same attribution `ports` does, so this server's answer is
			// the one `ports --json` gives here.
			if containers, dockerErr := docker.List(); dockerErr == nil {
				result.Ports = inventory.AttributePorts(result.Ports, containers)
			}
			return result.Ports, nil
		},
		summary: summarizePorts,
	},
	"doctor": {
		describesHost: true,
		local: func(c *config.Config, cmd *cobra.Command) (any, error) {
			maxAge, _ := cmd.Flags().GetDuration("backup-max-age")
			strict, _ := cmd.Flags().GetBool("strict")
			return doctor.Run(c, doctor.DefaultCollectFuncs(), doctor.Options{BackupMaxAge: maxAge, Strict: strict})
		},
		summary: summarizeDoctor,
		// --strict is a verdict on the whole run. Sent to each server it
		// turned a warning there into "remote command failed" and its JSON
		// into an error message, so it is applied here to every answer.
		remoteDrop: []string{"--strict"},
		verdict:    doctorStrictVerdict,
		icon:       "🩺",
	},
	"processes": {
		describesHost: true,
		local: func(_ *config.Config, cmd *cobra.Command) (any, error) {
			limit, _ := cmd.Flags().GetInt("limit")
			sortBy, _ := cmd.Flags().GetString("sort")
			return system.ListProcesses(limit, sortBy)
		},
		summary: summarizeProcesses,
	},
	"report": {
		describesHost: true,
		local: func(c *config.Config, cmd *cobra.Command) (any, error) {
			keep, _ := cmd.Flags().GetInt("keep")
			noSave, _ := cmd.Flags().GetBool("no-save")
			return report.Run(c, report.DefaultCollectFuncs(), report.Options{Keep: keep, NoSave: noSave})
		},
		summary: summarizeReport,
	},
	"backup list": {
		local: func(c *config.Config, _ *cobra.Command) (any, error) {
			return backup.List(c.ResolveBackupDir())
		},
		summary: summarizeBackupList,
	},
	"network scan": {
		describesHost: true,
		local: func(*config.Config, *cobra.Command) (any, error) {
			return network.ScanWithTimeout(30 * time.Second)
		},
		summary: summarizeNetworkScan,
	},
}

// errContainerCannotSeeHost is system.ErrContainerCannotSeeHost as a reader
// sees it: the sentence the dashboard and the MCP server give, which names
// both ways out.
var errContainerCannotSeeHost error = containerCannotSeeHost{}

type containerCannotSeeHost struct{}

func (containerCannotSeeHost) Error() string { return system.ContainerCannotSeeHost }
func (containerCannotSeeHost) Unwrap() error { return system.ErrContainerCannotSeeHost }

// Seams for tests: the far side of each kind of server.
var (
	remoteRun = remote.Run
	localRun  = func(f fanOut, c *config.Config, cmd *cobra.Command) ([]byte, error) {
		v, err := f.local(c, cmd)
		if err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
)

// fanOutFor finds the command args name and what --all does with it.
func fanOutFor(args []string) (fanOut, *cobra.Command, error) {
	cmd, _, err := rootCmd.Find(args)
	if err != nil || cmd == rootCmd {
		return fanOut{}, nil, fmt.Errorf("--all needs a command. It runs: %s", strings.Join(fanOutNames(), ", "))
	}
	name := strings.TrimPrefix(cmd.CommandPath(), rootCmd.Name()+" ")
	f, ok := fanOuts[name]
	if !ok {
		return fanOut{}, nil, fmt.Errorf("%s does not run with --all; use --server <name> for one server. --all runs: %s", name, strings.Join(fanOutNames(), ", "))
	}
	if f.refuse != nil {
		if err := f.refuse(cmd); err != nil {
			return fanOut{}, nil, err
		}
	}
	return f, cmd, nil
}

func fanOutNames() []string {
	names := make([]string, 0, len(fanOuts))
	for name := range fanOuts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// jsonArgs is args asking for JSON exactly once, whatever the user typed. The
// human output is built from each server's JSON answer, so a server asked
// without --json answers in text that no summary can read.
func jsonArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "--json" || strings.HasPrefix(a, "--json=") {
			continue
		}
		out = append(out, a)
	}
	return append(out, "--json")
}

// runAllServers executes a command on all configured servers in parallel.
func runAllServers(c *config.Config, args []string, jsonOut bool) error {
	return fanOutAll(os.Stdout, c, args, jsonOut)
}

func fanOutAll(w io.Writer, c *config.Config, args []string, jsonOut bool) error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("no servers configured. Add servers to your config file")
	}
	f, cmd, err := fanOutFor(args)
	if err != nil {
		return err
	}

	remoteArgs := jsonArgs(forwardedArgs(cmd, args, f.remoteDrop...))
	results := make([]serverResult, len(c.Servers))
	var wg sync.WaitGroup

	for i, srv := range c.Servers {
		wg.Add(1)
		go func(idx int, server config.ServerConfig) {
			defer wg.Done()
			result := serverResult{Server: server.Name}

			var out []byte
			var err error
			switch {
			case server.Local && f.describesHost && system.InContainer():
				err = errContainerCannotSeeHost
			case server.Local:
				out, err = localRun(f, c, cmd)
			default:
				out, err = remoteRun(&server, remoteArgs...)
			}
			if err != nil {
				result.Error = util.FormatError(err, verboseOutput || jsonOut)
			} else {
				result.Data = json.RawMessage(out)
			}

			results[idx] = result
		}(i, srv)
	}

	wg.Wait()

	if jsonOut {
		if err := writeJSON(w, results); err != nil {
			return err
		}
		return fanOutVerdict(f, cmd, results)
	}
	icon := f.icon
	if icon == "" {
		icon = "📡"
	}
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(w, "❌ %-12s %s\n", r.Server, r.Error)
			continue
		}
		line, err := f.summary(r.Data)
		if err != nil {
			fmt.Fprintf(w, "⚠️  %-12s answered with something this version cannot read: %v\n", r.Server, err)
			continue
		}
		fmt.Fprintf(w, "%s %-12s %s\n", icon, r.Server, line)
	}
	return fanOutVerdict(f, cmd, results)
}

// fanOutVerdict fails the run when any server could not be asked or its
// command failed there. Every server's line has already been printed; this
// is what a script or cron job reads. A command with its own verdict gives
// that one instead when it fails, so a run is never reported twice.
func fanOutVerdict(f fanOut, cmd *cobra.Command, results []serverResult) error {
	if f.verdict != nil {
		if err := f.verdict(cmd, results); err != nil {
			return err
		}
	}
	var failed []string
	for _, r := range results {
		if r.Error != "" {
			failed = append(failed, r.Server)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	noun := "servers"
	if len(results) == 1 {
		noun = "server"
	}
	return fmt.Errorf("%s failed on %d of %d %s: %s", commandPath(cmd), len(failed), len(results), noun, strings.Join(failed, ", "))
}

// doctorStrictVerdict is doctor --strict across servers: the run fails when
// any server did not pass, including one that could not be asked or read.
func doctorStrictVerdict(cmd *cobra.Command, results []serverResult) error {
	if strict, _ := cmd.Flags().GetBool("strict"); !strict {
		return nil
	}
	var failed []string
	for _, r := range results {
		var d struct {
			Status string `json:"status"`
		}
		if r.Error != "" || json.Unmarshal(r.Data, &d) != nil || d.Status != doctor.SeverityPass {
			failed = append(failed, r.Server)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("doctor did not pass on %d of %d servers: %s", len(failed), len(results), strings.Join(failed, ", "))
}

// serverResult holds the result from a single server.
type serverResult struct {
	Server string          `json:"server"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

var errNoFields = errors.New("none of the fields this command answers with")

// count is n of a noun, in English: "1 process", "2 processes".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "s") {
		return fmt.Sprintf("%d %ses", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// percent renders a reading, or a dash where the answer had none.
func percent(v *float64) string {
	if v == nil {
		return "    -"
	}
	return fmt.Sprintf("%4.0f%%", *v)
}

func summarizeStatus(data []byte) (string, error) {
	type reading struct {
		UsagePercent *float64 `json:"usage_percent"`
	}
	var s struct {
		Uptime *string   `json:"uptime"`
		CPU    *reading  `json:"cpu"`
		Memory *reading  `json:"memory"`
		Disks  []reading `json:"disks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	if s.Uptime == nil && s.CPU == nil && s.Memory == nil && s.Disks == nil {
		return "", errNoFields
	}
	var cpu, mem, disk *float64
	if s.CPU != nil {
		cpu = s.CPU.UsagePercent
	}
	if s.Memory != nil {
		mem = s.Memory.UsagePercent
	}
	if len(s.Disks) > 0 {
		disk = s.Disks[0].UsagePercent
	}
	up := "-"
	if s.Uptime != nil && *s.Uptime != "" {
		up = *s.Uptime
	}
	return fmt.Sprintf("CPU %s | Mem %s | Disk %s | Up %s", percent(cpu), percent(mem), percent(disk), up), nil
}

// levelRank orders the alerts vocabulary. A word outside it ranks above
// critical, so a level this version does not know is shown rather than
// hidden behind an OK.
func levelRank(level string) int {
	switch alerts.Level(level) {
	case alerts.LevelOK:
		return 0
	case alerts.LevelWarning:
		return 1
	case alerts.LevelCritical:
		return 2
	case "":
		return -1
	default:
		return 3
	}
}

func summarizeAlerts(data []byte) (string, error) {
	type item struct {
		Mount   string   `json:"mount"`
		Status  string   `json:"status"`
		Current *float64 `json:"current"`
	}
	var a struct {
		CPU    *item  `json:"cpu"`
		Memory *item  `json:"memory"`
		Disks  []item `json:"disks"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return "", err
	}
	if a.CPU == nil && a.Memory == nil && a.Disks == nil {
		return "", errNoFields
	}

	worst := ""
	note := func(it *item) {
		if it != nil && levelRank(it.Status) > levelRank(worst) {
			worst = it.Status
		}
	}
	// One resource: its reading, and its level when that is not ok.
	field := func(label string, it *item) string {
		if it == nil {
			return label + "     -"
		}
		s := label + " " + percent(it.Current)
		if it.Mount != "" {
			s += " " + it.Mount
		}
		if it.Status != string(alerts.LevelOK) {
			level := it.Status
			if level == "" {
				level = "no level"
			}
			s += " " + level
		}
		return s
	}

	note(a.CPU)
	note(a.Memory)
	var disk *item
	for i := range a.Disks {
		d := &a.Disks[i]
		note(d)
		if disk == nil || levelRank(d.Status) > levelRank(disk.Status) ||
			(levelRank(d.Status) == levelRank(disk.Status) && d.Current != nil && (disk.Current == nil || *d.Current > *disk.Current)) {
			disk = d
		}
	}

	head := strings.ToUpper(worst)
	if head == "" {
		head = "NO LEVEL"
	}
	line := fmt.Sprintf("%-8s | %s | %s | %s", head, field("CPU", a.CPU), field("Mem", a.Memory), field("Disk", disk))
	if len(a.Disks) > 1 {
		line += fmt.Sprintf(" (fullest of %d)", len(a.Disks))
	}
	return line, nil
}

func summarizeDockerList(data []byte) (string, error) {
	var containers []struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &containers); err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "no containers", nil
	}
	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
	}
	return fmt.Sprintf("%s | %d running | %d not running", count(len(containers), "container"), running, len(containers)-running), nil
}

func summarizeDockerStats(data []byte) (string, error) {
	var stats []struct {
		Name string `json:"name"`
		CPU  string `json:"cpu_percent"`
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return "", err
	}
	if len(stats) == 0 {
		return "no running containers", nil
	}
	busiest, best := "", -1.0
	for _, s := range stats {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s.CPU, "%")), 64)
		if err == nil && v > best {
			busiest, best = s.Name, v
		}
	}
	line := count(len(stats), "running container")
	if busiest != "" {
		line += fmt.Sprintf(" | busiest %s at %.1f%% CPU", busiest, best)
	}
	return line, nil
}

func summarizePorts(data []byte) (string, error) {
	var list []struct {
		Protocol string `json:"protocol"`
		Port     string `json:"port"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no open ports", nil
	}
	// The same port is often open on IPv4 and IPv6; it is one port to a person.
	seen := map[string]bool{}
	byProto := map[string]int{}
	for _, p := range list {
		key := p.Protocol + "/" + p.Port
		if !seen[key] {
			seen[key] = true
			byProto[p.Protocol]++
		}
	}
	protos := make([]string, 0, len(byProto))
	for proto := range byProto {
		protos = append(protos, proto)
	}
	sort.Strings(protos)
	parts := make([]string, len(protos))
	for i, proto := range protos {
		parts[i] = fmt.Sprintf("%s %d", proto, byProto[proto])
	}
	return fmt.Sprintf("%s (%s)", count(len(seen), "open port"), strings.Join(parts, ", ")), nil
}

func summarizeDoctor(data []byte) (string, error) {
	var d struct {
		Status  *string `json:"status"`
		Summary *struct {
			Warn *int `json:"warn"`
			Fail *int `json:"fail"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return "", err
	}
	if d.Status == nil {
		return "", errNoFields
	}
	num := func(n *int) string {
		if n == nil {
			return "-"
		}
		return strconv.Itoa(*n)
	}
	var warn, fail *int
	if d.Summary != nil {
		warn, fail = d.Summary.Warn, d.Summary.Fail
	}
	return fmt.Sprintf("%-4s | warn %s | fail %s", strings.ToUpper(*d.Status), num(warn), num(fail)), nil
}

func summarizeProcesses(data []byte) (string, error) {
	var p struct {
		Processes []struct {
			Name string   `json:"name"`
			CPU  *float64 `json:"cpu"`
			Mem  *float64 `json:"mem"`
		} `json:"processes"`
		Total   *int              `json:"total"`
		Zombies []json.RawMessage `json:"zombies"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return "", err
	}
	if p.Total == nil && p.Processes == nil {
		return "", errNoFields
	}
	parts := []string{}
	if p.Total != nil {
		parts = append(parts, count(*p.Total, "process"))
	}
	if len(p.Processes) > 0 {
		top := p.Processes[0]
		reading := func(label string, v *float64) string {
			if v == nil {
				return label + " -"
			}
			return fmt.Sprintf("%s %.1f%%", label, *v)
		}
		parts = append(parts, fmt.Sprintf("top %s (%s, %s)", top.Name, reading("CPU", top.CPU), reading("Mem", top.Mem)))
	}
	parts = append(parts, count(len(p.Zombies), "zombie"))
	return strings.Join(parts, " | "), nil
}

func summarizeReport(data []byte) (string, error) {
	var r struct {
		IsBaseline     *bool             `json:"is_baseline"`
		SnapshotSaved  *bool             `json:"snapshot_saved"`
		NeedsAttention []json.RawMessage `json:"needs_attention"`
		NotableChanges []json.RawMessage `json:"notable_changes"`
		ComparedTo     string            `json:"compared_to"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	if r.IsBaseline == nil && r.NeedsAttention == nil && r.NotableChanges == nil {
		return "", errNoFields
	}
	parts := []string{
		fmt.Sprintf("%d need attention", len(r.NeedsAttention)),
		count(len(r.NotableChanges), "notable change"),
	}
	switch {
	case r.ComparedTo != "":
		parts = append(parts, "since "+r.ComparedTo)
	case r.IsBaseline != nil && *r.IsBaseline:
		parts = append(parts, "first snapshot, nothing to compare")
	}
	if r.SnapshotSaved != nil && *r.SnapshotSaved {
		parts = append(parts, "snapshot saved")
	}
	return strings.Join(parts, " | "), nil
}

func summarizeBackupList(data []byte) (string, error) {
	var entries []struct {
		Name      string `json:"name"`
		Size      string `json:"size"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "no backups", nil
	}
	newest := -1
	var newestAt time.Time
	for i, e := range entries {
		t, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err == nil && (newest < 0 || t.After(newestAt)) {
			newest, newestAt = i, t
		}
	}
	line := count(len(entries), "backup")
	if newest >= 0 {
		e := entries[newest]
		line += fmt.Sprintf(" | newest %s (%s, %s)", e.Name, e.CreatedAt, e.Size)
	}
	return line, nil
}

func summarizeNetworkScan(data []byte) (string, error) {
	var devices []json.RawMessage
	if err := json.Unmarshal(data, &devices); err != nil {
		return "", err
	}
	return count(len(devices), "device") + " found", nil
}
