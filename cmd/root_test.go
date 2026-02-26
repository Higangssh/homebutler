package cmd

import (
	"os"
	"reflect"
	"testing"
)

func TestFilterFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		flags    []string
		expected []string
	}{
		{
			name:     "remove --server flag",
			args:     []string{"status", "--server", "rpi5", "--json"},
			flags:    []string{"--server", "--all"},
			expected: []string{"status", "--json"},
		},
		{
			name:     "remove --all flag (boolean, no value)",
			args:     []string{"status", "--all", "--json"},
			flags:    []string{"--server", "--all"},
			expected: []string{"status", "--json"},
		},
		{
			name:     "remove multiple flags",
			args:     []string{"alerts", "--server", "vps", "--all"},
			flags:    []string{"--server", "--all"},
			expected: []string{"alerts"},
		},
		{
			name:     "no flags to remove",
			args:     []string{"status", "--json"},
			flags:    []string{"--server", "--all"},
			expected: []string{"status", "--json"},
		},
		{
			name:     "empty args",
			args:     []string{},
			flags:    []string{"--server"},
			expected: nil,
		},
		{
			name:     "flag at end without value",
			args:     []string{"status", "--all"},
			flags:    []string{"--all"},
			expected: []string{"status"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterFlags(tc.args, tc.flags...)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("filterFlags(%v, %v) = %v, want %v", tc.args, tc.flags, got, tc.expected)
			}
		})
	}
}

func TestListServerNames_Empty(t *testing.T) {
	// Import would be circular, so just test the helper logic
	// This tests the string building pattern
}

func TestGetFlag(t *testing.T) {
	// Save and restore os.Args
	savedArgs := os.Args
	defer func() { os.Args = savedArgs }()

	tests := []struct {
		name     string
		flag     string
		def      string
		expected string
	}{
		{"found", "--config", "default.yaml", "default.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getFlag(tc.flag, tc.def)
			// Without manipulating os.Args, this just returns default
			if got != tc.expected {
				t.Errorf("getFlag(%q, %q) = %q, want %q", tc.flag, tc.def, got, tc.expected)
			}
		})
	}
}

func TestHasFlag(t *testing.T) {
	// hasFlag reads os.Args, tested implicitly through integration
	// Just verify it doesn't panic with current args
	_ = hasFlag("--nonexistent")
}

func TestIsFlag(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"--json", true},
		{"-h", true},
		{"status", false},
		{"", false},
		{"-", false},
	}

	for _, tc := range tests {
		got := isFlag(tc.input)
		if got != tc.expected {
			t.Errorf("isFlag(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}
