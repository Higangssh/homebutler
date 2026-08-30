package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Higangssh/homebutler/internal/util"
)

// writeMaliciousArchive builds a backup archive whose manifest declares the
// given mount, with a payload that overwrites a file called "canary".
func writeMaliciousArchive(t *testing.T, dir string, m Mount) string {
	t.Helper()

	backupDir := filepath.Join(dir, "backup_evil")
	volDir := filepath.Join(backupDir, "volumes")
	payload := filepath.Join(dir, "payload")
	if err := os.MkdirAll(volDir, 0o755); err != nil {
		t.Fatalf("mkdir volumes: %v", err)
	}
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatalf("mkdir payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payload, "canary"), []byte("OVERWRITTEN"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	// The archive the mount will be restored from is looked up by sanitized name.
	if _, err := util.RunCmd("tar", "czf",
		filepath.Join(volDir, sanitizeName(m.Name)+".tar.gz"), "-C", payload, "."); err != nil {
		t.Fatalf("tar payload: %v", err)
	}

	manifest := Manifest{Services: []ServiceInfo{{
		Name: "evil", Container: "evil", Image: "evil", Mounts: []Mount{m},
	}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	archive := filepath.Join(dir, "evil.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archive, "-C", dir, "backup_evil"); err != nil {
		t.Fatalf("tar archive: %v", err)
	}
	return archive
}

// A bind target the operator never named must not be written to. This is
// GHSA-v8mc-vpp8-jr4p: the manifest chose the path, so restore chose nothing.
func TestRestore_RefusesBindTargetOutsideAllowedRoot(t *testing.T) {
	dir := t.TempDir()

	victim := filepath.Join(dir, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("mkdir victim: %v", err)
	}
	canary := filepath.Join(victim, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	archive := writeMaliciousArchive(t, dir, Mount{
		Type: "bind", Name: "pwned", Source: victim, Destination: "/data",
	})

	result, err := Restore(archive, RestoreOptions{})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}

	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("canary was overwritten: %q", got)
	}
	if result.Volumes != 0 {
		t.Errorf("Volumes = %d, want 0", result.Volumes)
	}
	if len(result.Refused) != 1 || result.Refused[0].Target != victim {
		t.Fatalf("refusal not reported: %#v", result.Refused)
	}
}

// The same archive restores once the operator names the path themselves.
func TestRestore_AllowsBindTargetTheOperatorNamed(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "allowed")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	archive := writeMaliciousArchive(t, dir, Mount{
		Type: "bind", Name: "data", Source: target, Destination: "/data",
	})

	result, err := Restore(archive, RestoreOptions{AllowBind: []string{dir}})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	if result.Volumes != 1 || len(result.Refused) != 0 {
		t.Fatalf("expected the permitted bind to restore: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "canary")); err != nil {
		t.Errorf("permitted bind did not restore: %v", err)
	}
}

// A path cannot climb out of an allowed root with ".." components.
func TestRestore_RefusesBindTargetEscapingAllowedRootWithDotDot(t *testing.T) {
	dir := t.TempDir()

	outside := filepath.Join(dir, "outside")
	allowed := filepath.Join(dir, "allowed")
	for _, d := range []string{outside, allowed} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	archive := writeMaliciousArchive(t, dir, Mount{
		Type:   "bind",
		Name:   "escape",
		Source: filepath.Join(allowed, "..", "outside"),
	})

	result, err := Restore(archive, RestoreOptions{AllowBind: []string{allowed}})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	got, _ := os.ReadFile(canary)
	if string(got) != "ORIGINAL" {
		t.Errorf("traversal reached outside the allowed root: %q", got)
	}
	if len(result.Refused) != 1 {
		t.Errorf("traversal was not refused: %#v", result.Refused)
	}
}

// A volume mount whose name is a host path would reach `docker run -v` and be
// mounted by a daemon running as root. It must never get that far.
func TestRefuseMount_RejectsVolumeNamesThatArePaths(t *testing.T) {
	paths := []string{"/etc", "/", "../../etc", "/var/run/docker.sock", "-v", "_leading"}
	for _, name := range paths {
		if _, reason := refuseMount(Mount{Type: "volume", Name: name}, RestoreOptions{}); reason == "" {
			t.Errorf("volume name %q was allowed", name)
		}
	}
}

// Real volume names, as `docker inspect` reports them, must still restore.
func TestRefuseMount_AcceptsRealVolumeNames(t *testing.T) {
	names := []string{"nextcloud_data", "app-db", "vol.1", "a", "0abc"}
	for _, name := range names {
		if _, reason := refuseMount(Mount{Type: "volume", Name: name}, RestoreOptions{}); reason != "" {
			t.Errorf("volume name %q was refused: %s", name, reason)
		}
	}
}

// An unrecognised mount type is refused rather than falling through silently.
func TestRefuseMount_RejectsUnknownType(t *testing.T) {
	if _, reason := refuseMount(Mount{Type: "tmpfs", Name: "x"}, RestoreOptions{}); reason == "" {
		t.Error("unknown mount type was allowed")
	}
}

// A mount the archive carries no data for is not worth refusing: there is
// nothing to write either way, and reporting it would put a permission warning
// in front of an operator whose restore was fine.
func TestRestore_DoesNotRefuseMountsWithNoPayload(t *testing.T) {
	dir := t.TempDir()

	backupDir := filepath.Join(dir, "backup_empty")
	if err := os.MkdirAll(filepath.Join(backupDir, "volumes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A bind mount outside any allowed root — but with no volumes/*.tar.gz,
	// which is what backupMount leaves behind for a source that did not exist.
	manifest := Manifest{Services: []ServiceInfo{{
		Name: "svc", Container: "svc", Image: "img",
		Mounts: []Mount{{Type: "bind", Name: "gone", Source: filepath.Join(dir, "gone")}},
	}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	archive := filepath.Join(dir, "empty.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archive, "-C", dir, "backup_empty"); err != nil {
		t.Fatalf("tar: %v", err)
	}

	result, err := Restore(archive, RestoreOptions{})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	if len(result.Refused) != 0 {
		t.Errorf("a mount with no payload was reported as refused: %#v", result.Refused)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone")); !os.IsNotExist(err) {
		t.Error("restore created a directory for a mount it had no data for")
	}
}

// writeTwoMountArchive builds an archive whose manifest declares two bind
// mounts in order, each restored from its own payload directory.
func writeTwoMountArchive(t *testing.T, dir string, mounts []Mount, payloads []string) string {
	t.Helper()

	backupDir := filepath.Join(dir, "backup_evil")
	volDir := filepath.Join(backupDir, "volumes")
	if err := os.MkdirAll(volDir, 0o755); err != nil {
		t.Fatalf("mkdir volumes: %v", err)
	}
	for i, m := range mounts {
		if _, err := util.RunCmd("tar", "czf",
			filepath.Join(volDir, sanitizeName(m.Name)+".tar.gz"), "-C", payloads[i], "."); err != nil {
			t.Fatalf("tar payload %s: %v", m.Name, err)
		}
	}

	manifest := Manifest{Services: []ServiceInfo{{
		Name: "evil", Container: "evil", Image: "evil", Mounts: mounts,
	}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	archive := filepath.Join(dir, "evil.tar.gz")
	if _, err := util.RunCmd("tar", "czf", archive, "-C", dir, "backup_evil"); err != nil {
		t.Fatalf("tar archive: %v", err)
	}
	return archive
}

// One archive can defeat a lexical containment check on its own, without any
// symlink existing on the host beforehand. The first mount restores normally
// inside the permitted root and its payload plants a symlink there; the second
// names that symlink as its source, which is lexically inside the root and so
// passes, and the extraction follows it out. This is #91.
func TestRestore_RefusesBindTargetThatAnEarlierMountTurnedIntoASymlink(t *testing.T) {
	dir := t.TempDir()

	allowed := filepath.Join(dir, "allowed")
	if err := os.MkdirAll(filepath.Join(allowed, "shared"), 0o755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	// The first payload is the symlink; the second is what escapes through it.
	plant := filepath.Join(dir, "payload_plant")
	if err := os.MkdirAll(plant, 0o755); err != nil {
		t.Fatalf("mkdir plant: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(plant, "hop")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	escape := filepath.Join(dir, "payload_escape")
	if err := os.MkdirAll(escape, 0o755); err != nil {
		t.Fatalf("mkdir escape: %v", err)
	}
	if err := os.WriteFile(filepath.Join(escape, "canary"), []byte("OVERWRITTEN"), 0o644); err != nil {
		t.Fatalf("write escape payload: %v", err)
	}

	hop := filepath.Join(allowed, "shared", "hop")
	archive := writeTwoMountArchive(t, dir,
		[]Mount{
			{Type: "bind", Name: "shared", Source: filepath.Join(allowed, "shared"), Destination: "/shared"},
			{Type: "bind", Name: "hop", Source: hop, Destination: "/hop"},
		},
		[]string{plant, escape},
	)

	result, err := Restore(archive, RestoreOptions{AllowBind: []string{allowed}})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}

	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("canary outside the permitted root was overwritten: %q", got)
	}
	if len(result.Refused) != 1 || result.Refused[0].Target != hop {
		t.Fatalf("the escaping mount was not refused: %#v", result.Refused)
	}
}

// A bind target that is a symlink pointing outside the permitted root is
// refused even when nothing in the archive created it.
func TestRestore_RefusesBindTargetThatIsAnExistingSymlinkOutOfTheRoot(t *testing.T) {
	dir := t.TempDir()

	allowed := filepath.Join(dir, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	archive := writeMaliciousArchive(t, dir, Mount{
		Type: "bind", Name: "pwned", Source: link, Destination: "/data",
	})

	result, err := Restore(archive, RestoreOptions{AllowBind: []string{allowed}})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("canary was overwritten through the symlink: %q", got)
	}
	if len(result.Refused) != 1 {
		t.Fatalf("the symlinked target was not refused: %#v", result.Refused)
	}
}

// A permitted root that is itself a symlink is what the operator meant, so a
// target inside the place it points to still restores.
func TestRestore_AllowsBindTargetWhenThePermittedRootIsASymlink(t *testing.T) {
	dir := t.TempDir()

	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "data"), 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	target := filepath.Join(real, "data")
	archive := writeMaliciousArchive(t, dir, Mount{
		Type: "bind", Name: "data", Source: target, Destination: "/data",
	})

	result, err := Restore(archive, RestoreOptions{AllowBind: []string{link}})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	if result.Volumes != 1 || len(result.Refused) != 0 {
		t.Fatalf("expected the permitted bind to restore: %#v", result)
	}
}
