package cmd

import (
	"testing"

	"github.com/Higangssh/homebutler/internal/backup"
)

// A drill that fails used to exit 0, which made the one thing the command
// exists to say invisible to whatever was running it.
func TestDrillVerdictFailsWhenTheBackupDoesNotComeBack(t *testing.T) {
	if err := drillVerdict("uptime-kuma", &backup.DrillResult{Passed: true}); err != nil {
		t.Fatalf("a passing drill must exit clean, got %v", err)
	}

	err := drillVerdict("vaultwarden", &backup.DrillResult{Passed: false})
	if err == nil {
		t.Fatal("a failed drill exited 0: a cron job would read a backup that does not restore as success")
	}
	if got := err.Error(); got != "vaultwarden did not come back from the backup" {
		t.Errorf("the failure should name the app, got %q", got)
	}
}

func TestDrillAllVerdictFailsWhenAnyAppDoesNotComeBack(t *testing.T) {
	clean := &backup.DrillReport{Total: 3, Passed: 3}
	if err := drillAllVerdict(clean); err != nil {
		t.Fatalf("every app came back, got %v", err)
	}

	// One failure out of several is still a failure: --all is not a vote.
	err := drillAllVerdict(&backup.DrillReport{Total: 3, Passed: 2, Failed: 1})
	if err == nil {
		t.Fatal("one app failed to come back and --all exited 0")
	}
	if got := err.Error(); got != "1 of 3 apps did not come back from the backup" {
		t.Errorf("the failure should count what failed, got %q", got)
	}
}
