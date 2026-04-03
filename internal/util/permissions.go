package util

import (
	"fmt"
	"os"
	"strings"
)

// IsPermissionError checks if the error is a permission-related error.
func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted")
}

// PermissionHint wraps an error with a sudo hint if it's a permission error.
// command is the suggested sudo command (e.g., "sudo homebutler upgrade").
func PermissionHint(err error, command string) error {
	if err == nil {
		return nil
	}
	if IsPermissionError(err) {
		return fmt.Errorf("%w\n\n  ⚠️  Try: %s", err, command)
	}
	return err
}
