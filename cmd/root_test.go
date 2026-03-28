package cmd

import (
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
