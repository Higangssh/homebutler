package cmd

import (
	"strings"
	"testing"
)

// The --json + --filter rejection happens before loadConfig, so the test can run
// without any config or collection setup.
func TestRunInventoryScanRejectsJSONWithFilter(t *testing.T) {
	oldJSON := jsonOutput
	jsonOutput = true
	defer func() { jsonOutput = oldJSON }()

	err := runInventoryScan("exposed")
	if err == nil {
		t.Fatal("expected error when --filter is combined with --json")
	}
	if !strings.Contains(err.Error(), "--filter") || !strings.Contains(err.Error(), "--json") {
		t.Errorf("unhelpful error for --filter + --json: %v", err)
	}
}

// show advertises "same as scan", so it must accept the same flags.
func TestNewInventoryShowCmdAcceptsFilter(t *testing.T) {
	cmd := newInventoryShowCmd()
	if err := cmd.ParseFlags([]string{"--filter", "exposed"}); err != nil {
		t.Fatalf("show must accept --filter: %v", err)
	}
	got, _ := cmd.Flags().GetString("filter")
	if got != "exposed" {
		t.Errorf("show --filter parsed %q, want \"exposed\"", got)
	}
}
