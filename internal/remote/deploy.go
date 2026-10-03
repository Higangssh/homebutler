package remote

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
func Deploy(server *config.ServerConfig, localBin, releaseVersion string) (*DeployResult, error) {
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

	// Determine install path on remote: try /usr/local/bin, fallback to ~/.local/bin
	installDir, viaSudo, err := detectInstallDir(client)
	if err != nil {
		return nil, fmt.Errorf("detect install dir on %s: %w", server.Name, err)
	}
	target := installDir + "/homebutler"

	if localBin != "" {
		// Air-gapped: copy local file
		result.Source = "local"
		data, err := os.ReadFile(localBin)
		if err != nil {
			return nil, fmt.Errorf("read local binary: %w", err)
		}
		if err := install(client, data, target, viaSudo); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	} else {
		// Download from GitHub
		result.Source = "github"
		if releaseVersion == "" {
			return nil, fmt.Errorf("release version is required for GitHub deploy")
		}
		data, err := downloadRelease(remoteOS, remoteArch, releaseVersion)
		if err != nil {
			return nil, fmt.Errorf("download for %s/%s: %w\n\nFor air-gapped environments, use:\n  homebutler deploy --server %s --local ./homebutler-%s-%s",
				remoteOS, remoteArch, err, server.Name, remoteOS, remoteArch)
		}
		if err := install(client, data, target, viaSudo); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	}

	// Verify the binary that was just written, by its path. Going through
	// PATH would run whichever homebutler came first, which on a machine
	// that already had one is not necessarily this one.
	quoted, err := quotePath(target)
	if err != nil {
		return nil, err
	}
	if err := runSession(client, quoted+" version"); err != nil {
		result.Status = "error"
		result.Message = "uploaded but verification failed: " + err.Error()
		return result, nil
	}

	// Add to PATH permanently if needed
	ensurePath(client, installDir)

	result.Status = "ok"
	result.Message = fmt.Sprintf("installed to %s/homebutler (%s/%s)", installDir, remoteOS, remoteArch)
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

// detectInstallDir finds the best install location on the remote server, and
// whether writing there needs sudo.
// Priority: /usr/local/bin (writable or via sudo) > ~/.local/bin
//
// The directory comes back absolute. It used to be "$HOME/.local/bin", and
// once v0.10.0 quoted remote paths that string reached scp unexpanded (#303).
func detectInstallDir(client *ssh.Client) (dir string, viaSudo bool, err error) {
	// Try /usr/local/bin
	if err := runSession(client, "test -w /usr/local/bin"); err == nil {
		return "/usr/local/bin", false, nil
	}
	// Try with sudo
	if err := runSession(client, "sudo -n test -w /usr/local/bin 2>/dev/null"); err == nil {
		return "/usr/local/bin", true, nil
	}
	// Fallback: ~/.local/bin
	home, err := runOutput(client, `printf %s "$HOME"`)
	if err != nil || !strings.HasPrefix(home, "/") {
		return "", false, fmt.Errorf("could not read the remote home directory (got %q): %v", home, err)
	}
	dir = home + "/.local/bin"
	quoted, err := quotePath(dir)
	if err != nil {
		return "", false, err
	}
	if err := runSession(client, "mkdir -p "+quoted); err != nil {
		return "", false, fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, false, nil
}

// install writes the binary to target. Through sudo it is uploaded to a
// temporary file the login user owns and moved into place by install(1): scp
// runs as the login user, and the sudo branch used to choose /usr/local/bin and
// then upload there without sudo, which could only fail.
func install(client *ssh.Client, data []byte, target string, viaSudo bool) error {
	if !viaSudo {
		return scpUpload(client, data, target, 0755)
	}
	tmp, err := runOutput(client, "mktemp")
	if err != nil || !strings.HasPrefix(tmp, "/") {
		return fmt.Errorf("create a temporary file for the sudo install (got %q): %v", tmp, err)
	}
	qtmp, err := quotePath(tmp)
	if err != nil {
		return err
	}
	defer runSession(client, "rm -f "+qtmp)
	if err := scpUpload(client, data, tmp, 0600); err != nil {
		return err
	}
	qtarget, err := quotePath(target)
	if err != nil {
		return err
	}
	if out, err := runCombined(client, "sudo -n install -m 0755 "+qtmp+" "+qtarget); err != nil {
		return fmt.Errorf("sudo install into %s: %w%s", target, err, reason(out))
	}
	return nil
}

// ensurePath adds installDir to PATH in shell rc files if not already present.
// Covers .profile, .bashrc, and .zshrc for broad compatibility.
func ensurePath(client *ssh.Client, installDir string) {
	if installDir == "/usr/local/bin" {
		return // already in PATH on most systems
	}

	exportLine := fmt.Sprintf(`export PATH="$PATH:%s"`, installDir)
	// Before #303 the line was written with a literal $HOME, so a file that
	// already has that one is already patched.
	legacy := installDir
	if strings.HasSuffix(installDir, "/.local/bin") {
		legacy = "$HOME/.local/bin"
	}
	rcFiles := []string{"$HOME/.profile", "$HOME/.bashrc", "$HOME/.zshrc"}

	for _, rc := range rcFiles {
		// Only patch files that exist
		checkExist := fmt.Sprintf(`test -f %s`, rc)
		if err := runSession(client, checkExist); err != nil {
			continue
		}
		// Skip if already present
		checkCmd := fmt.Sprintf(`grep -qF -e %s -e %s %s 2>/dev/null`, quoteLiteral(installDir), quoteLiteral(legacy), rc)
		if err := runSession(client, checkCmd); err != nil {
			addCmd := fmt.Sprintf(`echo %s >> %s`, quoteLiteral(exportLine), rc)
			runSession(client, addCmd)
		}
	}
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

func downloadRelease(osName, arch, version string) ([]byte, error) {
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}

	filename := fmt.Sprintf("homebutler_%s_%s_%s.tar.gz", version, osName, arch)
	url := fmt.Sprintf("https://github.com/Higangssh/homebutler/releases/download/v%s/%s", version, filename)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	// Download tar.gz
	tarData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Verify checksum
	if err := verifyChecksum(tarData, filename, version); err != nil {
		return nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	return extractBinaryFromTarGz(tarData)
}

// verifyChecksum downloads checksums.txt and verifies the SHA256 hash.
func verifyChecksum(data []byte, filename, version string) error {
	if version == "" {
		return fmt.Errorf("version is required")
	}

	checksumsURL := fmt.Sprintf("https://github.com/Higangssh/homebutler/releases/download/v%s/checksums.txt", version)

	resp, err := http.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("cannot fetch checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// No checksums available — skip verification for older releases
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	// Find expected hash for our file
	var expectedHash string
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			expectedHash = parts[0]
			break
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("no checksum found for %s", filename)
	}

	// Compute actual hash
	h := sha256.Sum256(data)
	actualHash := hex.EncodeToString(h[:])

	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s\n  expected: %s\n  got:      %s", filename, expectedHash, actualHash)
	}

	return nil
}

func runSession(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}

// runOutput runs cmd and returns its stdout, trimmed.
func runOutput(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.Output(cmd)
	return strings.TrimSpace(string(out)), err
}

// runCombined runs cmd and returns everything it printed.
func runCombined(client *ssh.Client, cmd string) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.CombinedOutput(cmd)
}

// reason turns what a remote command printed into the tail of an error. scp
// reports a failure as a \x01 byte followed by the message, on stdout, and
// that message — "No such file or directory" in #303 — is the only thing that
// says why. It used to be discarded, leaving "Process exited with status 1".
func reason(out []byte) string {
	text := strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return -1
		}
		return r
	}, string(out))
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	return ": " + text
}

// scpUpload writes data to a remote file using the scp protocol.
//
// The receiver answers every step with one byte: 0 to go on, 1 or 2 followed
// by a line saying why not. This waits for each answer. It used to send the
// header and the whole file without reading any, so a refusal arrived while
// twelve megabytes were still being pushed at a process that had stopped
// reading, and the channel closed with the message unread: the user got
// "Process exited with status 1" and nothing else (#303).
func scpUpload(client *ssh.Client, data []byte, remotePath string, mode os.FileMode) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	quoted, err := quotePath(remotePath)
	if err != nil {
		return err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Start("scp -t " + quoted); err != nil {
		return err
	}
	answers := bufio.NewReader(stdout)
	fail := func(step string, err error) error {
		stdin.Close()
		_ = session.Wait()
		return fmt.Errorf("scp %s: %w%s", step, err, reason(stderr.Bytes()))
	}

	if err := scpAck(answers); err != nil {
		return fail("start", err)
	}
	if _, err := fmt.Fprintf(stdin, "C%04o %d %s\n", mode, len(data), filepath.Base(remotePath)); err != nil {
		return fail("header", err)
	}
	if err := scpAck(answers); err != nil {
		return fail("header", err)
	}
	if _, err := stdin.Write(data); err != nil {
		return fail("data", err)
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fail("data", err)
	}
	if err := scpAck(answers); err != nil {
		return fail("data", err)
	}
	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("%w%s", err, reason(stderr.Bytes()))
	}
	return nil
}

// scpAck reads one answer from an scp receiver: nil for 0, and for 1 or 2 the
// message that follows it.
func scpAck(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return fmt.Errorf("no answer from the remote scp: %w", err)
	}
	if b == 0 {
		return nil
	}
	msg, _ := r.ReadString('\n')
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = fmt.Sprintf("refused with code %d", b)
	}
	return errors.New(msg)
}
