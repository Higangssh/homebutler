package cmd

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/spf13/cobra"
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

// The --filter usage string is derived from SupportedFilters, so it lists
// exactly what the renderer accepts and never goes stale.
func TestInventoryFilterFlagUsageListsSupportedFilters(t *testing.T) {
	for _, cmd := range []*cobra.Command{newInventoryScanCmd(), newInventoryShowCmd()} {
		flag := cmd.Flags().Lookup("filter")
		if flag == nil {
			t.Fatalf("%s must register --filter", cmd.Name())
		}
		for _, f := range inventory.SupportedFilters() {
			if !strings.Contains(flag.Usage, f) {
				t.Errorf("%s --filter usage %q does not list supported filter %q", cmd.Name(), flag.Usage, f)
			}
		}
	}
}
