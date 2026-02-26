package remote

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	dialTimeout    = 10 * time.Second
	commandTimeout = 30 * time.Second
)

// Run executes a homebutler command on a remote server via SSH.
// It expects homebutler to be installed on the remote host.
func Run(server *config.ServerConfig, args ...string) ([]byte, error) {
	client, err := connect(server)
	if err != nil {
		return nil, err // connect() already returns detailed error messages
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("[%s] failed to open SSH session: %w\n  → Check if the server is accepting new connections", server.Name, err)
	}
	defer session.Close()

	// Try configured bin path, then common locations
	binPath := server.SSHBinPath()
	cmd := fmt.Sprintf("export PATH=$HOME/.local/bin:$HOME/bin:/usr/local/bin:$PATH; %s %s", binPath, strings.Join(args, " "))
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("[%s] remote command failed: %w\n  → Output: %s\n  → Check if homebutler is installed on the remote server: homebutler deploy %s", server.Name, err, strings.TrimSpace(string(out)), server.Name)
	}

	return out, nil
}

func connect(server *config.ServerConfig) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if server.UseKeyAuth() {
		signer, err := loadKey(server.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("[%s] failed to load SSH key (%s): %w\n  → Check the key_file path in your config: ~/.config/homebutler/config.yaml", server.Name, server.KeyFile, err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		if server.Password == "" {
			return nil, fmt.Errorf("[%s] no SSH credentials configured\n  → Add 'key_file' or 'password' to this server in ~/.config/homebutler/config.yaml", server.Name)
		}
		authMethods = append(authMethods, ssh.Password(server.Password))
	}

	hostKeyCallback, err := newKnownHostsCallback()
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            server.SSHUser(),
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         dialTimeout,
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.SSHPort())
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, fmt.Errorf("[%s] connection timed out (%s)\n  → Check if the server is online and reachable\n  → Verify host/port in ~/.config/homebutler/config.yaml", server.Name, addr)
		}
		// Wrap known_hosts errors with actionable messages
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) > 0 {
				// Key mismatch — known_hosts has a different key
				return nil, fmt.Errorf("[%s] ⚠️  SSH HOST KEY CHANGED (%s)\n"+
					"  The server's host key does not match the one in ~/.ssh/known_hosts.\n"+
					"  This could mean:\n"+
					"    1. The server was reinstalled or its SSH keys were regenerated\n"+
					"    2. A man-in-the-middle attack is in progress\n\n"+
					"  → If you trust this change: homebutler trust %s --reset\n"+
					"  → If unexpected: do NOT connect and investigate", server.Name, addr, server.Name)
			}
			// Unknown host — TOFU: auto-add to known_hosts and retry
			if tofuErr := tofuConnect(addr, cfg); tofuErr == nil {
				// Reload known_hosts and retry
				newCb, cbErr := newKnownHostsCallback()
				if cbErr == nil {
					retryCfg := *cfg
					retryCfg.HostKeyCallback = newCb
					retryClient, retryErr := ssh.Dial("tcp", addr, &retryCfg)
					if retryErr != nil {
						return nil, fmt.Errorf("[%s] connected but failed to establish session after registering host key (%s): %w\n  → Try again, or manually: homebutler trust %s", server.Name, addr, retryErr, server.Name)
					}
					return retryClient, nil
				}
			}
			return nil, fmt.Errorf("[%s] failed to auto-register host key for %s\n  → Register manually: homebutler trust %s\n  → Check SSH connectivity: ssh %s@%s -p %d",
				server.Name, addr, server.Name, server.SSHUser(), server.Host, server.SSHPort())
		}
		// Generic SSH error
		return nil, fmt.Errorf("[%s] SSH connection failed (%s): %w\n  → Check: server online? correct host/port? firewall rules?\n  → Config: ~/.config/homebutler/config.yaml", server.Name, addr, err)
	}

	return client, nil
}

// knownHostsPath returns the path to ~/.ssh/known_hosts.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// newKnownHostsCallback returns an ssh.HostKeyCallback that verifies against known_hosts.
func newKnownHostsCallback() (ssh.HostKeyCallback, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	// Ensure ~/.ssh directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("cannot create ~/.ssh directory: %w", err)
	}
	// Create known_hosts if it doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("cannot create known_hosts: %w", err)
		}
		f.Close()
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read known_hosts: %w", err)
	}
	return cb, nil
}

// tofuConnect performs Trust On First Use: connects to get the host key,
// then adds it to known_hosts automatically.
func tofuConnect(addr string, cfg *ssh.ClientConfig) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}

	// Connect with a callback that captures the host key
	var hostKey ssh.PublicKey
	captureCfg := *cfg
	captureCfg.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		hostKey = key
		return nil
	}

	client, err := ssh.Dial("tcp", addr, &captureCfg)
	if err != nil {
		return fmt.Errorf("TOFU dial failed: %w", err)
	}
	client.Close()

	if hostKey == nil {
		return fmt.Errorf("no host key captured")
	}

	// Write to known_hosts
	line := knownhosts.Line([]string{addr}, hostKey)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot write known_hosts: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil {
		return err
	}

	return nil
}

// TrustServer connects to a server, displays its host key fingerprint,
// and adds it to known_hosts if the user confirms.
func TrustServer(server *config.ServerConfig, confirm func(fingerprint string) bool) error {
	var authMethods []ssh.AuthMethod
	if server.UseKeyAuth() {
		signer, err := loadKey(server.KeyFile)
		if err != nil {
			return fmt.Errorf("load SSH key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		if server.Password == "" {
			return fmt.Errorf("password auth selected but no password configured for %s", server.Name)
		}
		authMethods = append(authMethods, ssh.Password(server.Password))
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.SSHPort())
	var serverKey ssh.PublicKey

	// Connect with a callback that captures the host key but always fails,
	// so we can show the fingerprint before committing.
	captureCfg := &ssh.ClientConfig{
		User: server.SSHUser(),
		Auth: authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			serverKey = key
			return fmt.Errorf("key captured") // intentional: we just want the key
		},
		Timeout: dialTimeout,
	}

	ssh.Dial("tcp", addr, captureCfg) // expected to fail
	if serverKey == nil {
		return fmt.Errorf("could not retrieve host key from %s (%s)", server.Name, addr)
	}

	fingerprint := ssh.FingerprintSHA256(serverKey)
	if !confirm(fingerprint) {
		return fmt.Errorf("trust cancelled by user")
	}

	// Add to known_hosts
	khPath, err := knownHostsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(khPath), 0700); err != nil {
		return fmt.Errorf("cannot create ~/.ssh directory: %w", err)
	}

	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, serverKey)
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot write to known_hosts: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("failed to write known_hosts entry: %w", err)
	}

	return nil
}

// RemoveHostKeys removes all known_hosts entries for a server's address.
func RemoveHostKeys(server *config.ServerConfig) error {
	khPath, err := knownHostsPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(khPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot read known_hosts: %w", err)
	}

	addr := knownhosts.Normalize(fmt.Sprintf("%s:%d", server.Host, server.SSHPort()))
	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}
		// known_hosts format: host[,host...] keytype base64key
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			kept = append(kept, line)
			continue
		}
		hosts := strings.Split(fields[0], ",")
		match := false
		for _, h := range hosts {
			if h == addr {
				match = true
				break
			}
		}
		if !match {
			kept = append(kept, line)
		}
	}

	return os.WriteFile(khPath, []byte(strings.Join(kept, "\n")), 0600)
}

func loadKey(keyFile string) (ssh.Signer, error) {
	if keyFile == "" {
		// Try default key locations
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		defaults := []string{
			filepath.Join(home, ".ssh", "id_ed25519"),
			filepath.Join(home, ".ssh", "id_rsa"),
		}
		for _, path := range defaults {
			if signer, err := readKey(path); err == nil {
				return signer, nil
			}
		}
		return nil, fmt.Errorf("no SSH key found (tried ~/.ssh/id_ed25519, ~/.ssh/id_rsa). Specify key path in config or use auth: password")
	}

	// Expand ~ prefix
	if strings.HasPrefix(keyFile, "~/") {
		home, _ := os.UserHomeDir()
		keyFile = filepath.Join(home, keyFile[2:])
	}

	return readKey(keyFile)
}

func readKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}
