package system

import "testing"

func TestParseProcesses(t *testing.T) {
	output := `  PID  %CPU %MEM COMMAND
 1234  25.0  3.2 /usr/bin/node
  567  12.5  1.8 /usr/sbin/httpd
  890   5.0  0.5 vim`

	procs := parseProcesses(output, 5)
	if len(procs) != 3 {
		t.Fatalf("expected 3 processes, got %d", len(procs))
	}
	if procs[0].PID != 1234 {
		t.Errorf("expected PID 1234, got %d", procs[0].PID)
	}
	if procs[0].CPU != 25.0 {
		t.Errorf("expected CPU 25.0, got %f", procs[0].CPU)
	}
	if procs[0].Mem != 3.2 {
		t.Errorf("expected Mem 3.2, got %f", procs[0].Mem)
	}
	if procs[0].Name != "node" {
		t.Errorf("expected name 'node', got %q", procs[0].Name)
	}
	if procs[2].Name != "vim" {
		t.Errorf("expected name 'vim', got %q", procs[2].Name)
	}
}

func TestParseProcesses_PathWithSpaces(t *testing.T) {
	output := `  PID  %CPU %MEM COMMAND
  100  10.0  2.0 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome`

	procs := parseProcesses(output, 5)
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}
	if procs[0].Name != "Google Chrome" {
		t.Errorf("expected name 'Google Chrome', got %q", procs[0].Name)
	}
}

func TestParseProcesses_Empty(t *testing.T) {
	procs := parseProcesses("", 5)
	if len(procs) != 0 {
		t.Errorf("expected 0 processes for empty input, got %d", len(procs))
	}
}

func TestParseProcesses_HeaderOnly(t *testing.T) {
	procs := parseProcesses("  PID  %CPU %MEM COMMAND", 5)
	if len(procs) != 0 {
		t.Errorf("expected 0 processes for header-only input, got %d", len(procs))
	}
}

func TestParseProcesses_Limit(t *testing.T) {
	output := `  PID  %CPU %MEM COMMAND
    1  10.0  1.0 a
    2   9.0  1.0 b
    3   8.0  1.0 c
    4   7.0  1.0 d
    5   6.0  1.0 e`

	procs := parseProcesses(output, 3)
	if len(procs) != 3 {
		t.Fatalf("expected 3 processes (limited), got %d", len(procs))
	}
	if procs[2].PID != 3 {
		t.Errorf("expected last PID 3, got %d", procs[2].PID)
	}
}

func TestTopProcesses(t *testing.T) {
	procs, err := TopProcesses(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(procs) == 0 {
		t.Error("expected at least one process")
	}
	if len(procs) > 5 {
		t.Errorf("expected at most 5 processes, got %d", len(procs))
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("expected positive PID, got %d", p.PID)
		}
		if p.Name == "" {
			t.Error("expected non-empty process name")
		}
	}
}
