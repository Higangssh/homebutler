package system

import (
	"testing"
	"time"
)

// A platform Status cannot read used to come back as a machine at 0% CPU with
// 0 GB of memory, which report and alerts passed on as healthy (#384).
func TestStatusRefusesAPlatformItCannotRead(t *testing.T) {
	old := goos
	goos = "plan9"
	defer func() { goos = old }()

	info, err := Status()
	if err == nil {
		t.Fatalf("Status() on plan9 = %+v, want an error", info)
	}
	if info != nil {
		t.Errorf("Status() returned a status alongside its error: %+v", info)
	}
	if want := "unsupported OS: plan9"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestMemFromBytes(t *testing.T) {
	got := memFromBytes(8*gib, 6*gib)
	want := MemInfo{TotalGB: 8, UsedGB: 2, Percent: 25}
	if got != want {
		t.Errorf("memFromBytes(8 GiB, 6 GiB free) = %+v, want %+v", got, want)
	}
	// More available than total is a reading that raced, not negative use.
	if got := memFromBytes(4*gib, 5*gib); got.UsedGB != 0 || got.Percent != 0 {
		t.Errorf("memFromBytes(avail > total) = %+v, want no use", got)
	}
}

func TestDiskFromBytes(t *testing.T) {
	got := diskFromBytes(`C:\`, 100*gib, 25*gib)
	want := DiskInfo{Mount: `C:\`, TotalGB: 100, UsedGB: 75, Percent: 75}
	if got != want {
		t.Errorf("diskFromBytes = %+v, want %+v", got, want)
	}
}

// GetSystemTimes includes idle time in kernel time. Adding all three would
// count idle twice and understate every reading.
func TestCPUTimesFromSystemTimesDoesNotCountIdleTwice(t *testing.T) {
	t1 := cpuTimesFromSystemTimes(0, 0, 0)
	// 600 idle, 800 kernel of which 600 is that idle, 200 user: 400 busy of 1000.
	t2 := cpuTimesFromSystemTimes(600, 800, 200)
	if got := cpuDelta(t1, t2); got != 40 {
		t.Errorf("usage = %v, want 40", got)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{90 * time.Minute, "1h 30m"},
		{50 * time.Hour, "2d 2h"},
	}
	for _, tt := range tests {
		if got := formatUptime(tt.d); got != tt.want {
			t.Errorf("formatUptime(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
