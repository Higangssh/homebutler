package system

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// UnitLookup finds the processes that belong to an on-demand systemd service:
// one that runs only when something asks for it and exits once idle, as
// fwupd and packagekit do on Ubuntu. Whether one is running when a report is
// taken is chance.
//
// A service is on demand when its unit file is static, no loaded unit wants
// or requires it, and it is started by a D-Bus name, a socket, a path or a
// timer. That separates fwupd from udisks2, which has a bus name too but is
// enabled and wanted by graphical.target.
type UnitLookup struct {
	// Cgroup returns the contents of /proc/<pid>/cgroup.
	Cgroup func(pid int) (string, error)
	// Systemctl runs systemctl with args and gives up when ctx is done.
	Systemctl func(ctx context.Context, args ...string) (string, error)
	// Timeout bounds the one systemctl call.
	Timeout time.Duration
}

// unitTimeout is how long a report waits for systemctl. Asking about every
// running service takes a few tens of milliseconds on a Raspberry Pi; a
// systemctl that has not answered in two seconds is not going to, and report
// runs from cron.
const unitTimeout = 2 * time.Second

var defaultUnits = UnitLookup{
	Cgroup: func(pid int) (string, error) {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
		return string(b), err
	},
	Systemctl: func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "systemctl", args...)
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		return string(out), err
	},
	Timeout: unitTimeout,
}

// MarkOnDemand sets OnDemand on the processes that belong to an on-demand
// systemd service. Off Linux it does nothing.
func MarkOnDemand(procs []ProcessInfo) {
	if runtime.GOOS != "linux" {
		return
	}
	defaultUnits.Mark(procs)
}

// unitProperties are what systemctl is asked about each unit. Type is not
// among them: Type=dbus needs a BusName, which is asked for directly.
var unitProperties = []string{"Id", "LoadState", "UnitFileState", "WantedBy", "RequiredBy", "BusName", "TriggeredBy"}

// Mark sets OnDemand on the processes that belong to an on-demand service.
//
// It fails safe. A process whose cgroup cannot be read, that is not in a
// system service, or whose service systemctl did not describe, is left
// unmarked and compared as it always was. So is every process when
// systemctl fails or does not answer within Timeout.
func (l UnitLookup) Mark(procs []ProcessInfo) {
	if l.Cgroup == nil || l.Systemctl == nil {
		return
	}
	unitOf := make(map[int]string, len(procs))
	seen := map[string]bool{}
	var units []string
	for _, p := range procs {
		c, err := l.Cgroup(p.PID)
		if err != nil {
			continue
		}
		unit := serviceOf(c)
		if unit == "" {
			continue
		}
		unitOf[p.PID] = unit
		if !seen[unit] {
			seen[unit] = true
			units = append(units, unit)
		}
	}
	if len(units) == 0 {
		return
	}
	sort.Strings(units)

	timeout := l.Timeout
	if timeout <= 0 {
		timeout = unitTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := append([]string{"show", "--no-pager", "--property=" + strings.Join(unitProperties, ","), "--"}, units...)
	out, err := l.Systemctl(ctx, args...)
	if err != nil || ctx.Err() != nil {
		return
	}

	onDemand := map[string]bool{}
	for _, props := range parseUnitBlocks(out) {
		if seen[props["Id"]] && isOnDemand(props) {
			onDemand[props["Id"]] = true
		}
	}
	for i := range procs {
		if unit, ok := unitOf[procs[i].PID]; ok && onDemand[unit] {
			procs[i].OnDemand = true
		}
	}
}

// isOnDemand is the rule in UnitLookup's comment.
func isOnDemand(props map[string]string) bool {
	return props["LoadState"] == "loaded" &&
		props["UnitFileState"] == "static" &&
		props["WantedBy"] == "" && props["RequiredBy"] == "" &&
		(props["BusName"] != "" || props["TriggeredBy"] != "")
}

// serviceOf reads the system service a process runs in from its
// /proc/<pid>/cgroup: the unified hierarchy's line on cgroup v2, or the
// name=systemd line on a v1 or hybrid machine. Slices are skipped, so a
// service in a sub-slice is found, and anything under the service's own
// cgroup is the service's. A process in a scope, the root, a container or
// a user's session gets nothing. A user service is the user's manager's,
// not the system's, and is not asked about.
func serviceOf(cgroup string) string {
	var path string
	for _, line := range strings.Split(cgroup, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[1] == "name=systemd" || parts[0] == "0" && parts[1] == "" {
			path = parts[2]
			if parts[1] == "name=systemd" {
				break
			}
		}
	}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		switch {
		case part == "user.slice":
			return ""
		case strings.HasSuffix(part, ".slice"):
			continue
		case strings.HasSuffix(part, ".service"):
			return part
		default:
			return ""
		}
	}
	return ""
}

// parseUnitBlocks splits `systemctl show` output for several units into one
// map per unit. Units are separated by a blank line, and the properties are
// printed in systemd's order rather than the order they were asked for.
func parseUnitBlocks(out string) []map[string]string {
	var blocks []map[string]string
	block := map[string]string{}
	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, block)
		}
		block = map[string]string{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		block[k] = v
	}
	flush()
	return blocks
}
