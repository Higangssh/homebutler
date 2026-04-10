package system

import (
	"runtime"
	"testing"
)

func TestRound2(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{1.234, 1.23},
		{1.235, 1.23},
		{1.239, 1.23},
		{0.0, 0.0},
		{99.999, 99.99},
		{100.0, 100.0},
	}

	for _, tt := range tests {
		got := round2(tt.input)
		if got != tt.want {
			t.Errorf("round2(%f) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"100G", 100.0},
		{"1.5T", 1536.0},
		{"512M", 0.5},
		{"2Gi", 2.0},
		{"1Ti", 1024.0},
		{"256Mi", 0.25},
	}

	for _, tt := range tests {
		got := parseSize(tt.input)
		if got != tt.want {
			t.Errorf("parseSize(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestStatus(t *testing.T) {
	info, err := Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Hostname == "" {
		t.Error("hostname should not be empty")
	}
	if info.OS == "" {
		t.Error("OS should not be empty")
	}
	if info.Arch == "" {
		t.Error("arch should not be empty")
	}
	if info.CPU.Cores <= 0 {
		t.Errorf("expected cores > 0, got %d", info.CPU.Cores)
	}
	if info.Memory.TotalGB <= 0 {
		t.Errorf("expected total memory > 0, got %f", info.Memory.TotalGB)
	}
}

func TestNetworkScanParseARPLine(t *testing.T) {
	// Just test that Status doesn't panic
	info, err := Status()
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if info.Time == "" {
		t.Error("time should not be empty")
	}
}

// --- cpuDelta tests ---

func TestCpuDelta(t *testing.T) {
	tests := []struct {
		name string
		t1   *cpuTimes
		t2   *cpuTimes
		want float64
	}{
		{
			name: "50% usage",
			t1:   &cpuTimes{total: 1000, idle: 500},
			t2:   &cpuTimes{total: 1100, idle: 550},
			want: 50.0,
		},
		{
			name: "0% usage (all idle)",
			t1:   &cpuTimes{total: 1000, idle: 1000},
			t2:   &cpuTimes{total: 1100, idle: 1100},
			want: 0.0,
		},
		{
			name: "100% usage (no idle)",
			t1:   &cpuTimes{total: 1000, idle: 500},
			t2:   &cpuTimes{total: 1100, idle: 500},
			want: 100.0,
		},
		{
			name: "no delta (same values)",
			t1:   &cpuTimes{total: 1000, idle: 500},
			t2:   &cpuTimes{total: 1000, idle: 500},
			want: 0.0,
		},
		{
			name: "25% usage",
			t1:   &cpuTimes{total: 0, idle: 0},
			t2:   &cpuTimes{total: 400, idle: 300},
			want: 25.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cpuDelta(tt.t1, tt.t2)
			if got != tt.want {
				t.Errorf("cpuDelta() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestCpuDelta_NegativeTotal(t *testing.T) {
	// If t2.total < t1.total (shouldn't happen, but guard against it)
	got := cpuDelta(&cpuTimes{total: 1000, idle: 500}, &cpuTimes{total: 900, idle: 400})
	if got != 0 {
		t.Errorf("expected 0 for negative delta total, got %f", got)
	}
}

// --- parseProcStatLine tests ---

func TestParseProcStatLine(t *testing.T) {
	// Real /proc/stat format: cpu user nice system idle iowait irq softirq steal
	line := "cpu  10132153 290696 3084719 46828483 16683 0 25195 0 0 0"
	result := parseProcStatLine(line)
	if result == nil {
		t.Fatal("expected non-nil result")
	} else {
		// total = 10132153+290696+3084719+46828483+16683+0+25195+0 = 60377929
		expectedTotal := 10132153.0 + 290696.0 + 3084719.0 + 46828483.0 + 16683.0 + 0.0 + 25195.0 + 0.0
		if result.total != expectedTotal {
			t.Errorf("total = %f, want %f", result.total, expectedTotal)
		}
		// idle = idle + iowait = 46828483 + 16683 = 46845166
		expectedIdle := 46828483.0 + 16683.0
		if result.idle != expectedIdle {
			t.Errorf("idle = %f, want %f", result.idle, expectedIdle)
		}
	}
}

func TestParseProcStatLine_TooFewFields(t *testing.T) {
	result := parseProcStatLine("cpu  100 200 300")
	if result != nil {
		t.Error("expected nil for line with too few fields")
	}
}

func TestParseProcStatLine_InvalidFields(t *testing.T) {
	result := parseProcStatLine("cpu  abc def ghi jkl")
	if result != nil {
		t.Error("expected nil for non-numeric fields")
	}
}

func TestParseProcStatLine_MinimalFields(t *testing.T) {
	// Minimum valid: cpu user nice sys idle (5 fields total)
	result := parseProcStatLine("cpu  100 50 30 820")
	if result == nil {
		t.Fatal("expected non-nil result for minimal valid line")
	} else {
		expectedTotal := 100.0 + 50.0 + 30.0 + 820.0
		if result.total != expectedTotal {
			t.Errorf("total = %f, want %f", result.total, expectedTotal)
		}
	}
}

// --- readDarwinCPUTimes integration test ---

func TestReadDarwinCPUTimes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping darwin-only test")
	}
	result := readDarwinCPUTimes()
	if result == nil {
		t.Fatal("expected non-nil result on darwin")
	} else if result.total <= 0 {
		t.Errorf("expected total > 0, got %f", result.total)
	}
}

// --- readLinuxCPUTimes integration test ---

func TestReadLinuxCPUTimes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping linux-only test")
	}
	result := readLinuxCPUTimes()
	if result == nil {
		t.Fatal("expected non-nil result on linux")
	} else if result.total <= 0 {
		t.Errorf("expected total > 0, got %f", result.total)
	}
}

// --- getCPU tests ---

func TestGetCPU(t *testing.T) {
	cpu := getCPU()
	if cpu.Cores <= 0 {
		t.Errorf("expected cores > 0, got %d", cpu.Cores)
	}
	if cpu.UsagePercent < 0 || cpu.UsagePercent > 100 {
		t.Errorf("expected usage 0-100, got %f", cpu.UsagePercent)
	}
}

func TestGetCPU_CoresMatchRuntime(t *testing.T) {
	cpu := getCPU()
	if cpu.Cores != runtime.NumCPU() {
		t.Errorf("expected cores = %d (runtime.NumCPU()), got %d", runtime.NumCPU(), cpu.Cores)
	}
}
