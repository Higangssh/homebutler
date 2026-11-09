package system

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/schema"
	"github.com/Higangssh/homebutler/internal/util"
)

type StatusInfo struct {
	Hostname string     `json:"hostname"`
	OS       string     `json:"os"`
	Arch     string     `json:"arch"`
	Uptime   string     `json:"uptime"`
	CPU      CPUInfo    `json:"cpu"`
	Memory   MemInfo    `json:"memory"`
	Disks    []DiskInfo `json:"disks"`
	Time     string     `json:"time"`
}

// StatusDoc is status as a document of its own: what `status --json`, the
// system_status tool and the status routes answer with. StatusInfo is also
// nested inside report and inventory answers, and a schema_version there
// would read as the version of the document it sits in, which it is not. So
// the version goes on the outside, and embedding keeps the keys flat: the
// document is StatusInfo's keys with one more.
type StatusDoc struct {
	SchemaVersion schema.Version `json:"schema_version"`
	*StatusInfo
}

// Document wraps a status for answering with at the top level.
func Document(info *StatusInfo) StatusDoc {
	return StatusDoc{StatusInfo: info}
}

type CPUInfo struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
}

type MemInfo struct {
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	Percent float64 `json:"usage_percent"`
}

type DiskInfo struct {
	Mount   string  `json:"mount"`
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	Percent float64 `json:"usage_percent"`
}

// goos is the platform Status reads for. It is runtime.GOOS, held in a
// variable so a test can ask what happens on a platform it is not running on.
var goos = runtime.GOOS

// Status reads this machine. On a platform it has no way to read, it fails
// rather than answering: a status of zeros looks like an idle machine, and
// report, doctor and alerts would all pass it on as one (#384).
func Status() (*StatusInfo, error) {
	hostname, _ := os.Hostname()

	info := &StatusInfo{
		Hostname: hostname,
		OS:       goos,
		Arch:     runtime.GOARCH,
		Time:     time.Now().Format(time.RFC3339),
	}

	switch goos {
	case "linux", "darwin":
		info.Uptime = getUptime()
		info.CPU = getCPU()
		info.Memory = getMemory()
		info.Disks = getDisks()
	case "windows":
		r, err := readWindows()
		if err != nil {
			return nil, err
		}
		info.Uptime = r.uptime
		info.CPU = r.cpu
		info.Memory = r.memory
		info.Disks = r.disks
	default:
		return nil, fmt.Errorf("unsupported OS: %s", goos)
	}

	return info, nil
}

// readings is what a platform reader fills in for Status.
type readings struct {
	uptime string
	cpu    CPUInfo
	memory MemInfo
	disks  []DiskInfo
}

// formatUptime renders time since boot the way every platform prints it.
func formatUptime(dur time.Duration) string {
	days := int(dur.Hours() / 24)
	hours := int(dur.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(dur.Minutes())%60)
}

func getUptime() string {
	switch goos {
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
		return formatUptime(time.Since(time.Unix(sec, 0)))
	case "linux":
		out, err := util.RunCmd("cat", "/proc/uptime")
		if err != nil {
			return "unknown"
		}
		var secs float64
		fmt.Sscanf(out, "%f", &secs)
		return formatUptime(time.Duration(secs) * time.Second)
	default:
		return "unknown"
	}
}

func getCPU() CPUInfo {
	cores := runtime.NumCPU()
	usage := 0.0

	switch goos {
	case "darwin":
		// iostat -c 2 gives two samples; we use the second (instant usage).
		// readDarwinCPUTimes already handles this internally, no double-call needed.
		t := readDarwinCPUTimes()
		if t != nil {
			// t.total = user+sys+idle, t.idle = idle → usage = (total-idle)/total*100
			if t.total > 0 {
				usage = ((t.total - t.idle) / t.total) * 100
			}
		}
	case "linux":
		t1 := readLinuxCPUTimes()
		time.Sleep(linuxCPUWindow)
		t2 := readLinuxCPUTimes()
		if t1 != nil && t2 != nil {
			usage = cpuDelta(t1, t2)
		}
	}

	if usage > 100 {
		usage = 100
	}

	return CPUInfo{
		UsagePercent: round2(usage),
		Cores:        cores,
	}
}

// linuxCPUWindow is how long CPU usage is measured over on Linux. /proc/stat
// counts in ticks, a hundred a second per core, so the window sets the
// resolution: 200ms on four cores is about 80 ticks, steps of 1.25%, and an
// idle Raspberry Pi at half a percent read 0.0% (#332). A second is about 400
// ticks, steps of 0.25%, and the same span macOS samples with `iostat -c 2`.
const linuxCPUWindow = time.Second

// cpuTimes holds cumulative CPU tick counts.
type cpuTimes struct {
	total float64
	idle  float64
}

// cpuDelta calculates CPU usage percentage from two samples.
func cpuDelta(t1, t2 *cpuTimes) float64 {
	dTotal := t2.total - t1.total
	dIdle := t2.idle - t1.idle
	if dTotal <= 0 {
		return 0
	}
	return ((dTotal - dIdle) / dTotal) * 100
}

// readLinuxCPUTimes reads cumulative CPU times from /proc/stat.
func readLinuxCPUTimes() *cpuTimes {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			return parseProcStatLine(line)
		}
	}
	return nil
}

// parseProcStatLine parses a "cpu ..." line from /proc/stat.
// Fields: cpu user nice system idle iowait irq softirq steal guest guest_nice
func parseProcStatLine(line string) *cpuTimes {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil
	}
	var user, nice, sys, idle, iowait, irq, softirq, steal float64
	if n, _ := fmt.Sscanf(fields[1], "%f", &user); n != 1 {
		return nil
	}
	if n, _ := fmt.Sscanf(fields[2], "%f", &nice); n != 1 {
		return nil
	}
	if n, _ := fmt.Sscanf(fields[3], "%f", &sys); n != 1 {
		return nil
	}
	if n, _ := fmt.Sscanf(fields[4], "%f", &idle); n != 1 {
		return nil
	}
	if len(fields) > 5 {
		fmt.Sscanf(fields[5], "%f", &iowait)
	}
	if len(fields) > 6 {
		fmt.Sscanf(fields[6], "%f", &irq)
	}
	if len(fields) > 7 {
		fmt.Sscanf(fields[7], "%f", &softirq)
	}
	if len(fields) > 8 {
		fmt.Sscanf(fields[8], "%f", &steal)
	}
	total := user + nice + sys + idle + iowait + irq + softirq + steal
	return &cpuTimes{total: total, idle: idle + iowait}
}

// readDarwinCPUTimes reads cumulative CPU times via host_processor_info.
// macOS doesn't have kern.cp_time, so we parse `top -l 1 -n 0 -stats cpu`
// or use sysctl machdep.xcpm.cpu_thermal_level as fallback.
// Simplest reliable approach: parse /usr/bin/top output for CPU usage line.
func readDarwinCPUTimes() *cpuTimes {
	// Use iostat which gives instant CPU breakdown: user sys idle
	// -w 1 = minimum 1 second interval, -c 2 = two samples (second is instant)
	out, err := util.RunCmd("/usr/sbin/iostat", "-c", "2", "-w", "1")
	if err != nil {
		return nil
	}
	// iostat -c 2 outputs header + 2 samples. Use the last line (second sample).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return nil
	}
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	fields := strings.Fields(lastLine)
	// iostat fields: KB/t tps MB/s us[3] sy[4] id[5] 1m 5m 15m
	if len(fields) < 6 {
		return nil
	}
	var user, sys, idle float64
	if n, _ := fmt.Sscanf(fields[3], "%f", &user); n != 1 {
		return nil
	}
	if n, _ := fmt.Sscanf(fields[4], "%f", &sys); n != 1 {
		return nil
	}
	if n, _ := fmt.Sscanf(fields[5], "%f", &idle); n != 1 {
		return nil
	}
	total := user + sys + idle
	return &cpuTimes{total: total, idle: idle}
}

func getMemory() MemInfo {
	switch goos {
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

// gib is the unit every platform reports memory and disk in: what df -h and
// /proc/meminfo call G, a binary gigabyte.
const gib = 1024 * 1024 * 1024

// memFromBytes turns a physical total and the part still available into the
// figures Status reports. Used is what is not available, as on Linux, where
// it is MemTotal less MemAvailable.
func memFromBytes(total, avail uint64) MemInfo {
	if avail > total {
		avail = total
	}
	used := total - avail
	return MemInfo{
		TotalGB: round2(float64(total) / gib),
		UsedGB:  round2(float64(used) / gib),
		Percent: round2(float64(used) / float64(total) * 100),
	}
}

// diskFromBytes turns a volume's size and free space into a DiskInfo.
func diskFromBytes(mount string, total, free uint64) DiskInfo {
	if free > total {
		free = total
	}
	used := total - free
	return DiskInfo{
		Mount:   mount,
		TotalGB: round2(float64(total) / gib),
		UsedGB:  round2(float64(used) / gib),
		Percent: round2(float64(used) / float64(total) * 100),
	}
}

// cpuTimesFromSystemTimes converts what Windows' GetSystemTimes returns into
// cpuTimes. Windows counts idle time inside kernel time, so the total is
// kernel plus user, not all three.
func cpuTimesFromSystemTimes(idle, kernel, user uint64) *cpuTimes {
	return &cpuTimes{total: float64(kernel + user), idle: float64(idle)}
}

// driveIdentity is what Windows says a drive letter is: the target
// QueryDosDevice resolves it to, and the volume name the mount manager has
// for it. Either may be missing.
type driveIdentity struct {
	dosTarget string
	dosOK     bool
	volume    string
	volumeOK  bool
}

// volumeKey decides whether a fixed drive is reported and, if it is, what
// identifies the storage behind it, so two letters for one volume are
// reported once.
//
// A letter QueryDosDevice resolves to \??\ is a path alias, which is what
// subst makes: subst Z: C:\Windows is \??\C:\Windows, subst Y: C:\ is \??\C:
// and even has C:'s volume name. It answers with that drive's numbers, so it
// is skipped. A real device is kept whether or not the mount manager names
// it: an ImDisk RAM disk, a WinFsp or Dokany mount (rclone, Cryptomator,
// SSHFS-Win) is attached with DefineDosDevice to \Device\..., is fixed, and
// has no volume name. Such a drive is identified by its device instead, and a
// drive that answers neither question is identified by its own root rather
// than dropped.
func volumeKey(root string, id driveIdentity) (key string, keep bool) {
	switch {
	case id.dosOK && strings.HasPrefix(id.dosTarget, `\??\`):
		return "", false
	case id.volumeOK:
		return id.volume, true
	case id.dosOK:
		return id.dosTarget, true
	default:
		return root, true
	}
}

// firstRootPerVolume keeps the first drive root of each volume volumeKey
// keeps.
func firstRootPerVolume(roots []string, identify func(root string) driveIdentity) []string {
	seen := make(map[string]bool)
	var kept []string
	for _, root := range roots {
		key, keep := volumeKey(root, identify(root))
		if !keep || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, root)
	}
	return kept
}
