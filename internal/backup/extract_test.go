package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarMember is one entry to place in a test archive, built with archive/tar so
// the dangerous names can be written at all — the tar binary declines to create
// most of them, which is the point of these tests.
type tarMember struct {
	name     string
	typeflag byte
	linkname string
	body     string
	mode     os.FileMode
}

func writeTarGz(t *testing.T, path string, members []tarMember) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, m := range members {
		mode := m.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     m.name,
			Typeflag: m.typeflag,
			Linkname: m.linkname,
			Mode:     int64(mode),
			Size:     int64(len(m.body)),
		}
		if m.typeflag != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", m.name, err)
		}
		if m.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatalf("write body %s: %v", m.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

func TestExtractTarGz_RestoresAnOrdinaryArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "conf", typeflag: tar.TypeDir, mode: 0o755},
		{name: "conf/app.yml", typeflag: tar.TypeReg, body: "key: value", mode: 0o600},
		{name: "conf/link", typeflag: tar.TypeSymlink, linkname: "app.yml"},
	})

	dest := filepath.Join(dir, "dest")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "conf", "app.yml"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != "key: value" {
		t.Errorf("content = %q, want %q", got, "key: value")
	}
	info, err := os.Stat(filepath.Join(dest, "conf", "app.yml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	link, err := os.Readlink(filepath.Join(dest, "conf", "link"))
	if err != nil || link != "app.yml" {
		t.Errorf("symlink = %q, %v; want \"app.yml\"", link, err)
	}
}

// tar strips a leading "/" and carries on. homebutler refuses, because an
// archive that asked to write to an absolute path is not one whose remaining
// members should be trusted into the same directory.
func TestExtractTarGz_RefusesAbsoluteMemberName(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "/etc/homebutler-canary", typeflag: tar.TypeReg, body: "OWNED"},
	})

	err := extractTarGz(archive, filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatal("extractTarGz() accepted an absolute member name")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Errorf("error = %v, want it to name the absolute path", err)
	}
	if _, err := os.Stat("/etc/homebutler-canary"); err == nil {
		t.Fatal("an absolute member was written outside the destination")
	}
}

// The ".." climb, which is the case tar skips with a warning.
func TestExtractTarGz_RefusesMemberClimbingOutOfTheRoot(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("mkdir victim: %v", err)
	}
	canary := filepath.Join(victim, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "../victim/canary", typeflag: tar.TypeReg, body: "OVERWRITTEN"},
	})

	if err := extractTarGz(archive, filepath.Join(dir, "dest")); err == nil {
		t.Fatal("extractTarGz() accepted a member climbing out of the root")
	}
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("canary was overwritten: %q", got)
	}
}

// The same shape as #91, one level down: an earlier member of the archive is a
// symlink out of the tree, and a later member is named through it. Every
// component is relative and none contains "..", so a name check alone accepts
// it.
func TestExtractTarGz_RefusesMemberWrittenThroughASymlinkItPlanted(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "hop", typeflag: tar.TypeSymlink, linkname: outside},
		{name: "hop/canary", typeflag: tar.TypeReg, body: "OVERWRITTEN"},
	})

	if err := extractTarGz(archive, filepath.Join(dir, "dest")); err == nil {
		t.Fatal("extractTarGz() wrote through a symlink the archive planted")
	}
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("canary was overwritten through the planted symlink: %q", got)
	}
}

// A hard link never names a path outside the tree, so a name check does not see
// it. What it does is give the extracted tree a second name for a file that
// already exists somewhere else.
func TestExtractTarGz_RefusesHardLinkOutOfTheTree(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "exposed", typeflag: tar.TypeLink, linkname: "../secret"},
	})

	dest := filepath.Join(dir, "dest")
	if err := extractTarGz(archive, dest); err == nil {
		t.Fatal("extractTarGz() accepted a hard link out of the tree")
	}
	if _, err := os.Lstat(filepath.Join(dest, "exposed")); err == nil {
		t.Error("the hard link was created")
	}
}

// Dropping a member quietly would mean a restore reporting success while having
// left part of the archive on the floor.
func TestExtractTarGz_RefusesDeviceNodesRatherThanSkippingThem(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "dev/null", typeflag: tar.TypeChar},
	})

	err := extractTarGz(archive, filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatal("extractTarGz() accepted a character device")
	}
	if !strings.Contains(err.Error(), "dev/null") {
		t.Errorf("error = %v, want it to name the member", err)
	}
}

// A directory stored read-only must not stop its own contents being written,
// and must still end up read-only.
func TestExtractTarGz_AppliesDirectoryModesAfterWriting(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "locked", typeflag: tar.TypeDir, mode: 0o500},
		{name: "locked/file", typeflag: tar.TypeReg, body: "in a read-only directory"},
	})

	dest := filepath.Join(dir, "dest")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dest, "locked"), 0o755) })

	if _, err := os.ReadFile(filepath.Join(dest, "locked", "file")); err != nil {
		t.Fatalf("file inside the read-only directory was not written: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "locked"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Errorf("directory mode = %v, want 0500", info.Mode().Perm())
	}
}

// A restore overwrites, and an existing symlink at the destination must be
// replaced rather than written through.
func TestExtractTarGz_ReplacesAnExistingSymlinkRatherThanFollowingIt(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "file")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "file", typeflag: tar.TypeReg, body: "RESTORED"},
	})

	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("wrote through the existing symlink: %q", got)
	}
	restored, err := os.ReadFile(filepath.Join(dest, "file"))
	if err != nil || string(restored) != "RESTORED" {
		t.Errorf("member = %q, %v; want %q", restored, err, "RESTORED")
	}
}

// #92: one derivation, so mountHasPayload and restoreMount cannot disagree
// about which archive a mount refers to.
func TestMountArchivePath_IsSanitizedAndShared(t *testing.T) {
	volDir := "/backup/volumes"
	for _, name := range []string{"../../etc/passwd", "data", "/abs/path"} {
		got := mountArchivePath(name, volDir)
		if filepath.Dir(got) != volDir {
			t.Errorf("mountArchivePath(%q) = %q, escaped %q", name, got, volDir)
		}
		if !strings.HasSuffix(got, ".tar.gz") {
			t.Errorf("mountArchivePath(%q) = %q, want a .tar.gz path", name, got)
		}
	}
}

// Archiving on macOS splits a file that carries extended attributes into the
// file plus a sibling "._<file>" holding the attributes. bsdtar absorbs the
// sibling on the way back out, so homebutler never saw one while it was
// shelling out to tar. Written literally they are files that were never in the
// source, and they end up inside restored volumes.
func TestExtractTarGz_DropsAppleDoubleSidecars(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "app.yml", typeflag: tar.TypeReg, body: "key: value"},
		{name: "._app.yml", typeflag: tar.TypeReg, body: "\x00\x05\x16\x07AppleDouble metadata"},
		{name: "sub", typeflag: tar.TypeDir, mode: 0o755},
		{name: "sub/._data", typeflag: tar.TypeReg, body: "\x00\x05\x16\x07more metadata"},
		{name: "sub/data", typeflag: tar.TypeReg, body: "payload"},
	})

	dest := filepath.Join(dir, "dest")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	for _, gone := range []string{"._app.yml", filepath.Join("sub", "._data")} {
		if _, err := os.Lstat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s was written; it was never in the source tree", gone)
		}
	}
	for name, want := range map[string]string{
		"app.yml":                    "key: value",
		filepath.Join("sub", "data"): "payload",
	} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}

// The name is not the test — a real file can be called "._notes". Only a member
// that actually carries the AppleDouble magic is dropped.
func TestExtractTarGz_KeepsAFileMerelyNamedLikeASidecar(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, []tarMember{
		{name: "._notes", typeflag: tar.TypeReg, body: "these are my actual notes"},
		{name: "._tiny", typeflag: tar.TypeReg, body: "ab"},
		{name: "._empty", typeflag: tar.TypeReg, body: ""},
	})

	dest := filepath.Join(dir, "dest")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
	for name, want := range map[string]string{
		"._notes": "these are my actual notes",
		"._tiny":  "ab",
		"._empty": "",
	} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q — a real file was dropped", name, got, err, want)
		}
	}
}
