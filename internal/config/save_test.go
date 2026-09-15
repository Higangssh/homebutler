package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

// A config file is written by a person. Saving one value must not reformat
// their file, drop their comments, or delete the keys this version does not
// recognise — which is what regenerating it from a struct would do.
const handWritten = `# my homelab
servers:
  - name: nas          # the noisy one
    host: 192.168.1.20
    local: false

alerts:
  cpu: 90
  # memory is deliberately low here
  memory: 70
  disk: 90

# not a key homebutler knows about
experimental:
  something: true
`

func writeSaveFixture(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func saveOne(t *testing.T, path string, patch Patch) {
	t.Helper()
	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if err := Save(path, rev, patch); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestSaveChangesOnlyTheLineItWasAskedTo(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(75)}})

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	before := strings.Split(handWritten, "\n")
	got := strings.Split(string(after), "\n")
	if len(before) != len(got) {
		t.Fatalf("line count changed from %d to %d:\n%s", len(before), len(got), after)
	}

	var changed []string
	for i := range before {
		if before[i] != got[i] {
			changed = append(changed, before[i]+"  ->  "+got[i])
		}
	}
	if len(changed) != 1 {
		t.Fatalf("expected one changed line, got %d:\n  %s", len(changed), strings.Join(changed, "\n  "))
	}
	if !strings.Contains(changed[0], "75") {
		t.Fatalf("the changed line is not the threshold: %s", changed[0])
	}
}

func TestSaveKeepsCommentsAndUnknownKeys(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	saveOne(t, path, Patch{Alerts: &AlertsPatch{Memory: f64(80)}})

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)

	for _, want := range []string{
		"# my homelab",
		"# the noisy one",
		"# memory is deliberately low here",
		"# not a key homebutler knows about",
		"experimental:",
		"something: true",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("save dropped %q:\n%s", want, text)
		}
	}
}

// Someone with the dashboard open and an editor in another window is ordinary.
// Their edit must not be discarded by a save that never saw it.
func TestSaveRefusesAFileThatChangedSinceItWasRead(t *testing.T) {
	path := writeSaveFixture(t, handWritten)

	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(handWritten, "disk: 90", "disk: 55", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Save(path, rev, Patch{Alerts: &AlertsPatch{CPU: f64(75)}})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("expected ErrStale, got %v", err)
	}

	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "disk: 55") {
		t.Fatal("the hand edit was overwritten")
	}
}

func TestSaveAcceptsAFileThatHasNotChanged(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, rev, Patch{Alerts: &AlertsPatch{CPU: f64(75)}}); err != nil {
		t.Fatalf("save: %v", err)
	}
}

// A config kept in a dotfiles repository is usually a symlink. Renaming onto
// the link replaces it with a regular file and detaches it from the repo.
func TestSaveWritesThroughASymlink(t *testing.T) {
	real := writeSaveFixture(t, handWritten)

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "config.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	saveOne(t, link, Patch{Alerts: &AlertsPatch{Disk: f64(80)}})

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}

	contents, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "disk: 80") {
		t.Fatalf("the change did not reach the real file:\n%s", contents)
	}
}

func TestSaveKeepsThePermissionsTightAndDoesNotLoosenThem(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}

	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(75)}})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o400 {
		t.Fatalf("permissions changed from 0400 to %04o", perm)
	}
}

func TestSaveTightensAFileThatWasTooOpen(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(75)}})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 for a file that was world-readable, got %04o", perm)
	}
}

func TestSaveAddsASectionThatWasNotThere(t *testing.T) {
	path := writeSaveFixture(t, "servers:\n  - name: nas\n    host: 192.168.1.20\n")
	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(85)}})

	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "alerts:") || !strings.Contains(string(after), "cpu: 85") {
		t.Fatalf("the section was not added:\n%s", after)
	}
	if !strings.Contains(string(after), "name: nas") {
		t.Fatalf("the existing content was lost:\n%s", after)
	}
}

func TestSaveRejectsAPatchThatChangesNothing(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	rev, _ := ReadRevision(path)
	if err := Save(path, rev, Patch{}); err == nil {
		t.Fatal("expected an empty patch to be refused")
	}
}

// A threshold is written the way a person writes one.
func TestSaveWritesNumbersWithoutTrailingZeroes(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(92.5), Memory: f64(80)}})

	after, _ := os.ReadFile(path)
	text := string(after)
	if !strings.Contains(text, "cpu: 92.5") {
		t.Errorf("expected cpu: 92.5:\n%s", text)
	}
	if !strings.Contains(text, "memory: 80") || strings.Contains(text, "memory: 80.0") {
		t.Errorf("expected memory: 80 rather than 80.0:\n%s", text)
	}
}

// The saved file has to be one homebutler will load back.
func TestSavedFileLoadsAgain(t *testing.T) {
	path := writeSaveFixture(t, handWritten)
	saveOne(t, path, Patch{Alerts: &AlertsPatch{CPU: f64(75), Memory: f64(80), Disk: f64(85)}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if cfg.Alerts.CPU != 75 || cfg.Alerts.Memory != 80 || cfg.Alerts.Disk != 85 {
		t.Fatalf("thresholds did not round-trip: %+v", cfg.Alerts)
	}
}

// The pre-write validation is not decoration: a save that would produce a file
// homebutler then refuses has to fail before it replaces anything. Flapping
// thresholds are the reachable error today — a negative one is rejected by
// Validate — and a save carrying that shape in the file it is editing must not
// go through.
func TestSaveRefusesWhenTheResultWouldBeInvalid(t *testing.T) {
	path := writeSaveFixture(t, `alerts:
  cpu: 90
watch:
  flapping:
    short_threshold: -3
`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Save(path, rev, Patch{Alerts: &AlertsPatch{CPU: f64(75)}})
	if err == nil {
		t.Fatal("expected the save to be refused")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unexpected error: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the file was changed by a save that was refused")
	}
}
