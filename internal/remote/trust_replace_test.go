package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const piAddr = "192.168.0.4:22"

func newHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// entry writes a known_hosts line for key under the given host field.
func entry(hosts string, key ssh.PublicKey) string {
	line := knownhosts.Line([]string{"placeholder"}, key)
	return hosts + strings.TrimPrefix(line, "placeholder")
}

// replaceIn writes content to a known_hosts of its own, replaces piAddr's
// entries with newKey, and returns the error and the file's path.
func replaceIn(t *testing.T, content string, newKey ssh.PublicKey) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{piAddr}, newKey)
	return path, replaceHostKeys(path, "pi", piAddr, newKey, line)
}

// accepts says whether the known_hosts at path lets key through for piAddr,
// asked the way a connection asks it.
func accepts(t *testing.T, path string, key ssh.PublicKey) bool {
	t.Helper()
	cb, err := knownhosts.New(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.168.0.4"), Port: 22}
	return cb(piAddr, remote, key) == nil
}

// Every way OpenSSH or a person writes down this one host has to go. Hashed is
// the one that matters most: Debian and Ubuntu default to HashKnownHosts yes,
// and an old key left there stayed trusted beside the new one while the
// command said "replaced".
func TestReplaceDropsEveryEntryForTheHost(t *testing.T) {
	cases := map[string]func(ssh.PublicKey) string{
		"plain":         func(k ssh.PublicKey) string { return entry("192.168.0.4", k) },
		"bracketed :22": func(k ssh.PublicKey) string { return entry("[192.168.0.4]:22", k) },
		"in a list":     func(k ssh.PublicKey) string { return entry("pi.lan,192.168.0.4", k) },
		"hashed":        func(k ssh.PublicKey) string { return entry(knownhosts.HashHostname("192.168.0.4"), k) },
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			old, other, fresh := newHostKey(t), newHostKey(t), newHostKey(t)
			unrelated := entry(knownhosts.HashHostname("192.168.0.5"), other)
			path, err := replaceIn(t, "# mine\n"+unrelated+"\n"+write(old)+"\n", fresh)
			if err != nil {
				t.Fatalf("replace: %v", err)
			}
			if accepts(t, path, old) {
				data, _ := os.ReadFile(path)
				t.Fatalf("the old key is still accepted after the replace:\n%s", data)
			}
			if !accepts(t, path, fresh) {
				t.Fatal("the new key is not accepted after the replace")
			}
			data, _ := os.ReadFile(path)
			for _, keep := range []string{"# mine", unrelated} {
				if !strings.Contains(string(data), keep+"\n") {
					t.Fatalf("the replace dropped %q:\n%s", keep, data)
				}
			}
		})
	}
}

// A pattern names more than this host, so it is not rewritten on anyone's
// behalf — but the command must not then say the key was replaced.
func TestReplaceNamesAPatternThatStillTrustsTheOldKey(t *testing.T) {
	old, fresh := newHostKey(t), newHostKey(t)
	pattern := entry("192.168.0.*", old)
	// Another host with the same key is not why this one still trusts it,
	// and naming it would send someone to edit the wrong line.
	sameKeyElsewhere := entry("192.0.2.50", old)
	path, err := replaceIn(t, sameKeyElsewhere+"\n"+pattern+"\n", fresh)

	var stale *StillTrustedError
	if !errors.As(err, &stale) {
		t.Fatalf("a pattern still trusting the old key was not reported: %v", err)
	}
	if len(stale.Lines) != 1 || stale.Lines[0] != pattern {
		t.Fatalf("reported lines %q, want the pattern %q", stale.Lines, pattern)
	}
	if !strings.Contains(err.Error(), "192.168.0.*") || !strings.Contains(err.Error(), "pi") {
		t.Fatalf("the error does not name the line and the server: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), pattern+"\n") {
		t.Fatalf("the pattern was rewritten:\n%s", data)
	}
	if !accepts(t, path, fresh) {
		t.Fatal("the new key was not recorded")
	}
}

// A line that excludes this host does not apply to it, so it is neither
// removed nor reported; an @revoked line is never removed.
func TestReplaceLeavesLinesThatDoNotTrustTheHost(t *testing.T) {
	old, revoked, fresh := newHostKey(t), newHostKey(t), newHostKey(t)
	negated := entry("192.168.0.*,!192.168.0.4", old)
	revokedLine := "@revoked " + entry("*", revoked)
	path, err := replaceIn(t, negated+"\n"+revokedLine+"\n", fresh)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	data, _ := os.ReadFile(path)
	for _, keep := range []string{negated, revokedLine} {
		if !strings.Contains(string(data), keep+"\n") {
			t.Fatalf("the replace dropped %q:\n%s", keep, data)
		}
	}
}

// A known_hosts that is a symlink whose file does not exist yet stays a
// symlink: the file is written where it points.
func TestReplaceWritesThroughADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	dotfiles := filepath.Join(dir, "dotfiles")
	if err := os.Mkdir(dotfiles, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "known_hosts")
	if err := os.Symlink(filepath.Join("dotfiles", "known_hosts"), link); err != nil {
		t.Fatal(err)
	}

	fresh := newHostKey(t)
	if err := replaceHostKeys(link, "pi", piAddr, fresh, knownhosts.Line([]string{piAddr}, fresh)); err != nil {
		t.Fatalf("replace: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if !accepts(t, filepath.Join(dotfiles, "known_hosts"), fresh) {
		t.Fatal("the new key was not written where the symlink points")
	}
}

// When the directory it points into is not there, the link is left alone and
// the replace fails, rather than a file appearing in the link's place.
func TestReplaceRefusesASymlinkIntoAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "known_hosts")
	target := filepath.Join(dir, "gone", "known_hosts")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	fresh := newHostKey(t)
	if err := replaceHostKeys(link, "pi", piAddr, fresh, knownhosts.Line([]string{piAddr}, fresh)); err == nil {
		t.Fatal("replacing through a symlink into a missing directory succeeded")
	}
	got, err := os.Readlink(link)
	if err != nil || got != target {
		t.Fatalf("the symlink was changed: %q, %v", got, err)
	}
}

// A chain of links longer than knownHostsTarget follows is refused, rather
// than the link it stopped on being renamed over and turned into a file.
func TestReplaceRefusesASymlinkChainTooLongToFollow(t *testing.T) {
	dir := t.TempDir()
	const links = 41
	for i := 0; i < links; i++ {
		// link-i points at link-(i+1); the last one points at nothing.
		next := filepath.Join(dir, "link-"+strconv.Itoa(i+1))
		if err := os.Symlink(next, filepath.Join(dir, "link-"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	head := filepath.Join(dir, "link-0")

	fresh := newHostKey(t)
	if err := replaceHostKeys(head, "pi", piAddr, fresh, knownhosts.Line([]string{piAddr}, fresh)); err == nil {
		t.Fatal("replacing through a symlink chain too long to follow succeeded")
	}
	for i := 0; i < links; i++ {
		info, err := os.Lstat(filepath.Join(dir, "link-"+strconv.Itoa(i)))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("link-%d is no longer a symlink: %v", i, err)
		}
	}
}

// A line nothing can parse stops the replace, and the error points at the
// file and line someone can open: not the temporary copy, which is already
// gone, and not a line number counted after the host's entries were dropped.
func TestReplaceNamesTheRealFileAndLineItCannotParse(t *testing.T) {
	old, fresh := newHostKey(t), newHostKey(t)
	content := entry("192.168.0.4", old) + "\n" +
		"# a comment\n" +
		"192.0.2.9 ssh-ed25519 this-is-not-a-key\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := replaceHostKeys(path, "pi", piAddr, fresh, knownhosts.Line([]string{piAddr}, fresh))
	if err == nil {
		t.Fatal("a known_hosts with a line nothing can parse was replaced")
	}
	if !strings.Contains(err.Error(), path+":3:") {
		t.Fatalf("the error does not name %s line 3: %v", path, err)
	}
	if strings.Contains(err.Error(), ".known_hosts.homebutler-") {
		t.Fatalf("the error names the temporary copy: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Fatalf("known_hosts changed:\n%s", data)
	}
	names, _ := os.ReadDir(dir)
	if len(names) != 1 {
		t.Fatalf("files were left beside known_hosts: %v", names)
	}
}
