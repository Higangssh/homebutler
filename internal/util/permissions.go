package util

import (
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

