package remote

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/util"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	dialTimeout = 10 * time.Second
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
		return nil, classified(ClassRemote, "[%s] failed to open SSH session: %w\n  → Check if the server is accepting new connections", server.Name, err)
	}
	defer session.Close()

	// Try configured bin path, then common locations
	binPath := server.SSHBinPath()
	// Quote all arguments to prevent shell injection
	cmd := fmt.Sprintf("export PATH=$HOME/.local/bin:$HOME/bin:$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/local/sbin:/snap/bin:$PATH; %s %s", util.ShellQuote(binPath), util.ShellQuoteArgs(args))
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return nil, classified(ClassRemote, "[%s] remote command failed: %w\n  → Output: %s\n  → Check if homebutler is installed on the remote server: homebutler deploy %s", server.Name, err, strings.TrimSpace(string(out)), server.Name)
	}

	return out, nil
}

func connect(server *config.ServerConfig) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if server.UseKeyAuth() {
		signer, err := loadKey(server.KeyFile)
		if err != nil {
			return nil, classified(ClassAuthentication, "[%s] failed to load SSH key (%s): %w\n  → Check the key_file path in your config: ~/.config/homebutler/config.yaml", server.Name, server.KeyFile, err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		if server.Password == "" {
			return nil, classified(ClassAuthentication, "[%s] no SSH credentials configured\n  → Add 'key_file' or 'password' to this server in ~/.config/homebutler/config.yaml", server.Name)
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
			return nil, classified(ClassUnreachable, "[%s] connection timed out (%s)\n  → Check if the server is online and reachable\n  → Verify host/port in ~/.config/homebutler/config.yaml", server.Name, addr)
		}
		// Wrap known_hosts errors with actionable messages
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) > 0 {
				// Key mismatch — known_hosts has a different key
				return nil, classified(ClassHostKey, "[%s] ⚠️  SSH HOST KEY CHANGED (%s)\n"+
					"  The server's host key does not match the one in ~/.ssh/known_hosts.\n"+
					"  This could mean:\n"+
					"    1. The server was reinstalled or its SSH keys were regenerated\n"+
					"    2. A man-in-the-middle attack is in progress\n\n"+
					"  → If you trust this change: homebutler trust %s --reset\n"+
					"  → If unexpected: do NOT connect and investigate", server.Name, addr, server.Name)
			}
			// Unknown host. Trusting it on sight is a reasonable trade for key
			// authentication: the private key never leaves this machine, and
			// the worst a stranger at that address learns is that somebody
			// tried to reach it.
			//
			// A password is not that. Trusting the address and then sending
			// the password to it hands the credential to whoever answered —
			// a machine reached by a typo, by a stale DNS record, or by an
			// address someone with access to the config changed on purpose.
			// There is no signal afterwards either: the failure looks like a
			// wrong password. So a password stops here and asks for the host
			// to be trusted deliberately, which `homebutler trust` does.
			if !server.UseKeyAuth() {
				return nil, classified(ClassHostKey, "[%s] this host is not in ~/.ssh/known_hosts, and this server signs in with a password (%s)\n"+
					"  homebutler will not send a password to a host it has not been told to trust: whatever answers at that\n"+
					"  address would receive it, and a wrong address looks exactly like a wrong password afterwards.\n"+
					"  → Check that the address is the machine you mean, then: homebutler trust %s", server.Name, addr, server.Name)
			}

			// Key authentication: TOFU — record the host key and retry.
			tofuErr := tofuConnect(addr, cfg)
			if tofuErr == nil {
				// Reload known_hosts and retry
				newCb, cbErr := newKnownHostsCallback()
				if cbErr == nil {
					retryCfg := *cfg
					retryCfg.HostKeyCallback = newCb
					retryClient, retryErr := ssh.Dial("tcp", addr, &retryCfg)
					if retryErr != nil {
						return nil, classified(ClassHostKey, "[%s] connected but failed to establish session after registering host key (%s): %w\n  → Try again, or manually: homebutler trust %s", server.Name, addr, retryErr, server.Name)
					}
					return retryClient, nil
				}
			}
			return nil, tofuFailureError(server, addr, tofuErr)
		}
		// Generic SSH error. x/crypto reports a refused credential in the
		// message rather than a distinct type, and the difference matters to
		// anything downstream: one is a machine that is down, the other is a
		// machine that is up and said no.
		class := ClassUnreachable
		if strings.Contains(err.Error(), "unable to authenticate") {
			class = ClassAuthentication
		}
		return nil, classified(class, "[%s] SSH connection failed (%s): %w\n  → Check: server online? correct host/port? firewall rules?\n  → Config: ~/.config/homebutler/config.yaml", server.Name, addr, err)
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

	// The probe carries no credentials. A host key arrives during the key
	// exchange, before authentication, so nothing has to be offered to read
	// it — and this connection is to a machine homebutler has decided nothing
	// about yet. Copying the whole config here meant the password was sent to
	// whatever answered at that address: a config pointed at the wrong host,
	// by a typo or by someone who could edit it, gave the password away before
	// the host key was so much as written down.
	var hostKey ssh.PublicKey
	captureCfg := *cfg
	captureCfg.Auth = nil
	captureCfg.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		hostKey = key
		return nil
	}

	client, err := ssh.Dial("tcp", addr, &captureCfg)
	if err == nil {
		client.Close()
	}

	// Without credentials the handshake is expected to end at authentication.
	// The key exchange came first, so the failure says nothing about whether
	// the host key was read: only whether hostKey is set does.
	if hostKey == nil {
		return &tofuDialError{err: err}
	}

	fingerprint := ssh.FingerprintSHA256(hostKey)
	fmt.Fprintf(os.Stderr, "[%s] trusting new SSH host key for %s: %s\n", addr, addr, fingerprint)

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
	// Said early rather than after a round trip: a server with no usable
	// credential cannot be connected to once it is trusted, so there is no
	// point recording its key.
	if server.UseKeyAuth() {
		if _, err := loadKey(server.KeyFile); err != nil {
			return fmt.Errorf("load SSH key: %w", err)
		}
	} else if server.Password == "" {
		return fmt.Errorf("password auth selected but no password configured for %s", server.Name)
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.SSHPort())
	var serverKey ssh.PublicKey

	// Connect with a callback that captures the host key but always fails, so
	// we can show the fingerprint before committing.
	//
	// It carries no credentials. The key exchange comes before authentication
	// and the callback stops the handshake there, so nothing is offered to a
	// host that is, by definition, not trusted yet — the checks above are for
	// saying early that the credential is missing, not for using it here.
	captureCfg := &ssh.ClientConfig{
		User: server.SSHUser(),
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

// tofuDialError marks a TOFU failure whose cause was the SSH connection itself
// (network or authentication) rather than host key registration.
type tofuDialError struct{ err error }

func (e *tofuDialError) Error() string { return e.err.Error() }
func (e *tofuDialError) Unwrap() error { return e.err }

// tofuFailureError builds the error returned when TOFU auto-registration fails.
func tofuFailureError(server *config.ServerConfig, addr string, tofuErr error) error {
	connHint := fmt.Sprintf("→ Check SSH connectivity: ssh %s@%s -p %d",
		server.SSHUser(), server.Host, server.SSHPort())

	// The connection itself failed, so the host key never came into play.
	// Pointing at `homebutler trust` here would send the user down the wrong path.
	var dialErr *tofuDialError
	if errors.As(tofuErr, &dialErr) {
		// The connection never got as far as a host key, so this is the host
		// being unreachable rather than a trust problem.
		return classified(ClassUnreachable, "[%s] SSH connection failed while registering a new host key (%s): %w\n  %s",
			server.Name, addr, tofuErr, connHint)
	}

	// known_hosts could not be written because the filesystem holding it is
	// read-only — which is how the container image mounts ~/.ssh, on purpose.
	// "Register manually" is not something that can be done from in there: the
	// same command hits the same read-only file. The way out is to trust the
	// host where the key lives and restart the container, which is what
	// docs/docker.md says.
	if errors.Is(tofuErr, syscall.EROFS) {
		return classified(ClassHostKey, "[%s] the host key for %s is new, and ~/.ssh/known_hosts is read-only here: %w\n"+
			"  → Trust it where the key lives, then restart this container: homebutler trust %s",
			server.Name, addr, tofuErr, server.Name)
	}

	return classified(ClassHostKey, "[%s] failed to auto-register host key for %s: %w\n  → Register manually: homebutler trust %s\n  %s",
		server.Name, addr, tofuErr, server.Name, connHint)
}
