package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/watch"
)

// ContextFor is the path the tool and the CLI take: the attribution read
// from the watch list reaches the answer, both the sentence saying it was
// inferred and the warning about a list that could not be read.
func TestContextForCarriesTheAttributionThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HOMEBUTLER_CONTAINER", "1") // no current state is read either way
	watchDir := filepath.Join(home, ".homebutler", "watch")
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}
	if err := watch.SaveIncident(watchDir, &inc, 0); err != nil {
		t.Fatal(err)
	}
	if err := watch.SaveTargets(watchDir, []watch.Target{{Container: "web"}}); err != nil {
		t.Fatal(err)
	}

	got, err := ContextFor(nil, watchDir, "web-1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Note, "taken to be the container's because the watch list now has") {
		t.Errorf("note = %q, want it to say the kind was inferred from the watch list", got.Note)
	}

	if err := os.WriteFile(filepath.Join(watchDir, "targets.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ContextFor(nil, watchDir, "web-1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "watch list could not be read") || strings.Contains(got.Warnings[0], home) {
		t.Errorf("warnings = %q, want the unreadable watch list, without its path", got.Warnings)
	}
	if !strings.Contains(got.Unattributed, "watch list could not be read") {
		t.Errorf("unattributed = %q", got.Unattributed)
	}
}

// The inferred sentence follows a note that may end in an error string, and
// reads as a second sentence.
func TestJoinSentencesEndsTheFirstOne(t *testing.T) {
	got := joinSentences("nothing was compared: permission denied", "This incident does not record its kind.")
	if got != "nothing was compared: permission denied. This incident does not record its kind." {
		t.Errorf("joined = %q", got)
	}
	if got := joinSentences("Compared.", "Then this."); got != "Compared. Then this." {
		t.Errorf("joined = %q", got)
	}
	if got := joinSentences("", "Only this."); got != "Only this." {
		t.Errorf("joined = %q", got)
	}
}

// An incident not known to be a container's always says why, even when the
// caller gave no reason.
func TestIncidentContextDefaultsTheUnattributedReason(t *testing.T) {
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Unattributed == "" {
		t.Error("unattributed is empty for an incident with no container")
	}
}
