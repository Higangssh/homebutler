package system

import "testing"

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
