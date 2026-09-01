package util

import (
	"errors"
	"fmt"
)

// HintError keeps an actionable suggestion separate from the underlying cause.
type HintError struct {
	Cause error
	Hint  string
}

func (e *HintError) Error() string {
	return fmt.Sprintf("%s\n\n  ⚠️  Try: %s", e.Cause, e.Hint)
}

func (e *HintError) Unwrap() error {
	return e.Cause
}

// NewHintError associates an actionable suggestion with an error.
func NewHintError(cause error, hint string) error {
	return &HintError{Cause: cause, Hint: hint}
}

// FormatError returns the actionable message by default and the complete
// cause plus hint when verbose output was requested.
func FormatError(err error, verbose bool) string {
	var hintErr *HintError
	if errors.As(err, &hintErr) {
		if verbose {
			return hintErr.Error()
		}
		return "permission denied — rerun with: " + hintErr.Hint
	}
	return err.Error()
}
