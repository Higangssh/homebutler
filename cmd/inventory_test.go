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
