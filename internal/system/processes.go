package system

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// ProcessInfo holds information about a running process.
type ProcessInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Mem    float64 `json:"mem"`
	RSS    int64   `json:"rss"`
	State  string  `json:"state,omitempty"`
	Zombie bool    `json:"zombie,omitempty"`

	// Elapsed is how long the process has been running. Excluded from JSON
	// for the same reason as Command: this is here for callers comparing
	// runs, not for the processes command's output.
	Elapsed time.Duration `json:"-"`

	// Command is the full invocation, used to tell two processes sharing an
	// executable name apart. It is excluded from JSON deliberately: command
	// lines carry secrets in flags, and nothing that reads this struct over
	// the wire has ever had to be handled as a credential. Callers that need
	// to compare invocations across runs should hash it.
	Command string `json:"-"`
}

// ProcessResult holds the process list and summary metadata.
type ProcessResult struct {
	Processes []ProcessInfo `json:"processes"`
	Total     int           `json:"total"`
	Zombies   []ProcessInfo `json:"zombies,omitempty"`
}

// TopProcesses returns the top n processes sorted by the given field.
// sortBy can be "cpu" (default) or "mem".
func TopProcesses(n int) ([]ProcessInfo, error) {
	return TopProcessesSorted(n, "cpu")
}

// TopProcessesSorted returns the top n processes sorted by sortBy field.
func TopProcessesSorted(n int, sortBy string) ([]ProcessInfo, error) {
	all, err := allProcesses()
	if err != nil {
		return nil, err
	}

	sortProcesses(all, sortBy)

	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all, nil
}

// ListProcesses returns a full process result with top N, total count, and zombies.
func ListProcesses(n int, sortBy string) (*ProcessResult, error) {
	all, err := allProcesses()
	if err != nil {
		return nil, err
	}

	// Collect zombies
	var zombies []ProcessInfo
	for _, p := range all {
		if p.Zombie {
			zombies = append(zombies, p)
		}
	}

	// Sort
	sortProcesses(all, sortBy)

	total := len(all)
	if n > 0 && len(all) > n {
		all = all[:n]
	}

	return &ProcessResult{
		Processes: all,
		Total:     total,
		Zombies:   zombies,
	}, nil
}

// sortProcesses sorts by primary field with secondary sort for tie-breaking.
func sortProcesses(procs []ProcessInfo, sortBy string) {
	switch sortBy {
	case "mem":
		sort.Slice(procs, func(i, j int) bool {
			if procs[i].Mem != procs[j].Mem {
				return procs[i].Mem > procs[j].Mem
			}
			return procs[i].CPU > procs[j].CPU
		})
	default: // cpu
		sort.Slice(procs, func(i, j int) bool {
			if procs[i].CPU != procs[j].CPU {
				return procs[i].CPU > procs[j].CPU
			}
			return procs[i].Mem > procs[j].Mem
		})
	}
}

// allProcesses returns all running processes, filtering out kernel threads.
func allProcesses() ([]ProcessInfo, error) {
	var out string
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,rss,state,comm")
	case "linux":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,rss,state,comm")
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err != nil {
		return nil, err
	}

	all := parseProcesses(out, 0)
	fillCommands(all)

	// Filter out kernel threads (PID <= 2 or bracketed names like [kthreadd])
	var filtered []ProcessInfo
	for _, p := range all {
		if p.PID <= 2 {
			continue
		}
		if strings.HasPrefix(p.Name, "[") || isKernelThread(p.Name) {
			continue
		}
		filtered = append(filtered, p)
	}

	return filtered, nil
}

// AllProcesses returns every running process except kernel threads, with the
// command line filled in. Unlike ListProcesses it does not sort or truncate:
// a caller comparing two runs needs the whole set, since a process dropping
// out of a top-N sample is not the same event as a process exiting.
func AllProcesses() ([]ProcessInfo, error) {
	return allProcesses()
}

// fillCommands joins invocations onto processes by PID.
//
// It is a second ps call rather than an extra column on the first, because
// comm can itself contain spaces — the existing parser joins the tail of the
// line to recover it — so appending args to the same output leaves no
// unambiguous split. A process that exits between the two calls simply has
// no command, and identity falls back to its name.
func fillCommands(procs []ProcessInfo) {
	// etime rather than etimes: etimes is Linux-only, and this has to parse
	// on macOS too.
	out, err := util.RunCmd("ps", "-eo", "pid=,etime=,args=")
	if err != nil {
		return
	}
	type detail struct {
		command string
		elapsed time.Duration
	}
	details := make(map[int]detail, len(procs))
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		details[pid] = detail{
			command: strings.Join(fields[2:], " "),
			elapsed: parseElapsed(fields[1]),
		}
	}
	for i := range procs {
		d := details[procs[i].PID]
		procs[i].Command = d.command
		procs[i].Elapsed = d.elapsed
	}
}

// parseElapsed reads ps's etime format: [[dd-]hh:]mm:ss. An unparseable
// value returns zero, which callers treat as "too young to report" — the
// safer direction, since a process wrongly held back appears on the next run
// and one wrongly reported is noise forever.
func parseElapsed(value string) time.Duration {
	days := 0
	if before, after, found := strings.Cut(value, "-"); found {
		d, err := strconv.Atoi(before)
		if err != nil {
			return 0
		}
		days, value = d, after
	}

	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	units := []time.Duration{time.Second, time.Minute, time.Hour}
	total := time.Duration(days) * 24 * time.Hour
	for i := range parts {
		n, err := strconv.Atoi(parts[len(parts)-1-i])
		if err != nil {
			return 0
		}
		total += time.Duration(n) * units[i]
	}
	return total
}

// isKernelThread detects common Linux kernel thread names.
func isKernelThread(name string) bool {
	kernelPrefixes := []string{
		"kthreadd", "kworker/", "ksoftirqd/", "migration/",
		"rcu_", "watchdog/", "cpuhp/", "netns", "kdevtmpfs",
		"inet_frag_wq", "kauditd", "khungtaskd", "oom_reaper",
		"writeback", "kcompactd", "ksmd", "khugepaged",
		"kintegrityd", "kblockd", "blkcg_punt", "edac-poller",
		"devfreq_wq", "kswapd", "ecryptfs", "kthrotld",
		"irq/", "scsi_", "md_", "raid", "jbd2",
		"ext4-", "xfs-", "btrfs-",
		"slub_flushwq", "mm_percpu_wq", "rcu_tasks",
		"0:0H",
	}
	for _, prefix := range kernelPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// parseProcesses extracts process info from ps output, skipping the header.
// If n <= 0, all processes are returned.
func parseProcesses(output string, n int) []ProcessInfo {
	lines := strings.Split(output, "\n")
	var procs []ProcessInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip header line
		if strings.HasPrefix(line, "PID") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			// Fallback for old format (without rss/state)
			if len(fields) >= 4 {
				var pid int
				var cpu, mem float64
				fmt.Sscanf(fields[0], "%d", &pid)
				fmt.Sscanf(fields[1], "%f", &cpu)
				fmt.Sscanf(fields[2], "%f", &mem)
				name := strings.Join(fields[3:], " ")
				if strings.Contains(name, "/") {
					name = filepath.Base(name)
				}
				procs = append(procs, ProcessInfo{PID: pid, Name: name, CPU: cpu, Mem: mem})
				if n > 0 && len(procs) >= n {
					break
				}
			}
			continue
		}

		var pid int
		var cpu, mem float64
		var rss int64
		fmt.Sscanf(fields[0], "%d", &pid)
		fmt.Sscanf(fields[1], "%f", &cpu)
		fmt.Sscanf(fields[2], "%f", &mem)
		fmt.Sscanf(fields[3], "%d", &rss)
		state := fields[4]

		// comm is the last column and may contain path with spaces
		name := strings.Join(fields[5:], " ")
		if strings.Contains(name, "/") {
			name = filepath.Base(name)
		}

		isZombie := strings.HasPrefix(state, "Z")

		procs = append(procs, ProcessInfo{
			PID:    pid,
			Name:   name,
			CPU:    cpu,
			Mem:    mem,
			RSS:    rss,
			State:  state,
			Zombie: isZombie,
		})

		if n > 0 && len(procs) >= n {
			break
		}
	}

	return procs
}
