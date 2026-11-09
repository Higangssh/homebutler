package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandKeyHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := []struct {
		in, want string
	}{
		{"~/.ssh/id_ed25519", filepath.Join(home, ".ssh", "id_ed25519")},
		{"/etc/keys/id", "/etc/keys/id"},
		{"~user/.ssh/id", "~user/.ssh/id"},
		{"keys/id", "keys/id"},
	}
	for _, tt := range tests {
		if got := expandKeyHome(tt.in); got != tt.want {
			t.Errorf("expandKeyHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
