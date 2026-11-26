//go:build !windows

package watch

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func saveAndStat(t *testing.T, dir string) os.FileMode {
	t.Helper()
	inc := Incident{ID: "web-1", Container: "web", DetectedAt: findBase}
	if err := SaveIncident(dir, &inc, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(incidentsDir(dir), "web-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// A new incident gets 0644 under the umask, as os.WriteFile gave it, so an
// operator running under umask 077 still gets 0600.
func TestSaveIncidentHonoursTheUmask(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(incidentsDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o077)
	mode := saveAndStat(t, dir)
	syscall.Umask(old)
	if mode != 0o600 {
		t.Errorf("mode = %v under umask 077, want 0600", mode)
	}

	dir = t.TempDir()
	old = syscall.Umask(0o022)
	mode = saveAndStat(t, dir)
	syscall.Umask(old)
	if mode != 0o644 {
		t.Errorf("mode = %v under umask 022, want 0644", mode)
	}
}

// A re-save keeps the mode the file already had, as os.WriteFile did.
func TestSaveIncidentKeepsAnExistingFilesMode(t *testing.T) {
	dir := t.TempDir()
	saveAndStat(t, dir)
	if err := os.Chmod(filepath.Join(incidentsDir(dir), "web-1.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode := saveAndStat(t, dir); mode != 0o600 {
		t.Errorf("mode = %v after a re-save; want the 0600 it was narrowed to", mode)
	}
}
