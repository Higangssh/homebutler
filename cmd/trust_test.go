package cmd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshHostAt answers SSH at a local address with a host key of its own and
// lets nobody in. Reading its host key is all trust does with it.
func sshHostAt(t *testing.T) (addr string, key ssh.PublicKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _, _, _ = ssh.NewServerConn(conn, cfg)
				conn.Close()
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
	})
	return listener.Addr().String(), signer.PublicKey()
}

// closedAddr is a local address nothing is listening on.
func closedAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

func otherHostKey(t *testing.T) ssh.PublicKey {
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

// trustFixture is a HOME and a config of the test's own: one server, nas, at
// addr, and a known_hosts that already holds a key for it — the key the host
// had before it changed — beside a comment and another host's entry.
type trustFixture struct {
	knownHosts string
	before     []byte
	staleKey   ssh.PublicKey
}

// plainEntry is how homebutler itself writes a host down.
func plainEntry(addr string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{addr}, key)
}

// hashedEntry is how OpenSSH writes one with HashKnownHosts yes, the default
// on Debian and Ubuntu.
func hashedEntry(addr string, key ssh.PublicKey) string {
	line := knownhosts.Line([]string{"placeholder"}, key)
	return knownhosts.HashHostname(knownhosts.Normalize(addr)) + strings.TrimPrefix(line, "placeholder")
}

func newTrustFixture(t *testing.T, addr string) *trustFixture {
	t.Helper()
	return newTrustFixtureWith(t, addr, plainEntry)
}

func newTrustFixtureWith(t *testing.T, addr string, write func(string, ssh.PublicKey) string) *trustFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}

	stale := otherHostKey(t)
	known := "# kept by hand\n" +
		knownhosts.Line([]string{"192.0.2.50:22"}, otherHostKey(t)) + "\n" +
		write(addr, stale) + "\n"
	knownHosts := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(known), 0o600); err != nil {
		t.Fatal(err)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("servers:\n  - name: nas\n    host: %s\n    port: %s\n    user: admin\n    auth: password\n    password: correct-horse-battery-staple\n", host, port)
	configPath := filepath.Join(t.TempDir(), "homebutler.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	oldPath, oldCfg := cfgPath, cfg
	t.Cleanup(func() { cfgPath, cfg = oldPath, oldCfg })
	cfgPath, cfg = configPath, nil

	return &trustFixture{knownHosts: knownHosts, before: []byte(known), staleKey: stale}
}

func (f *trustFixture) after(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// runTrustReset runs `trust nas --reset` with answer typed at the prompt.
func runTrustReset(t *testing.T, answer string) error {
	t.Helper()
	stdin := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(stdin, []byte(answer), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(stdin)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	saved := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = saved }()

	cmd := newTrustCmd()
	cmd.SetArgs([]string{"nas", "--reset"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// --reset is run when a host key has changed. It deleted the old entry before
// showing the new fingerprint, so answering no — that fingerprint is not the
// one I expected — left the server with no entry at all, and the next
// key-authenticated connection trusted whatever answered on first use.
func TestTrustResetAnsweredNoLeavesKnownHostsAsItWas(t *testing.T) {
	addr, _ := sshHostAt(t)
	f := newTrustFixture(t, addr)

	err := runTrustReset(t, "n\n")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("answering no did not cancel: %v", err)
	}
	if got := f.after(t); !bytes.Equal(got, f.before) {
		t.Fatalf("answering no changed known_hosts\nbefore:\n%s\nafter:\n%s", f.before, got)
	}
}

// No answer at all, as when stdin closes or the prompt is abandoned, is a no.
func TestTrustResetWithNoAnswerLeavesKnownHostsAsItWas(t *testing.T) {
	addr, _ := sshHostAt(t)
	f := newTrustFixture(t, addr)

	if err := runTrustReset(t, ""); err == nil {
		t.Fatal("trust went ahead without an answer")
	}
	if got := f.after(t); !bytes.Equal(got, f.before) {
		t.Fatalf("an unanswered prompt changed known_hosts\nbefore:\n%s\nafter:\n%s", f.before, got)
	}
}

// accepts says whether the fixture's known_hosts lets key through for addr,
// asked the way a connection asks it.
func (f *trustFixture) accepts(t *testing.T, addr string, key ssh.PublicKey) bool {
	t.Helper()
	cb, err := knownhosts.New(f.knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return cb(addr, remote, key) == nil
}

func TestTrustResetAnsweredYesReplacesTheEntry(t *testing.T) {
	addr, hostKey := sshHostAt(t)
	f := newTrustFixture(t, addr)

	if err := runTrustReset(t, "y\n"); err != nil {
		t.Fatalf("trust --reset: %v", err)
	}
	got := string(f.after(t))

	// The entry for this host is the new key, and only the new key.
	if !f.accepts(t, addr, hostKey) {
		t.Fatalf("the host's new key is not trusted afterwards:\n%s", got)
	}
	if f.accepts(t, addr, f.staleKey) {
		t.Fatalf("the old key is still trusted after --reset:\n%s", got)
	}
	if n := strings.Count(got, knownhosts.Normalize(addr)+" "); n != 1 {
		t.Fatalf("known_hosts has %d entries for %s, want 1:\n%s", n, addr, got)
	}

	// Everything else is left where it was.
	for _, line := range strings.Split(strings.TrimSpace(string(f.before)), "\n") {
		if strings.HasPrefix(line, knownhosts.Normalize(addr)+" ") {
			continue
		}
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("--reset dropped a line it had no business with: %q\n%s", line, got)
		}
	}
}

// OpenSSH on Debian and Ubuntu hashes the host names it records. The old key
// left under a hashed name stayed trusted beside the new one, and the command
// still said "replaced".
func TestTrustResetReplacesAHashedEntry(t *testing.T) {
	addr, hostKey := sshHostAt(t)
	f := newTrustFixtureWith(t, addr, hashedEntry)

	if err := runTrustReset(t, "y\n"); err != nil {
		t.Fatalf("trust --reset: %v", err)
	}
	got := f.after(t)
	if f.accepts(t, addr, f.staleKey) {
		t.Fatalf("the old key under a hashed name is still trusted after --reset:\n%s", got)
	}
	if !f.accepts(t, addr, hostKey) {
		t.Fatalf("the host's new key is not trusted afterwards:\n%s", got)
	}
}

// A wildcard line is someone's own rule for many hosts and is not rewritten,
// but when it still accepts the old key the command says so and fails, rather
// than reporting the key replaced.
func TestTrustResetSaysWhenAPatternStillTrustsTheOldKey(t *testing.T) {
	addr, hostKey := sshHostAt(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	pattern := ""
	f := newTrustFixtureWith(t, addr, func(_ string, key ssh.PublicKey) string {
		line := knownhosts.Line([]string{"placeholder"}, key)
		pattern = "[127.0.0.*]:" + port + strings.TrimPrefix(line, "placeholder")
		return pattern
	})

	err = runTrustReset(t, "y\n")
	if err == nil {
		t.Fatal("trust --reset reported success while a pattern still trusts the old key")
	}
	if !strings.Contains(err.Error(), pattern) || strings.Contains(err.Error(), "left as it was") {
		t.Fatalf("the error does not name the pattern line, or says nothing was written: %v", err)
	}
	if !f.accepts(t, addr, hostKey) {
		t.Fatalf("the new key was not recorded:\n%s", f.after(t))
	}
	if !strings.Contains(string(f.after(t)), pattern+"\n") {
		t.Fatalf("the pattern line was rewritten:\n%s", f.after(t))
	}
}

// A host that cannot be reached shows no fingerprint and cannot have been
// agreed to, so the old entry stays.
func TestTrustResetThatCannotConnectLeavesKnownHostsAsItWas(t *testing.T) {
	addr := closedAddr(t)
	f := newTrustFixture(t, addr)

	if err := runTrustReset(t, "y\n"); err == nil {
		t.Fatal("trust --reset against a closed port succeeded")
	}
	if got := f.after(t); !bytes.Equal(got, f.before) {
		t.Fatalf("a failed connection changed known_hosts\nbefore:\n%s\nafter:\n%s", f.before, got)
	}
}
