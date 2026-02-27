package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"golang.org/x/crypto/ssh"
)

// UpgradeResult holds the result of an upgrade operation for a single target.
type UpgradeResult struct {
	Target      string `json:"target"`
	PrevVersion string `json:"prev_version"`
	NewVersion  string `json:"new_version"`
	Status      string `json:"status"` // "upgraded", "up-to-date", "error"
	Message     string `json:"message,omitempty"`
}

// UpgradeReport is the overall upgrade result.
type UpgradeReport struct {
	LatestVersion string          `json:"latest_version"`
	Results       []UpgradeResult `json:"results"`
}

// FetchLatestVersion queries GitHub API for the latest release tag.
func FetchLatestVersion() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Higangssh/homebutler/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "homebutler")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to check latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

// SelfUpgrade replaces the current binary with the latest version.
func SelfUpgrade(currentVersion, latestVersion string) *UpgradeResult {
	result := &UpgradeResult{
		Target:      "local",
		PrevVersion: currentVersion,
	}

	if currentVersion == "dev" {
		result.Status = "error"
		result.Message = "running dev build — upgrade from release builds only"
		return result
	}

	if currentVersion == latestVersion {
		result.Status = "up-to-date"
		result.NewVersion = currentVersion
		result.Message = fmt.Sprintf("already v%s", currentVersion)
		return result
	}

	// Download new binary
	data, err := downloadRelease(runtime.GOOS, runtime.GOARCH, latestVersion)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}

	// Get current executable path (resolve symlinks)
	execPath, err := os.Executable()
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("cannot find executable path: %v", err)
		return result
	}
	// Resolve symlinks (e.g. Homebrew symlink)
	realPath, err := filepath.EvalSymlinks(execPath)
	if err == nil {
		execPath = realPath
	}

	// Replace binary: rename old → write new → remove old
	backupPath := execPath + ".bak"
	if err := os.Rename(execPath, backupPath); err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("cannot backup current binary: %v", err)
		return result
	}

	if err := os.WriteFile(execPath, data, 0755); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, execPath)
		result.Status = "error"
		result.Message = fmt.Sprintf("cannot write new binary: %v", err)
		return result
	}

	os.Remove(backupPath)

	result.Status = "upgraded"
	result.NewVersion = latestVersion
	result.Message = fmt.Sprintf("v%s → v%s", currentVersion, latestVersion)
	return result
}

// RemoteUpgrade upgrades homebutler on a remote server.
func RemoteUpgrade(server *config.ServerConfig, latestVersion string) *UpgradeResult {
	result := &UpgradeResult{
		Target: server.Name,
	}

	// Connect via SSH
	client, err := connect(server)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("ssh connect: %v", err)
		return result
	}
	defer client.Close()

	// Check current remote version
	remoteVersion, err := remoteGetVersion(client)
	if err != nil {
		result.Status = "error"
		result.PrevVersion = "unknown"
		result.Message = fmt.Sprintf("not installed — run 'homebutler deploy --server %s' first", server.Name)
		return result
	}
	result.PrevVersion = remoteVersion

	if remoteVersion == latestVersion {
		result.Status = "up-to-date"
		result.NewVersion = remoteVersion
		result.Message = fmt.Sprintf("already v%s", remoteVersion)
		return result
	}

	// Detect remote arch
	remoteOS, remoteArch, err := detectRemoteArch(client)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("detect arch: %v", err)
		return result
	}

	// Download binary for remote platform
	data, err := downloadRelease(remoteOS, remoteArch, latestVersion)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("download: %v", err)
		return result
	}

	// Find where homebutler is installed on remote
	installPath, err := remoteWhich(client)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("cannot find remote binary: %v", err)
		return result
	}

	// Upload new binary
	if err := scpUpload(client, data, installPath, 0755); err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("upload: %v", err)
		return result
	}

	// Verify new version
	newVersion, err := remoteGetVersion(client)
	if err != nil {
		result.Status = "error"
		result.Message = "uploaded but verification failed"
		return result
	}

	result.Status = "upgraded"
	result.NewVersion = newVersion
	result.Message = fmt.Sprintf("v%s → v%s (%s/%s)", remoteVersion, newVersion, remoteOS, remoteArch)
	return result
}

// remoteGetVersion runs `homebutler version` on the remote and parses the version string.
func remoteGetVersion(client *ssh.Client) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput("homebutler version 2>/dev/null || $HOME/.local/bin/homebutler version")
	if err != nil {
		return "", fmt.Errorf("homebutler not found on remote")
	}

	// Parse "homebutler 0.7.1 (built ...)" → "0.7.1"
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) >= 2 {
		return parts[1], nil
	}
	return "", fmt.Errorf("unexpected version output: %s", string(out))
}

// remoteWhich finds the homebutler binary path on the remote server.
func remoteWhich(client *ssh.Client) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput("which homebutler 2>/dev/null || echo $HOME/.local/bin/homebutler")
	if err != nil {
		return "", fmt.Errorf("cannot locate homebutler")
	}

	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("cannot locate homebutler")
	}
	return path, nil
}
