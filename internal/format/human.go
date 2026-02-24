package format

import (
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

// Status formats system status for human reading.
func Status(info *system.StatusInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🖥  %s (%s/%s)\n", info.Hostname, info.OS, info.Arch)
	fmt.Fprintf(&b, "   Uptime:  %s\n", info.Uptime)
	fmt.Fprintf(&b, "   CPU:     %.1f%% (%d cores)\n", info.CPU.UsagePercent, info.CPU.Cores)
	fmt.Fprintf(&b, "   Memory:  %.1f / %.1f GB (%.1f%%)\n", info.Memory.UsedGB, info.Memory.TotalGB, info.Memory.Percent)
	for _, d := range info.Disks {
		fmt.Fprintf(&b, "   Disk %s: %.0f / %.0f GB (%.0f%%)\n", d.Mount, d.UsedGB, d.TotalGB, d.Percent)
	}
	return b.String()
}

// DockerList formats container list for human reading.
func DockerList(containers []docker.Container) string {
	if len(containers) == 0 {
		return "No containers found.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s %-30s %-10s %s\n", "CONTAINER", "IMAGE", "STATE", "STATUS")
	for _, c := range containers {
		fmt.Fprintf(&b, "%-20s %-30s %-10s %s\n", c.Name, c.Image, c.State, c.Status)
	}
	return b.String()
}

// DockerAction formats docker restart/stop result.
func DockerAction(action, container string) string {
	return fmt.Sprintf("✅ %s: %s\n", action, container)
}

// Alerts formats alert check results for human reading.
func Alerts(result *alerts.AlertResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "   CPU:    %5.1f%% (threshold: %.0f%%) %s\n", result.CPU.Current, result.CPU.Threshold, statusIcon(result.CPU.Status))
	fmt.Fprintf(&b, "   Memory: %5.1f%% (threshold: %.0f%%) %s\n", result.Memory.Current, result.Memory.Threshold, statusIcon(result.Memory.Status))
	for _, d := range result.Disks {
		fmt.Fprintf(&b, "   Disk %s: %.0f%% (threshold: %.0f%%) %s\n", d.Mount, d.Current, d.Threshold, statusIcon(d.Status))
	}
	return b.String()
}

// Ports formats open ports for human reading.
func Ports(openPorts []ports.PortInfo) string {
	if len(openPorts) == 0 {
		return "No open ports found.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-6s %-20s %-8s %s\n", "PROTO", "ADDRESS", "PORT", "PROCESS")
	for _, p := range openPorts {
		process := p.Process
		if p.PID != "" {
			process = fmt.Sprintf("%s/%s", p.Process, p.PID)
		}
		fmt.Fprintf(&b, "%-6s %-20s %-8s %s\n", p.Protocol, p.Address, p.Port, process)
	}
	return b.String()
}

// NetworkScan formats discovered devices for human reading.
func NetworkScan(devices []network.Device) string {
	if len(devices) == 0 {
		return "No devices found.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %-20s %s\n", "IP", "MAC", "HOSTNAME")
	for _, d := range devices {
		hostname := d.Hostname
		if hostname == "" {
			hostname = "-"
		}
		fmt.Fprintf(&b, "%-18s %-20s %s\n", d.IP, d.MAC, hostname)
	}
	fmt.Fprintf(&b, "\n%d devices found.\n", len(devices))
	return b.String()
}

// WakeResult formats WOL result for human reading.
func WakeResult(mac, broadcast string) string {
	return fmt.Sprintf("✅ Magic packet sent to %s (broadcast: %s)\n", mac, broadcast)
}

// MultiServer formats --all results for human reading.
func MultiServer(results []map[string]interface{}) string {
	var b strings.Builder
	for _, r := range results {
		server, _ := r["server"].(string)
		if errMsg, ok := r["error"].(string); ok && errMsg != "" {
			fmt.Fprintf(&b, "❌ %-12s %s\n", server, errMsg)
			continue
		}
		data, ok := r["data"].(map[string]interface{})
		if !ok {
			fmt.Fprintf(&b, "📡 %-12s (no data)\n", server)
			continue
		}
		cpu := getNestedFloat(data, "cpu", "usage_percent")
		mem := getNestedFloat(data, "memory", "usage_percent")
		disk := getFirstDiskPercent(data)
		uptime, _ := data["uptime"].(string)
		fmt.Fprintf(&b, "📡 %-12s CPU %4.0f%% | Mem %4.0f%% | Disk %4.0f%% | Up %s\n", server, cpu, mem, disk, uptime)
	}
	return b.String()
}

func statusIcon(status string) string {
	switch status {
	case "ok":
		return "✅"
	case "warning":
		return "⚠️"
	case "critical":
		return "🔴"
	default:
		return status
	}
}

func getNestedFloat(data map[string]interface{}, keys ...string) float64 {
	current := data
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(float64); ok {
				return v
			}
			return 0
		}
		if next, ok := current[key].(map[string]interface{}); ok {
			current = next
		} else {
			return 0
		}
	}
	return 0
}

func getFirstDiskPercent(data map[string]interface{}) float64 {
	disks, ok := data["disks"].([]interface{})
	if !ok || len(disks) == 0 {
		return 0
	}
	if d, ok := disks[0].(map[string]interface{}); ok {
		if v, ok := d["usage_percent"].(float64); ok {
			return v
		}
	}
	return 0
}
