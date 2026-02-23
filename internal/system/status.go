package system

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

type StatusInfo struct {
	Hostname string    `json:"hostname"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	Uptime   string    `json:"uptime"`
	CPU      CPUInfo   `json:"cpu"`
	Memory   MemInfo   `json:"memory"`
	Disks    []DiskInfo `json:"disks"`
	Time     string    `json:"time"`
}

type CPUInfo struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
}

type MemInfo struct {
	TotalGB  float64 `json:"total_gb"`
	UsedGB   float64 `json:"used_gb"`
	Percent  float64 `json:"usage_percent"`
}

type DiskInfo struct {
	Mount   string  `json:"mount"`
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	Percent float64 `json:"usage_percent"`
}

func Status() (*StatusInfo, error) {
	hostname, _ := os.Hostname()

	info := &StatusInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Uptime:   getUptime(),
		CPU:      getCPU(),
		Memory:   getMemory(),
		Disks:    getDisks(),
		Time:     time.Now().Format(time.RFC3339),
	}

	return info, nil
}

func getUptime() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := util.RunCmd("/usr/sbin/sysctl", "-n", "kern.boottime")
		if err != nil {
			return "unknown"
		}
		// Parse: { sec = 1234567890, usec = 0 }
		parts := strings.Split(out, "=")
		if len(parts) < 2 {
			return "unknown"
		}
		secStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
		var sec int64
		fmt.Sscanf(secStr, "%d", &sec)
		boot := time.Unix(sec, 0)
		dur := time.Since(boot)
		days := int(dur.Hours() / 24)
		hours := int(dur.Hours()) % 24
		if days > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dh %dm", hours, int(dur.Minutes())%60)
	case "linux":
		out, err := util.RunCmd("cat", "/proc/uptime")
		if err != nil {
			return "unknown"
		}
		var secs float64
		fmt.Sscanf(out, "%f", &secs)
		dur := time.Duration(secs) * time.Second
		days := int(dur.Hours() / 24)
		hours := int(dur.Hours()) % 24
		if days > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dh %dm", hours, int(dur.Minutes())%60)
	default:
		return "unknown"
	}
}

func getCPU() CPUInfo {
	cores := runtime.NumCPU()
	usage := 0.0

	switch runtime.GOOS {
	case "darwin":
		out, err := util.RunCmd("top", "-l", "1", "-n", "0", "-stats", "cpu")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "CPU usage") {
					// CPU usage: 5.26% user, 10.52% sys, 84.21% idle
					parts := strings.Split(line, ",")
					for _, p := range parts {
						if strings.Contains(p, "idle") {
							var idle float64
							fmt.Sscanf(strings.TrimSpace(p), "%f%% idle", &idle)
							usage = 100 - idle
						}
					}
				}
			}
		}
	case "linux":
		out, err := util.RunCmd("grep", "cpu ", "/proc/stat")
		if err == nil {
			fields := strings.Fields(out)
			if len(fields) >= 8 {
				var user, nice, sys, idle float64
				fmt.Sscanf(fields[1], "%f", &user)
				fmt.Sscanf(fields[2], "%f", &nice)
				fmt.Sscanf(fields[3], "%f", &sys)
				fmt.Sscanf(fields[4], "%f", &idle)
				total := user + nice + sys + idle
				if total > 0 {
					usage = ((total - idle) / total) * 100
				}
			}
		}
	}

	return CPUInfo{
		UsagePercent: round2(usage),
		Cores:        cores,
	}
}

func getMemory() MemInfo {
	switch runtime.GOOS {
	case "darwin":
		out, err := util.RunCmd("/usr/sbin/sysctl", "-n", "hw.memsize")
		if err != nil {
			return MemInfo{}
		}
		var totalBytes int64
		fmt.Sscanf(strings.TrimSpace(out), "%d", &totalBytes)
		totalGB := float64(totalBytes) / (1024 * 1024 * 1024)

		// Get used memory from vm_stat
		vmOut, err := util.RunCmd("vm_stat")
		if err != nil {
			return MemInfo{TotalGB: round2(totalGB)}
		}
		pageSize := 16384 // Apple Silicon default
		var active, wired, speculative int64
		for _, line := range strings.Split(vmOut, "\n") {
			if strings.Contains(line, "page size of") {
				fmt.Sscanf(line, "Mach Virtual Memory Statistics: (page size of %d bytes)", &pageSize)
			}
			if strings.Contains(line, "Pages active") {
				fmt.Sscanf(strings.TrimSpace(strings.Split(line, ":")[1]), "%d", &active)
			}
			if strings.Contains(line, "Pages wired") {
				fmt.Sscanf(strings.TrimSpace(strings.Split(line, ":")[1]), "%d", &wired)
			}
			if strings.Contains(line, "Pages speculative") {
				fmt.Sscanf(strings.TrimSpace(strings.Split(line, ":")[1]), "%d", &speculative)
			}
		}
		usedBytes := (active + wired + speculative) * int64(pageSize)
		usedGB := float64(usedBytes) / (1024 * 1024 * 1024)

		return MemInfo{
			TotalGB: round2(totalGB),
			UsedGB:  round2(usedGB),
			Percent: round2((usedGB / totalGB) * 100),
		}
	case "linux":
		out, err := util.RunCmd("cat", "/proc/meminfo")
		if err != nil {
			return MemInfo{}
		}
		var totalKB, availKB int64
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(line, "MemTotal: %d kB", &totalKB)
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(line, "MemAvailable: %d kB", &availKB)
			}
		}
		totalGB := float64(totalKB) / (1024 * 1024)
		usedGB := float64(totalKB-availKB) / (1024 * 1024)
		return MemInfo{
			TotalGB: round2(totalGB),
			UsedGB:  round2(usedGB),
			Percent: round2((usedGB / totalGB) * 100),
		}
	default:
		return MemInfo{}
	}
}

func getDisks() []DiskInfo {
	out, err := util.RunCmd("df", "-h")
	if err != nil {
		return nil
	}

	var disks []DiskInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := fields[len(fields)-1]
		// Only show relevant mounts
		if mount == "/" || strings.HasPrefix(mount, "/home") || strings.HasPrefix(mount, "/mnt") || strings.HasPrefix(mount, "/Volumes") {
			var total, used float64
			var percent float64
			total = parseSize(fields[1])
			used = parseSize(fields[2])
			pctStr := strings.TrimSuffix(fields[4], "%")
			fmt.Sscanf(pctStr, "%f", &percent)

			disks = append(disks, DiskInfo{
				Mount:   mount,
				TotalGB: round2(total),
				UsedGB:  round2(used),
				Percent: percent,
			})
		}
	}
	return disks
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	var val float64
	if strings.HasSuffix(s, "Ti") || strings.HasSuffix(s, "T") {
		fmt.Sscanf(s, "%f", &val)
		return val * 1024
	}
	if strings.HasSuffix(s, "Gi") || strings.HasSuffix(s, "G") {
		fmt.Sscanf(s, "%f", &val)
		return val
	}
	if strings.HasSuffix(s, "Mi") || strings.HasSuffix(s, "M") {
		fmt.Sscanf(s, "%f", &val)
		return val / 1024
	}
	fmt.Sscanf(s, "%f", &val)
	return val
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}
