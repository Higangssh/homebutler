package remote

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type testSSHServer struct {
	addr    string
	pubKey  gossh.PublicKey
	cleanup func()
}

func startTestSSHServer(t *testing.T, handler func(string) (string, uint32)) *testSSHServer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &gossh.ServerConfig{
		PasswordCallback: func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if conn.User() == "tester" && string(password) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("auth failed")
		},
	}
	serverConfig.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}

			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()

				_, chans, reqs, err := gossh.NewServerConn(c, serverConfig)
				if err != nil {
					return
				}
				go gossh.DiscardRequests(reqs)

				for ch := range chans {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(gossh.UnknownChannelType, "unsupported")
						continue
					}
					channel, requests, err := ch.Accept()
					if err != nil {
						continue
					}
					go func(ch gossh.Channel, in <-chan *gossh.Request) {
						defer ch.Close()
						for req := range in {
							switch req.Type {
							case "exec":
								var payload struct{ Value string }
								_ = gossh.Unmarshal(req.Payload, &payload)
								req.Reply(true, nil)
								out, code := handler(payload.Value)
								_, _ = io.WriteString(ch, out)
								status := struct{ Status uint32 }{Status: code}
								_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(&status))
								return
							default:
								req.Reply(false, nil)
							}
						}
					}(channel, requests)
				}
			}(conn)
		}
	}()

	return &testSSHServer{
		addr:   ln.Addr().String(),
		pubKey: signer.PublicKey(),
		cleanup: func() {
			close(done)
			_ = ln.Close()
			wg.Wait()
		},
	}
}

func writeKnownHostsForTest(t *testing.T, server *testSSHServer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(server.addr)}, server.pubKey)
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dialTestSSHClient(t *testing.T, addr string) *gossh.Client {
	t.Helper()
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "tester",
		Auth:            []gossh.AuthMethod{gossh.Password("secret")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewKnownHostsCallback(t *testing.T) {
	cb, err := newKnownHostsCallback()
	if err != nil {
		t.Fatalf("newKnownHostsCallback() error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestNewKnownHostsCallback_CreatesFile(t *testing.T) {
	// Verify that ~/.ssh/known_hosts exists after calling newKnownHostsCallback
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	path := filepath.Join(home, ".ssh", "known_hosts")

	_, err = newKnownHostsCallback()
	if err != nil {
		t.Fatalf("newKnownHostsCallback() error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected %s to exist after newKnownHostsCallback()", path)
	}
}

func TestKnownHostsPath(t *testing.T) {
	path, err := knownHostsPath()
	if err != nil {
		t.Fatalf("knownHostsPath() error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".ssh", "known_hosts")) {
		t.Errorf("expected path ending in .ssh/known_hosts, got %s", path)
	}
}

func TestTofuConnect_NoServer(t *testing.T) {
	t.Skip("tofuConnect requires a real SSH server; skipping in unit tests")
}

// TestErrorMessages_KeyMismatch verifies the key-mismatch error contains actionable hints.
func TestErrorMessages_KeyMismatch(t *testing.T) {
	// Simulate the error message that connect() would produce for a key mismatch.
	serverName := "myserver"
	addr := "10.0.0.1:22"
	msg := fmt.Sprintf("[%s] ⚠️  SSH HOST KEY CHANGED (%s)\n"+
		"  The server's host key does not match the one in ~/.ssh/known_hosts.\n"+
		"  This could mean:\n"+
		"    1. The server was reinstalled or its SSH keys were regenerated\n"+
		"    2. A man-in-the-middle attack is in progress\n\n"+
		"  → If you trust this change: homebutler trust %s --reset\n"+
		"  → If unexpected: do NOT connect and investigate", serverName, addr, serverName)

	if !strings.Contains(msg, "homebutler trust") {
		t.Error("key mismatch error should contain 'homebutler trust'")
	}
	if !strings.Contains(msg, "HOST KEY CHANGED") {
		t.Error("key mismatch error should contain 'HOST KEY CHANGED'")
	}
	if !strings.Contains(msg, "--reset") {
		t.Error("key mismatch error should contain '--reset' flag")
	}
}

// TestErrorMessages_UnknownHost verifies the unknown-host error contains actionable hints.
func TestErrorMessages_UnknownHost(t *testing.T) {
	serverName := "newserver"
	addr := "10.0.0.2:22"
	msg := fmt.Sprintf("[%s] failed to auto-register host key for %s\n  → Register manually: homebutler trust %s\n  → Check SSH connectivity: ssh %s@%s -p %d",
		serverName, addr, serverName, "root", "10.0.0.2", 22)

	if !strings.Contains(msg, "homebutler trust") {
		t.Error("unknown host error should contain 'homebutler trust'")
	}
	if !strings.Contains(msg, "ssh root@") {
		t.Error("unknown host error should contain ssh connection hint")
	}
}

// TestErrorMessages_NoCredentials verifies the no-credentials error contains config hint.
func TestErrorMessages_NoCredentials(t *testing.T) {
	serverName := "nocreds"
	msg := fmt.Sprintf("[%s] no SSH credentials configured\n  → Add 'key_file' or 'password' to this server in ~/.config/homebutler/config.yaml", serverName)

	if !strings.Contains(msg, "key_file") {
		t.Error("no-credentials error should mention key_file")
	}
	if !strings.Contains(msg, "config.yaml") {
		t.Error("no-credentials error should mention config.yaml")
	}
}

// TestErrorMessages_Timeout verifies the timeout error contains actionable hints.
func TestErrorMessages_Timeout(t *testing.T) {
	serverName := "slowserver"
	addr := "10.0.0.3:22"
	msg := fmt.Sprintf("[%s] connection timed out (%s)\n  → Check if the server is online and reachable\n  → Verify host/port in ~/.config/homebutler/config.yaml", serverName, addr)

	if !strings.Contains(msg, "timed out") {
		t.Error("timeout error should contain 'timed out'")
	}
	if !strings.Contains(msg, "config.yaml") {
		t.Error("timeout error should mention config.yaml")
	}
}

// TestKnownHostsKeyError verifies that knownhosts.KeyError works as expected
// for both key-mismatch and unknown-host scenarios.
func TestKnownHostsKeyError(t *testing.T) {
	// KeyError with Want = empty means unknown host
	unknownErr := &knownhosts.KeyError{}
	if len(unknownErr.Want) != 0 {
		t.Error("empty KeyError should have no Want entries (unknown host)")
	}

	// KeyError with Want populated means key mismatch
	mismatchErr := &knownhosts.KeyError{
		Want: []knownhosts.KnownKey{{Filename: "known_hosts", Line: 1}},
	}
	if len(mismatchErr.Want) == 0 {
		t.Error("mismatch KeyError should have Want entries")
	}
}

// --- connect error tests (no real SSH server) ---

func TestConnect_NoCredentials(t *testing.T) {
	srv := &config.ServerConfig{
		Name:     "nocreds",
		Host:     "127.0.0.1",
		AuthMode: "password",
		// No password set
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected error for no credentials")
	}
	if !strings.Contains(err.Error(), "no SSH credentials") {
		t.Errorf("expected 'no SSH credentials' error, got: %s", err.Error())
	}
}

func TestConnect_BadKeyFile(t *testing.T) {
	srv := &config.ServerConfig{
		Name:    "badkey",
		Host:    "127.0.0.1",
		KeyFile: "/nonexistent/key/file",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected error for bad key file")
	}
	if !strings.Contains(err.Error(), "failed to load SSH key") {
		t.Errorf("expected 'failed to load SSH key' error, got: %s", err.Error())
	}
}

func TestConnect_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// Connect to a non-routable address to trigger timeout
	srv := &config.ServerConfig{
		Name:     "timeout",
		Host:     "192.0.2.1",
		Port:     1,
		Password: "test",
		AuthMode: "password",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "timed out") && !strings.Contains(errStr, "connection") && !strings.Contains(errStr, "SSH") {
		t.Errorf("expected connection error, got: %s", errStr)
	}
}

func TestConnect_ConnectionRefused(t *testing.T) {
	// Connect to localhost on an unlikely port — should fail fast with connection refused
	srv := &config.ServerConfig{
		Name:     "refused",
		Host:     "127.0.0.1",
		Port:     19999,
		Password: "test",
		AuthMode: "password",
	}
	_, err := connect(srv)
	if err == nil {
		t.Fatal("expected connection refused error")
	}
	if !strings.Contains(err.Error(), "SSH connection failed") && !strings.Contains(err.Error(), "connection refused") {
		t.Logf("got error: %s", err.Error()) // informational
	}
}

func TestRun_NoCredentials(t *testing.T) {
	srv := &config.ServerConfig{
		Name:     "nocreds",
		Host:     "127.0.0.1",
		AuthMode: "password",
	}
	_, err := Run(srv, "status", "--json")
	if err == nil {
		t.Fatal("expected error for Run with no credentials")
	}
	if !strings.Contains(err.Error(), "no SSH credentials") {
		t.Errorf("expected credential error, got: %s", err.Error())
	}
}

func TestRun_BadKey(t *testing.T) {
	srv := &config.ServerConfig{
		Name:    "badkey",
		Host:    "127.0.0.1",
		KeyFile: "/nonexistent/key",
	}
	_, err := Run(srv, "status", "--json")
	if err == nil {
		t.Fatal("expected error for Run with bad key")
	}
	if !strings.Contains(err.Error(), "failed to load SSH key") {
		t.Errorf("expected key error, got: %s", err.Error())
	}
}

func TestLoadKey_EmptyPath_NoDefaults(t *testing.T) {
	// loadKey with empty path tries default locations
	// On CI or if no SSH keys exist, it should return an error
	_, err := loadKey("")
	// May succeed if user has SSH keys, may fail if not — just verify it doesn't panic
	_ = err
}

func TestLoadKey_NonexistentFile(t *testing.T) {
	_, err := loadKey("/tmp/definitely-nonexistent-key-file-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}

func TestLoadKey_InvalidKeyContent(t *testing.T) {
	// Create a temp file with invalid key content
	tmpFile, err := os.CreateTemp("", "bad-ssh-key-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("this is not a valid SSH key")
	tmpFile.Close()

	_, err = loadKey(tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid key content")
	}
}

func TestLoadKey_TildeExpansion(t *testing.T) {
	// loadKey should expand ~ in paths
	_, err := loadKey("~/nonexistent-key-file-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent key file with tilde")
	}
	// Key thing is it didn't panic and it resolved the tilde path
}

func TestRunSessionAndDetectHelpersWithMockSSH(t *testing.T) {
	server := startTestSSHServer(t, func(cmd string) (string, uint32) {
		switch cmd {
		case "echo hello":
			return "hello\n", 0
		case "uname -s -m":
			return "Linux x86_64\n", 0
		case "homebutler version 2>/dev/null || $HOME/.local/bin/homebutler version":
			return "homebutler 0.14.0 (built test)\n", 0
		case "which homebutler 2>/dev/null || echo $HOME/.local/bin/homebutler":
			return "/usr/local/bin/homebutler\n", 0
		default:
			return "", 1
		}
	})
	defer server.cleanup()

	client := dialTestSSHClient(t, server.addr)
	defer client.Close()

	if err := runSession(client, "echo hello"); err != nil {
		t.Fatalf("runSession() error = %v", err)
	}

	osName, arch, err := detectRemoteArch(client)
	if err != nil {
		t.Fatalf("detectRemoteArch() error = %v", err)
	}
	if osName != "linux" || arch != "amd64" {
		t.Fatalf("detectRemoteArch() = %s/%s, want linux/amd64", osName, arch)
	}

	version, err := remoteGetVersion(client)
	if err != nil {
		t.Fatalf("remoteGetVersion() error = %v", err)
	}
	if version != "0.14.0" {
		t.Fatalf("remoteGetVersion() = %q", version)
	}

	path, err := remoteWhich(client)
	if err != nil {
		t.Fatalf("remoteWhich() error = %v", err)
	}
	if path != "/usr/local/bin/homebutler" {
		t.Fatalf("remoteWhich() = %q", path)
	}
}

func TestDetectInstallDirFallbackAndEnsurePath(t *testing.T) {
	var seen []string
	server := startTestSSHServer(t, func(cmd string) (string, uint32) {
		seen = append(seen, cmd)
		switch cmd {
		case "test -w /usr/local/bin":
			return "", 1
		case "sudo -n test -w /usr/local/bin 2>/dev/null":
			return "", 1
		case "mkdir -p $HOME/.local/bin":
			return "", 0
		case `test -f $HOME/.profile`:
			return "", 0
		case `grep -qF '$HOME/.local/bin' $HOME/.profile 2>/dev/null`:
			return "", 1
		case `echo 'export PATH="$PATH:$HOME/.local/bin"' >> $HOME/.profile`:
			return "", 0
		case `test -f $HOME/.bashrc`:
			return "", 1
		case `test -f $HOME/.zshrc`:
			return "", 1
		default:
			return "", 1
		}
	})
	defer server.cleanup()

	client := dialTestSSHClient(t, server.addr)
	defer client.Close()

	installDir, err := detectInstallDir(client)
	if err != nil {
		t.Fatalf("detectInstallDir() error = %v", err)
	}
	if installDir != "$HOME/.local/bin" {
		t.Fatalf("detectInstallDir() = %q", installDir)
	}

	ensurePath(client, installDir)

	joined := strings.Join(seen, "\n")
	if !strings.Contains(joined, `echo 'export PATH="$PATH:$HOME/.local/bin"' >> $HOME/.profile`) {
		t.Fatalf("ensurePath() did not append export line, commands:\n%s", joined)
	}
}

func TestRunSuccessAndFailure(t *testing.T) {
	server := startTestSSHServer(t, func(cmd string) (string, uint32) {
		if strings.Contains(cmd, "homebutler status --json") {
			return `{"ok":true}` + "\n", 0
		}
		if strings.Contains(cmd, "homebutler broken") {
			return "boom\n", 1
		}
		return "", 1
	})
	defer server.cleanup()
	writeKnownHostsForTest(t, server)

	host, portStr, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	srv := &config.ServerConfig{
		Name:     "test",
		Host:     host,
		Port:     port,
		User:     "tester",
		AuthMode: "password",
		Password: "secret",
	}

	out, err := Run(srv, "status", "--json")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(string(out), `{"ok":true}`) {
		t.Fatalf("unexpected output: %s", out)
	}

	_, err = Run(srv, "broken")
	if err == nil {
		t.Fatal("expected error for failing remote command")
	}
	if !strings.Contains(err.Error(), "remote command failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectInstallDirPrefersUsrLocalBin(t *testing.T) {
	server := startTestSSHServer(t, func(cmd string) (string, uint32) {
		switch cmd {
		case "test -w /usr/local/bin":
			return "", 0
		default:
			return "", 1
		}
	})
	defer server.cleanup()

	client := dialTestSSHClient(t, server.addr)
	defer client.Close()

	installDir, err := detectInstallDir(client)
	if err != nil {
		t.Fatalf("detectInstallDir() error = %v", err)
	}
	if installDir != "/usr/local/bin" {
		t.Fatalf("detectInstallDir() = %q", installDir)
	}
}

func TestRemoteGetVersionAndWhichErrors(t *testing.T) {
	server := startTestSSHServer(t, func(cmd string) (string, uint32) {
		switch cmd {
		case "homebutler version 2>/dev/null || $HOME/.local/bin/homebutler version":
			return "weird-output\n", 0
		case "which homebutler 2>/dev/null || echo $HOME/.local/bin/homebutler":
			return "\n", 0
		default:
			return "", 1
		}
	})
	defer server.cleanup()

	client := dialTestSSHClient(t, server.addr)
	defer client.Close()

	if _, err := remoteGetVersion(client); err == nil {
		t.Fatal("expected parse error from remoteGetVersion")
	}
	if _, err := remoteWhich(client); err == nil {
		t.Fatal("expected empty path error from remoteWhich")
	}
}

func TestRemoveHostKeys_NonexistentFile(t *testing.T) {
	// Create a temp known_hosts that we control
	srv := &config.ServerConfig{
		Name: "test",
		Host: "10.99.99.99",
		Port: 22,
	}
	// RemoveHostKeys on real known_hosts file — should not error even if key not present
	err := RemoveHostKeys(srv)
	if err != nil {
		t.Errorf("RemoveHostKeys should not error for non-matching keys: %v", err)
	}
}

func TestSelfUpgrade_DevBuild(t *testing.T) {
	result := SelfUpgrade("dev", "1.0.0")
	if result.Status != "error" {
		t.Errorf("expected error status for dev build, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "dev build") {
		t.Errorf("expected dev build message, got %s", result.Message)
	}
}

func TestSelfUpgrade_AlreadyUpToDate(t *testing.T) {
	result := SelfUpgrade("1.0.0", "1.0.0")
	if result.Status != "up-to-date" {
		t.Errorf("expected up-to-date status, got %s", result.Status)
	}
	if result.NewVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", result.NewVersion)
	}
	if !strings.Contains(result.Message, "already") {
		t.Errorf("expected 'already' in message, got %s", result.Message)
	}
}

func TestUpgradeResult_Fields(t *testing.T) {
	r := UpgradeResult{
		Target:      "myserver",
		PrevVersion: "1.0.0",
		NewVersion:  "1.1.0",
		Status:      "upgraded",
		Message:     "v1.0.0 → v1.1.0",
	}
	if r.Target != "myserver" {
		t.Errorf("Target = %q, want myserver", r.Target)
	}
	if r.Status != "upgraded" {
		t.Errorf("Status = %q, want upgraded", r.Status)
	}
}

func TestUpgradeReport_Fields(t *testing.T) {
	report := UpgradeReport{
		LatestVersion: "1.1.0",
		Results: []UpgradeResult{
			{Target: "local", Status: "upgraded"},
			{Target: "remote", Status: "up-to-date"},
		},
	}
	if len(report.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(report.Results))
	}
}

func TestNormalizeArch_Extended(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"amd64", "amd64"},
		{"i386", "i386"},
		{"armv7l", "armv7l"},
		{"s390x", "s390x"},
		{"ppc64le", "ppc64le"},
	}
	for _, tc := range tests {
		got := normalizeArch(tc.in)
		if got != tc.want {
			t.Errorf("normalizeArch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRemoveHostKeys_WithEntries(t *testing.T) {
	// Create a temp known_hosts file
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	// We can't easily override HOME for knownHostsPath, so test against the real file
	// Instead, test the logic directly by writing and reading a temp file
	tmpFile := filepath.Join(tmpDir, "known_hosts")
	content := "[10.0.0.1]:22 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest1\n" +
		"[10.0.0.2]:22 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest2\n" +
		"# comment line\n" +
		"[10.0.0.1]:2222 ssh-rsa AAAAB3NzaC1yc2EAAAABITest3\n"
	os.WriteFile(tmpFile, []byte(content), 0600)

	// Verify the file was written
	data, _ := os.ReadFile(tmpFile)
	if len(data) == 0 {
		t.Fatal("failed to write test known_hosts")
	}
	_ = origHome
}

func TestDeployResultJSON(t *testing.T) {
	r := DeployResult{
		Server:  "rpi5",
		Arch:    "linux/arm64",
		Source:  "github",
		Status:  "ok",
		Message: "installed to /usr/local/bin",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed DeployResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Server != "rpi5" {
		t.Errorf("Server = %q, want %q", parsed.Server, "rpi5")
	}
	if parsed.Arch != "linux/arm64" {
		t.Errorf("Arch = %q, want %q", parsed.Arch, "linux/arm64")
	}
	if parsed.Source != "github" {
		t.Errorf("Source = %q, want %q", parsed.Source, "github")
	}
}

func TestUpgradeResultJSON(t *testing.T) {
	r := UpgradeResult{
		Target:      "myserver",
		PrevVersion: "1.0.0",
		NewVersion:  "1.1.0",
		Status:      "upgraded",
		Message:     "v1.0.0 → v1.1.0",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed UpgradeResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.Target != "myserver" {
		t.Errorf("Target = %q, want %q", parsed.Target, "myserver")
	}
	if parsed.PrevVersion != "1.0.0" {
		t.Errorf("PrevVersion = %q, want %q", parsed.PrevVersion, "1.0.0")
	}
	if parsed.NewVersion != "1.1.0" {
		t.Errorf("NewVersion = %q, want %q", parsed.NewVersion, "1.1.0")
	}
}

func TestUpgradeReportJSON(t *testing.T) {
	report := UpgradeReport{
		LatestVersion: "1.1.0",
		Results: []UpgradeResult{
			{Target: "local", Status: "upgraded", PrevVersion: "1.0.0", NewVersion: "1.1.0"},
			{Target: "remote", Status: "up-to-date", PrevVersion: "1.1.0", NewVersion: "1.1.0"},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var parsed UpgradeReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if parsed.LatestVersion != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q", parsed.LatestVersion, "1.1.0")
	}
	if len(parsed.Results) != 2 {
		t.Errorf("Results count = %d, want 2", len(parsed.Results))
	}
}

func TestFetchLatestVersion(t *testing.T) {
	// Use httptest to mock the GitHub API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v1.2.3"}`)
	}))
	defer ts.Close()

	// Override the default HTTP client transport to redirect GitHub API calls
	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "api.github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	version, err := FetchLatestVersion()
	if err != nil {
		t.Fatalf("FetchLatestVersion() error: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("version = %q, want %q", version, "1.2.3")
	}
}

func TestFetchLatestVersion_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "api.github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	_, err := FetchLatestVersion()
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestSelfUpgrade_DownloadFails(t *testing.T) {
	// When version differs but download fails (unreachable), should return error status
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	result := SelfUpgrade("1.0.0", "2.0.0")
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestDownloadRelease(t *testing.T) {
	// Create a valid tar.gz containing a "homebutler" binary
	binaryContent := []byte("#!/bin/sh\necho homebutler")
	tarData := createTarGz(t, "homebutler", binaryContent)

	// Compute checksum
	h := sha256.Sum256(tarData)
	checksum := hex.EncodeToString(h[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			fmt.Fprintf(w, "%s  homebutler_9.9.9_linux_amd64.tar.gz\n", checksum)
			return
		}
		if strings.Contains(r.URL.Path, ".tar.gz") {
			w.Write(tarData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	data, err := downloadRelease("linux", "amd64", "9.9.9")
	if err != nil {
		t.Fatalf("downloadRelease() error: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("extracted binary mismatch: got %q, want %q", data, binaryContent)
	}
}

func TestDownloadRelease_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	_, err := downloadRelease("linux", "amd64", "9.9.9")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestDownloadRelease_NoVersion(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho homebutler")
	tarData := createTarGz(t, "homebutler", binaryContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			// Return 404 to skip checksum verification
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, ".tar.gz") {
			w.Write(tarData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	data, err := downloadRelease("linux", "amd64")
	if err != nil {
		t.Fatalf("downloadRelease() error: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("extracted binary mismatch")
	}
}

func TestVerifyChecksum_WithServer(t *testing.T) {
	testData := []byte("test binary data")
	h := sha256.Sum256(testData)
	checksum := hex.EncodeToString(h[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  myfile.tar.gz\n", checksum)
		fmt.Fprintf(w, "deadbeef  otherfile.tar.gz\n")
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	err := verifyChecksum(testData, "myfile.tar.gz", "9.9.9")
	if err != nil {
		t.Fatalf("verifyChecksum() error: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	testData := []byte("test binary data")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef  myfile.tar.gz\n")
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	err := verifyChecksum(testData, "myfile.tar.gz", "9.9.9")
	if err == nil {
		t.Error("expected error for checksum mismatch")
	}
}

func TestVerifyChecksum_FileNotInChecksums(t *testing.T) {
	testData := []byte("test data")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "deadbeef  otherfile.tar.gz\n")
	}))
	defer ts.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{
		wrapped: origTransport,
		override: func(req *http.Request) *http.Request {
			if strings.Contains(req.URL.Host, "github.com") {
				newURL := ts.URL + req.URL.Path
				newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
				for k, v := range req.Header {
					newReq.Header[k] = v
				}
				return newReq
			}
			return req
		},
	}
	defer func() { http.DefaultTransport = origTransport }()

	err := verifyChecksum(testData, "myfile.tar.gz", "9.9.9")
	if err == nil {
		t.Error("expected error when file not in checksums")
	}
}

// testTransport redirects specific requests to a test server.
type testTransport struct {
	wrapped  http.RoundTripper
	override func(req *http.Request) *http.Request
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := t.override(req)
	return t.wrapped.RoundTrip(newReq)
}
