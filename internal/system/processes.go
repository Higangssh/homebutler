package system

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

// ProcessInfo holds information about a running process.
type ProcessInfo struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
}

// TopProcesses returns the top n processes sorted by CPU usage.
func TopProcesses(n int) ([]ProcessInfo, error) {
	var out string
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,comm", "-r")
	case "linux":
		out, err = util.RunCmd("ps", "-eo", "pid,pcpu,pmem,comm", "--sort=-pcpu")
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err != nil {
		return nil, err
	}

	return parseProcesses(out, n), nil
}

// parseProcesses extracts process info from ps output, skipping the header.
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
		if len(fields) < 4 {
			continue
		}

		var pid int
		var cpu, mem float64
		fmt.Sscanf(fields[0], "%d", &pid)
		fmt.Sscanf(fields[1], "%f", &cpu)
		fmt.Sscanf(fields[2], "%f", &mem)

		// comm is the last column and may contain path with spaces
		name := strings.Join(fields[3:], " ")
		if strings.Contains(name, "/") {
			name = filepath.Base(name)
		}

		procs = append(procs, ProcessInfo{
			PID:  pid,
			Name: name,
			CPU:  cpu,
			Mem:  mem,
		})

		if len(procs) >= n {
			break
		}
	}

	return procs
}
