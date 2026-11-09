//go:build windows

package system

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no df, /proc or sysctl to shell out to, so the readings come
// from the Win32 calls that answer the same questions. golang.org/x/sys
// wraps the disk calls and GetTickCount64; it does not wrap
// GlobalMemoryStatusEx or GetSystemTimes, so those two are loaded here.
var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
)

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

func readWindows() (readings, error) {
	mem, err := windowsMemory()
	if err != nil {
		return readings{}, err
	}
	disks, err := windowsDisks()
	if err != nil {
		return readings{}, err
	}
	usage, err := windowsCPUUsage()
	if err != nil {
		return readings{}, err
	}
	return readings{
		uptime: formatUptime(windows.DurationSinceBoot()),
		cpu:    CPUInfo{UsagePercent: round2(usage), Cores: runtime.NumCPU()},
		memory: mem,
		disks:  disks,
	}, nil
}

func windowsMemory() (MemInfo, error) {
	var m memoryStatusEx
	m.length = uint32(unsafe.Sizeof(m))
	if r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return MemInfo{}, fmt.Errorf("reading memory: GlobalMemoryStatusEx: %w", err)
	}
	if m.totalPhys == 0 {
		return MemInfo{}, fmt.Errorf("reading memory: GlobalMemoryStatusEx reported no physical memory")
	}
	return memFromBytes(m.totalPhys, m.availPhys), nil
}

func systemTimes() (*cpuTimes, error) {
	var idle, kernel, user windows.Filetime
	r, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return nil, fmt.Errorf("reading CPU: GetSystemTimes: %w", err)
	}
	return cpuTimesFromSystemTimes(filetime(idle), filetime(kernel), filetime(user)), nil
}

func filetime(f windows.Filetime) uint64 {
	return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime)
}

// windowsCPUUsage measures over the same window Linux does. GetSystemTimes
// counts in 100ns units, so the resolution problem behind linuxCPUWindow is
// not there, but a shorter window would make the two platforms' figures
// mean different things.
func windowsCPUUsage() (float64, error) {
	t1, err := systemTimes()
	if err != nil {
		return 0, err
	}
	time.Sleep(linuxCPUWindow)
	t2, err := systemTimes()
	if err != nil {
		return 0, err
	}
	if t2.total <= t1.total {
		return 0, fmt.Errorf("reading CPU: GetSystemTimes did not advance over %s", linuxCPUWindow)
	}
	usage := cpuDelta(t1, t2)
	if usage > 100 {
		usage = 100
	}
	return usage, nil
}

// windowsDisks reports every fixed volume once, the way df's / and /home
// are reported elsewhere. Removable, network and optical drives are left
// out: they come and go, and a network share's size is the server's.
//
// A drive letter is not a volume. `subst Z: C:\Windows` makes a Z: that
// GetDriveType calls fixed and GetDiskFreeSpaceEx answers with C:'s numbers,
// and one volume can be given two letters. Listed as they come, one full
// disk would be reported, and alerted on, once per letter. volumeKey says
// which letters are kept.
func windowsDisks() ([]DiskInfo, error) {
	buf := make([]uint16, 254)
	n, err := windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0])
	if err != nil {
		return nil, fmt.Errorf("reading disks: GetLogicalDriveStrings: %w", err)
	}
	if int(n) > len(buf) {
		buf = make([]uint16, n)
		if n, err = windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0]); err != nil {
			return nil, fmt.Errorf("reading disks: GetLogicalDriveStrings: %w", err)
		}
	}

	var fixed []string
	for _, root := range splitDriveStrings(buf[:n]) {
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(p) == windows.DRIVE_FIXED {
			fixed = append(fixed, root)
		}
	}

	var disks []DiskInfo
	var lastErr error
	for _, root := range firstRootPerVolume(fixed, identifyDrive) {
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		var freeToCaller, total, free uint64
		if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &free); err != nil {
			lastErr = fmt.Errorf("reading disk %s: GetDiskFreeSpaceEx: %w", root, err)
			continue
		}
		if total == 0 {
			continue
		}
		disks = append(disks, diskFromBytes(root, total, free))
	}
	if len(disks) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("reading disks: no fixed drive could be read")
	}
	return disks, nil
}

// identifyDrive asks Windows what a drive root is: what its letter is
// defined as (QueryDosDevice, on "C:" without the backslash) and the
// \\?\Volume{GUID}\ name the mount manager has for it, if any.
func identifyDrive(root string) driveIdentity {
	var id driveIdentity
	if letter, err := windows.UTF16PtrFromString(strings.TrimSuffix(root, `\`)); err == nil {
		buf := make([]uint16, windows.MAX_PATH)
		if n, err := windows.QueryDosDevice(letter, &buf[0], uint32(len(buf))); err == nil && n > 0 {
			// The answer is a list; the first entry is the current definition.
			id.dosTarget, id.dosOK = windows.UTF16ToString(buf), true
		}
	}
	if p, err := windows.UTF16PtrFromString(root); err == nil {
		buf := make([]uint16, windows.MAX_PATH)
		if err := windows.GetVolumeNameForVolumeMountPoint(p, &buf[0], uint32(len(buf))); err == nil {
			id.volume, id.volumeOK = windows.UTF16ToString(buf), true
		}
	}
	return id
}

// splitDriveStrings splits GetLogicalDriveStrings' buffer, roots separated
// by NULs (C:\ NUL D:\ NUL), into the roots.
func splitDriveStrings(buf []uint16) []string {
	var roots []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				roots = append(roots, windows.UTF16ToString(buf[start:i]))
			}
			start = i + 1
		}
	}
	return roots
}
