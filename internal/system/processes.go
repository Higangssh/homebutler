package system

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

// ProcessInfo holds information about a running process.
type ProcessInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Mem    float64 `json:"mem"`
	State  string  `json:"state,omitempty"`
	Zombie bool    `json:"zombie,omitempty"`
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

	switch sortBy {
	case "mem":
		sort.Slice(all, func(i, j int) bool { return all[i].Mem > all[j].Mem })
	default: // cpu
		sort.Slice(all, func(i, j int) bool { return all[i].CPU > all[j].CPU })
	}

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
	switch sortBy {
	case "mem":
		sort.Slice(all, func(i, j int) bool { return all[i].Mem > all[j].Mem })
	default:
		sort.Slice(all, func(i, j int) bool { return all[i].CPU > all[j].CPU })
	}

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

// allProcesses returns all running processes.
func allProcesses() ([]ProcessInfo, error) {
	var out string
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,state,comm")
	case "linux":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,state,comm")
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err != nil {
		return nil, err
	}

	return parseProcesses(out, 0), nil
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
		if len(fields) < 5 {
			// Fallback for old 4-field format (without state)
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
		fmt.Sscanf(fields[0], "%d", &pid)
		fmt.Sscanf(fields[1], "%f", &cpu)
		fmt.Sscanf(fields[2], "%f", &mem)
		state := fields[3]

		// comm is the last column and may contain path with spaces
		name := strings.Join(fields[4:], " ")
		if strings.Contains(name, "/") {
			name = filepath.Base(name)
		}

		isZombie := strings.HasPrefix(state, "Z")

		procs = append(procs, ProcessInfo{
			PID:    pid,
			Name:   name,
			CPU:    cpu,
			Mem:    mem,
			State:  state,
			Zombie: isZombie,
		})

		if n > 0 && len(procs) >= n {
			break
		}
	}

	return procs
}
