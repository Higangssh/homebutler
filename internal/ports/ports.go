package ports

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

type PortInfo struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     string `json:"port"`
	PID      string `json:"pid,omitempty"`
	Process  string `json:"process,omitempty"`
}

func List() ([]PortInfo, error) {
	switch runtime.GOOS {
	case "darwin":
		return listDarwin()
	case "linux":
		return listLinux()
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func listDarwin() ([]PortInfo, error) {
	out, err := util.RunCmd("/usr/sbin/lsof", "-iTCP", "-sTCP:LISTEN", "-nP")
	if err != nil {
		return nil, fmt.Errorf("failed to list ports: %w", err)
	}

	var ports []PortInfo
	seen := make(map[string]bool)

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || fields[0] == "COMMAND" {
			continue
		}
		process := fields[0]
		pid := fields[1]
		addrPort := fields[8]

		// Deduplicate
		key := fmt.Sprintf("%s-%s", addrPort, pid)
		if seen[key] {
			continue
		}
		seen[key] = true

		addr, port := splitAddrPort(addrPort)
		ports = append(ports, PortInfo{
			Protocol: "tcp",
			Address:  addr,
			Port:     port,
			PID:      pid,
			Process:  process,
		})
	}
	return ports, nil
}

func listLinux() ([]PortInfo, error) {
	out, err := util.RunCmd("ss", "-tlnp")
	if err != nil {
		return nil, fmt.Errorf("failed to list ports: %w", err)
	}

	var ports []PortInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "State" {
			continue
		}
		local := fields[3]
		addr, port := splitAddrPort(local)

		process := ""
		if len(fields) >= 6 {
			// Extract process name from users:(("name",pid=123,fd=4))
			p := fields[5]
			if idx := strings.Index(p, "((\""); idx >= 0 {
				end := strings.Index(p[idx+3:], "\"")
				if end >= 0 {
					process = p[idx+3 : idx+3+end]
				}
			}
		}

		ports = append(ports, PortInfo{
			Protocol: "tcp",
			Address:  addr,
			Port:     port,
			Process:  process,
		})
	}
	return ports, nil
}

func splitAddrPort(s string) (string, string) {
	// Handle IPv6 [::1]:8080 or *:8080 or 127.0.0.1:8080
	lastColon := strings.LastIndex(s, ":")
	if lastColon < 0 {
		return s, ""
	}
	return s[:lastColon], s[lastColon+1:]
}
