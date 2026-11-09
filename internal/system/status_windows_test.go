//go:build windows

package system

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// Before #384 every one of these came back empty on Windows: uptime
// "unknown", 0.0 / 0.0 GB of memory, 0% CPU and no disks.
func TestStatusReadsThisWindowsMachine(t *testing.T) {
	info, err := Status()
	if err != nil {
		t.Fatalf("Status() = %v", err)
	}
	if info.Uptime == "unknown" || info.Uptime == "" {
		t.Errorf("uptime = %q, want a duration", info.Uptime)
	}
	if info.Memory.TotalGB <= 0 || info.Memory.UsedGB <= 0 {
		t.Errorf("memory = %+v, want a total and a use above zero", info.Memory)
	}
	if info.Memory.UsedGB > info.Memory.TotalGB {
		t.Errorf("memory = %+v, used more than total", info.Memory)
	}
	if info.CPU.UsagePercent < 0 || info.CPU.UsagePercent > 100 {
		t.Errorf("cpu usage = %v, want 0-100", info.CPU.UsagePercent)
	}
	if len(info.Disks) == 0 {
		t.Fatal("no disks, want at least the system drive")
	}
	for _, d := range info.Disks {
		if !strings.HasSuffix(d.Mount, `:\`) {
			t.Errorf("disk mount = %q, want a drive root like C:\\", d.Mount)
		}
		if d.TotalGB <= 0 {
			t.Errorf("disk %s total = %v, want above zero", d.Mount, d.TotalGB)
		}
	}
}

func TestSplitDriveStrings(t *testing.T) {
	buf := utf16.Encode([]rune("C:\\\x00D:\\\x00"))
	got := splitDriveStrings(buf)
	if len(got) != 2 || got[0] != `C:\` || got[1] != `D:\` {
		t.Errorf("splitDriveStrings = %q, want [C:\\ D:\\]", got)
	}
}
