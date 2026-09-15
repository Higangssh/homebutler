package remote

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/Higangssh/homebutler/internal/config"
)

// recordingSSHServer answers SSH and writes down every password offered to it.
// It stands in for whatever machine is actually listening at an address the
// config names — the point of these tests is what homebutler hands over before
// it knows which machine that is.
type recordingSSHServer struct {
	addr string

	mu        sync.Mutex
	passwords []string
}

func (s *recordingSSHServer) offered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.passwords...)
}

func startRecordingSSHServer(t *testing.T) *recordingSSHServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	server := &recordingSSHServer{}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			server.mu.Lock()
			server.passwords = append(server.passwords, string(password))
			server.mu.Unlock()
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.addr = listener.Addr().String()

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

	return server
}

// isolatedHome points ~/.ssh at a directory of this test's own, so a test that
// trusts a host cannot write into the known_hosts of whoever is running it.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

func serverAt(t *testing.T, addr string) *config.ServerConfig {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return &config.ServerConfig{
		Name:     "nas",
		Host:     host,
		Port:     number,
		User:     "admin",
		AuthMode: "password",
		Password: "correct-horse-battery-staple",
	}
}

// The one that matters: a password is never offered to a host homebutler has
// not been told to trust.
//
// It used to be offered twice. The connection that reads a new host key copied
// the whole client config, credentials included, so the password went out
// before the key was so much as written down; and trust-on-first-use then
// recorded the key and sent the password again. Whatever answered at that
// address received it — a machine reached by a typo, a stale DNS record, or an
// address changed by anyone who could edit the config. Nothing afterwards said
// so: a wrong address fails exactly like a wrong password.
func TestAPasswordIsNeverSentToAnUntrustedHost(t *testing.T) {
	home := isolatedHome(t)
	server := startRecordingSSHServer(t)

	_, err := connect(serverAt(t, server.addr))
	if err == nil {
		t.Fatal("connecting to an untrusted host with a password succeeded")
	}
	if offered := server.offered(); len(offered) != 0 {
		t.Fatalf("the password was sent to a host that was never trusted: %v", offered)
	}

	// And the host was not quietly trusted on the way past, either.
	known, readErr := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.TrimSpace(string(known)) != "" {
		t.Fatalf("the host was added to known_hosts anyway:\n%s", known)
	}
}

// The refusal has to say what to do about it, because the alternative reading
// — that the password is wrong — sends someone looking in the wrong place.
func TestTheRefusalSaysHowToTrustTheHost(t *testing.T) {
	isolatedHome(t)
	server := startRecordingSSHServer(t)

	_, err := connect(serverAt(t, server.addr))
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"known_hosts", "homebutler trust nas"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not mention %q: %v", want, err)
		}
	}
	if class := Classify(err); class != ClassHostKey {
		t.Fatalf("the refusal is classed %q, want %q", class, ClassHostKey)
	}
}

// Once the host is in known_hosts the password goes to it, which is the whole
// point of having trusted it.
func TestAPasswordIsSentToAHostThatWasTrusted(t *testing.T) {
	home := isolatedHome(t)
	server := startRecordingSSHServer(t)
	target := serverAt(t, server.addr)

	if err := TrustServer(target, func(string) bool { return true }); err != nil {
		t.Fatalf("trusting the host: %v", err)
	}
	known, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(known)) == "" {
		t.Fatal("trusting the host wrote nothing to known_hosts")
	}

	// The server refuses every password, so this fails at authentication —
	// after the password has been offered, which is what is being checked.
	if _, err := connect(target); err == nil {
		t.Fatal("the recording server accepted a password it always denies")
	}
	if offered := server.offered(); len(offered) != 1 || offered[0] != target.Password {
		t.Fatalf("the password did not reach the trusted host: %v", offered)
	}
}
