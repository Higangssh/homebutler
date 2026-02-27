package docker

import "testing"

func TestIsValidName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple", "nginx", true},
		{"with-hyphen", "my-container", true},
		{"with-underscore", "my_container", true},
		{"with-dot", "app.v2", true},
		{"with-numbers", "redis3", true},
		{"mixed", "my-app_v2.1", true},
		{"empty", "", false},
		{"semicolon-injection", "nginx;rm -rf /", false},
		{"pipe-injection", "nginx|cat /etc/passwd", false},
		{"backtick-injection", "nginx`whoami`", false},
		{"dollar-injection", "nginx$(id)", false},
		{"space", "my container", false},
		{"slash", "../etc/passwd", false},
		{"ampersand", "nginx&&echo pwned", false},
		{"too-long", string(make([]byte, 129)), false},
		{"max-length", string(make([]byte, 128)), false}, // all zero bytes → invalid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidName(tt.input)
			if got != tt.want {
				t.Errorf("isValidName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	input := "line1\nline2\nline3"
	lines := splitLines(input)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestSplitTabs(t *testing.T) {
	input := "a\tb\tc"
	parts := splitTabs(input)
	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(parts))
	}
	if parts[0] != "a" || parts[1] != "b" || parts[2] != "c" {
		t.Errorf("unexpected parts: %v", parts)
	}
}

func TestFriendlyStatus(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		state string
		want  string
	}{
		{"running-days", "Up 4 days", "running", "Running · 4d"},
		{"running-hours", "Up 6 hours", "running", "Running · 6h"},
		{"running-minutes", "Up 30 minutes", "running", "Running · 30m"},
		{"running-day", "Up 1 day", "running", "Running · 1d"},
		{"running-hour", "Up 1 hour", "running", "Running · 1h"},
		{"running-minute", "Up 1 minute", "running", "Running · 1m"},
		{"running-week", "Up 2 weeks", "running", "Running · 2w"},
		{"running-month", "Up 3 months", "running", "Running · 3mo"},
		{"exited-hours", "Exited (0) 6 hours ago", "exited", "Stopped · 6h ago"},
		{"exited-days", "Exited (137) 2 days ago", "exited", "Stopped · 2d ago"},
		{"exited-minutes", "Exited (1) 30 minutes ago", "exited", "Stopped · 30m ago"},
		{"unknown-state", "Paused", "paused", "Paused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := friendlyStatus(tt.raw, tt.state)
			if got != tt.want {
				t.Errorf("friendlyStatus(%q, %q) = %q, want %q", tt.raw, tt.state, got, tt.want)
			}
		})
	}
}

func TestShortenDuration(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"4 days", "4d"},
		{"1 day", "1d"},
		{"6 hours", "6h"},
		{"1 hour", "1h"},
		{"30 minutes", "30m"},
		{"1 minute", "1m"},
		{"45 seconds", "45s"},
		{"1 second", "1s"},
		{"2 weeks", "2w"},
		{"1 week", "1w"},
		{"3 months", "3mo"},
		{"1 month", "1mo"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortenDuration(tt.input)
			if got != tt.want {
				t.Errorf("shortenDuration(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitLinesEdgeCases(t *testing.T) {
	// Empty string should return single empty element
	lines := splitLines("")
	if len(lines) != 1 || lines[0] != "" {
		t.Errorf("splitLines(\"\") = %v, want [\"\"]", lines)
	}

	// Single line (no newline)
	lines = splitLines("hello")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("splitLines(\"hello\") = %v", lines)
	}

	// Trailing newline
	lines = splitLines("a\nb\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 elements, got %d: %v", len(lines), lines)
	}
}

func TestSplitTabsEdgeCases(t *testing.T) {
	// Single field (no tabs)
	parts := splitTabs("single")
	if len(parts) != 1 || parts[0] != "single" {
		t.Errorf("splitTabs(\"single\") = %v", parts)
	}

	// Empty field between tabs
	parts = splitTabs("a\t\tc")
	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d: %v", len(parts), parts)
	}
	if parts[1] != "" {
		t.Errorf("middle part = %q, want empty", parts[1])
	}
}

func TestSplit(t *testing.T) {
	result := split("a,b,c", ',')
	if len(result) != 3 {
		t.Errorf("expected 3 parts, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestIsValidNameMaxLength(t *testing.T) {
	// Exactly 128 valid characters should be valid
	name := make([]byte, 128)
	for i := range name {
		name[i] = 'a'
	}
	if !isValidName(string(name)) {
		t.Error("expected 128-char valid name to be valid")
	}

	// 129 valid characters should be invalid
	name = make([]byte, 129)
	for i := range name {
		name[i] = 'a'
	}
	if isValidName(string(name)) {
		t.Error("expected 129-char name to be invalid")
	}
}

func TestContainerStruct(t *testing.T) {
	c := Container{
		ID:     "a1b2c3d4e5f6",
		Name:   "nginx",
		Image:  "nginx:1.25",
		Status: "Running · 4d",
		State:  "running",
		Ports:  "0.0.0.0:80->80/tcp",
	}
	if c.Name != "nginx" {
		t.Errorf("Name = %q, want %q", c.Name, "nginx")
	}
}

func TestActionResultStruct(t *testing.T) {
	r := ActionResult{
		Action:    "restart",
		Container: "nginx",
		Status:    "ok",
	}
	if r.Action != "restart" {
		t.Errorf("Action = %q, want %q", r.Action, "restart")
	}
}

func TestLogsResultStruct(t *testing.T) {
	r := LogsResult{
		Container: "nginx",
		Lines:     "50",
		Logs:      "some log output",
	}
	if r.Container != "nginx" {
		t.Errorf("Container = %q, want %q", r.Container, "nginx")
	}
	if r.Lines != "50" {
		t.Errorf("Lines = %q, want %q", r.Lines, "50")
	}
}
