package remote

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"golang.org/x/crypto/ssh"
)

const (
	releaseURL  = "https://github.com/Higangssh/homebutler/releases/latest/download"
	remoteBin   = "$HOME/bin/homebutler"
	installDir  = "$HOME/bin"
)

// DeployResult holds the result of a deploy operation.
type DeployResult struct {
	Server  string `json:"server"`
	Arch    string `json:"arch"`
	Source  string `json:"source"` // "github" or "local"
	Status  string `json:"status"` // "ok" or "error"
	Message string `json:"message,omitempty"`
}

// Deploy installs homebutler on a remote server.
// If localBin is set, it copies that file directly (air-gapped mode).
// Otherwise, it downloads the correct binary from GitHub Releases.
func Deploy(server *config.ServerConfig, localBin string) (*DeployResult, error) {
	result := &DeployResult{Server: server.Name}

	// Connect
	client, err := connect(server)
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", server.Name, err)
	}
	defer client.Close()

	// Detect remote arch
	remoteOS, remoteArch, err := detectRemoteArch(client)
	if err != nil {
		return nil, fmt.Errorf("detect arch on %s: %w", server.Name, err)
	}
	result.Arch = remoteOS + "/" + remoteArch

	// Ensure ~/bin exists
	if err := runSession(client, "mkdir -p $HOME/bin"); err != nil {
		return nil, fmt.Errorf("mkdir on %s: %w", server.Name, err)
	}

	if localBin != "" {
		// Air-gapped: copy local file
		result.Source = "local"
		data, err := os.ReadFile(localBin)
		if err != nil {
			return nil, fmt.Errorf("read local binary: %w", err)
		}
		if err := scpUpload(client, data, "bin/homebutler", 0755); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	} else {
		// Download from GitHub
		result.Source = "github"
		data, err := downloadRelease(remoteOS, remoteArch)
		if err != nil {
			// Check if it's a network error and suggest --local
			return nil, fmt.Errorf("download for %s/%s: %w\n\nFor air-gapped environments, use:\n  homebutler deploy --server %s --local ./homebutler-%s-%s",
				remoteOS, remoteArch, err, server.Name, remoteOS, remoteArch)
		}
		if err := scpUpload(client, data, "bin/homebutler", 0755); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	}

	// Verify installation
	if err := runSession(client, "$HOME/bin/homebutler version"); err != nil {
		result.Status = "error"
		result.Message = "uploaded but verification failed: " + err.Error()
		return result, nil
	}

	result.Status = "ok"
	result.Message = fmt.Sprintf("installed to ~/bin/homebutler (%s/%s)", remoteOS, remoteArch)
	return result, nil
}

// DeployLocal validates architecture match when deploying current binary without --local flag.
func ValidateLocalArch(remoteOS, remoteArch string) error {
	localOS := runtime.GOOS
	localArch := runtime.GOARCH
	if localOS != remoteOS || localArch != remoteArch {
		return fmt.Errorf("local binary is %s/%s but remote is %s/%s\n\n"+
			"To deploy to a different architecture in air-gapped environments:\n"+
			"  1. Cross-compile: CGO_ENABLED=0 GOOS=%s GOARCH=%s go build -o homebutler-%s-%s\n"+
			"  2. Deploy: homebutler deploy --server <name> --local ./homebutler-%s-%s",
			localOS, localArch, remoteOS, remoteArch,
			remoteOS, remoteArch, remoteOS, remoteArch,
			remoteOS, remoteArch)
	}
	return nil
}

func detectRemoteArch(client *ssh.Client) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput("uname -s -m")
	if err != nil {
		return "", "", err
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 2 {
		return "", "", fmt.Errorf("unexpected uname output: %s", string(out))
	}

	osName := strings.ToLower(parts[0]) // Linux -> linux, Darwin -> darwin
	arch := normalizeArch(parts[1])

	return osName, arch, nil
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

func downloadRelease(osName, arch string) ([]byte, error) {
	filename := fmt.Sprintf("homebutler_%s_%s.tar.gz", osName, arch)
	url := releaseURL + "/" + filename

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	// Download tar.gz and extract the binary
	tarData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return extractBinaryFromTarGz(tarData)
}

func runSession(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}

// scpUpload writes data to a remote file using SCP protocol.
func scpUpload(client *ssh.Client, data []byte, remotePath string, mode os.FileMode) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", mode, len(data), filepath.Base(remotePath))
		w.Write(data)
		fmt.Fprint(w, "\x00")
	}()

	dir := filepath.Dir(remotePath)
	if dir == "." {
		dir = ""
	}
	return session.Run(fmt.Sprintf("scp -t %s", remotePath))
}
