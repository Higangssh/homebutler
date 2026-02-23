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
