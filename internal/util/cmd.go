package util

import (
	"os/exec"
	"strings"
)

// RunCmd executes a command and returns trimmed stdout.
// It does NOT pass through a shell — arguments are explicit.
// This prevents command injection attacks.
func RunCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
