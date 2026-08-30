package cmd

import (
	"strings"
	"testing"
)

// watch start used to return when the watch list was empty. Under a supervisor
// that restarts on exit, that is a loop: the process starts, prints one line,
// exits, and is started again every throttle interval.
//
// It is also wrong on its own merits since #97 — thresholds and remediation
// rules run in this process and neither needs a watch target.
func TestWatchStartDoesNotExitOnAnEmptyWatchList(t *testing.T) {
	src := readSource(t, "watch.go")

	// The empty-list branch must not return; it should say what it is doing.
	i := strings.Index(src, "Nothing on the watch list")
	if i < 0 {
		t.Fatal("watch start no longer reports an empty watch list at all")
	}
	// Look at the few lines that follow the message.
	tail := src[i:]
	if end := strings.Index(tail, "// Load watch config"); end > 0 {
		tail = tail[:end]
	}
	if strings.Contains(tail, "return nil") {
		t.Error("watch start still returns on an empty watch list, which a supervisor turns into a restart loop")
	}
}
