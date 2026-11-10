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
			name:     "remove doctor backup max age value flag",
			args:     []string{"doctor", "--backup-max-age", "24h", "--json"},
			flags:    []string{"--backup-max-age"},
			expected: []string{"doctor", "--json"},
		},
		{
			name:     "remove proxmox endpoint value flag",
			args:     []string{"proxmox", "status", "--endpoint", "pve", "--json"},
			flags:    []string{"--endpoint"},
			expected: []string{"proxmox", "status", "--json"},
		},
		{
			name:     "no flags to remove",
			args:     []string{"status", "--json"},
			flags:    []string{"--server", "--all"},
			expected: []string{"status", "--json"},
		},
		{
			name:     "keep verbose flag for remote",
			args:     []string{"status", "--server", "rpi5", "--verbose"},
			flags:    []string{"--server", "--all", "--config"},
			expected: []string{"status", "--verbose"},
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

// backup drill has an --all of its own, which shadows the global one, so
// `--server rpi5 backup drill --all` arrived at the Pi as `backup drill`, with
// neither an app nor --all, and failed on its arguments.
func TestForwardedArgsKeepACommandsOwnAll(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{
			args: []string{"--server", "rpi5", "backup", "drill", "--all"},
			want: []string{"backup", "drill", "--all"},
		},
		{
			args: []string{"--server=rpi5", "backup", "drill", "--all=true", "--json"},
			want: []string{"backup", "drill", "--all=true", "--json"},
		},
		{
			args: []string{"backup", "drill", "--all=false", "vaultwarden", "--server", "rpi5", "--config", "/tmp/x.yaml"},
			want: []string{"backup", "drill", "--all=false", "vaultwarden"},
		},
		// Without an --all of its own, any --all is the global one, and
		// forwarded it would make the server fan out again.
		{
			args: []string{"--server", "rpi5", "status", "--all", "--json"},
			want: []string{"status", "--json"},
		},
		{
			args: []string{"backup", "list", "--all=true", "--server=rpi5"},
			want: []string{"backup", "list"},
		},
	}
	for _, tc := range tests {
		cmd, _, err := rootCmd.Find(tc.args)
		if err != nil {
			t.Fatalf("Find(%v): %v", tc.args, err)
		}
		if got := forwardedArgs(cmd, tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("forwardedArgs(%s, %v) = %v, want %v", commandPath(cmd), tc.args, got, tc.want)
		}
	}
}

// Without --server, backup drill --all is still the drill's: every app in the
// archive here, not this command on every server.
func TestBackupDrillAllIsTheDrillsFlagLocally(t *testing.T) {
	ran := stubRuns(t)
	drill, _, err := rootCmd.Find([]string{"backup", "drill"})
	if err != nil {
		t.Fatal(err)
	}
	own := drill.Flags().Lookup("all")
	t.Cleanup(func() {
		_ = own.Value.Set("false")
		own.Changed = false
	})

	if err := executeRoot(t, ran, "backup", "drill", "--all"); err != nil {
		t.Fatal(err)
	}
	if *ran != "backup drill" {
		t.Fatalf("ran %q, want backup drill", *ran)
	}
	if allServers {
		t.Error("the global --all was set; want the drill's own")
	}
	if own.Value.String() != "true" {
		t.Errorf("backup drill's --all = %s, want true", own.Value.String())
	}
}
