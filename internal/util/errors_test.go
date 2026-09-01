package util

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFormatError(t *testing.T) {
	cause := errors.New("rename /usr/local/bin/homebutler: permission denied")
	err := NewHintError("cannot backup current binary /usr/local/bin/homebutler.bak", cause, "sudo homebutler upgrade")

	short := FormatError(err, false)
	if short != "cannot backup current binary /usr/local/bin/homebutler.bak — rerun with: sudo homebutler upgrade" {
		t.Fatalf("short error = %q", short)
	}
	verbose := FormatError(err, true)
	if !strings.Contains(verbose, cause.Error()) || !strings.Contains(verbose, "sudo homebutler upgrade") {
		t.Fatalf("verbose error = %q", verbose)
	}
	if !errors.Is(err, cause) {
		t.Fatal("HintError does not unwrap its cause")
	}
}

func TestFormatErrorWrapped(t *testing.T) {
	err := NewHintError("cannot create backup dir /mnt/nas/backups", errors.New("permission denied"), "sudo homebutler backup")
	wrapped := fmt.Errorf("operation failed: %w", err)
	if got := FormatError(wrapped, false); got != "cannot create backup dir /mnt/nas/backups — rerun with: sudo homebutler backup" {
		t.Fatalf("wrapped short error = %q", got)
	}
}
