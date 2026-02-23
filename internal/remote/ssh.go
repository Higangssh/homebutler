package remote

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"golang.org/x/crypto/ssh"
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
		return nil, fmt.Errorf("ssh connect to %s: %w", server.Name, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session to %s: %w", server.Name, err)
	}
	defer session.Close()

	// Try configured bin path, then common locations
	binPath := server.SSHBinPath()
	cmd := fmt.Sprintf("export PATH=$HOME/.local/bin:$HOME/bin:/usr/local/bin:$PATH; %s %s", binPath, strings.Join(args, " "))
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("remote command on %s: %w\noutput: %s", server.Name, err, string(out))
	}

	return out, nil
}

func connect(server *config.ServerConfig) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if server.UseKeyAuth() {
		signer, err := loadKey(server.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load SSH key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		if server.Password == "" {
			return nil, fmt.Errorf("password auth selected but no password configured for %s", server.Name)
		}
		authMethods = append(authMethods, ssh.Password(server.Password))
	}

	cfg := &ssh.ClientConfig{
		User:            server.SSHUser(),
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: known_hosts support
		Timeout:         dialTimeout,
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.SSHPort())
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, fmt.Errorf("connection timed out to %s (%s)", server.Name, addr)
		}
		return nil, fmt.Errorf("failed to connect to %s (%s): %w", server.Name, addr, err)
	}

	return client, nil
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
