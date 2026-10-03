package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/util"
	"golang.org/x/crypto/ssh"
)

// UpgradeResult holds the result of an upgrade operation for a single target.
type UpgradeResult struct {
	Target      string `json:"target"`
	PrevVersion string `json:"prev_version"`
	NewVersion  string `json:"new_version"`
	Status      string `json:"status"` // "upgraded", "up-to-date", "error"
	Message     string `json:"message,omitempty"`
	Error       error  `json:"-"`
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

	if message, stop := upToDateMessage(currentVersion, latestVersion); stop {
		result.Status = "up-to-date"
		result.NewVersion = currentVersion
		result.Message = message
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
		if util.IsPermissionError(err) {
			result.Error = util.NewHintError("cannot backup current binary "+backupPath, err, "sudo homebutler upgrade")
			result.Message = util.FormatError(result.Error, true)
		} else {
			result.Message = fmt.Sprintf("cannot backup current binary: %v", err)
		}
		return result
	}

	if err := os.WriteFile(execPath, data, 0755); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, execPath)
		result.Status = "error"
		if util.IsPermissionError(err) {
			result.Error = util.NewHintError("cannot write new binary "+execPath, err, "sudo homebutler upgrade")
			result.Message = util.FormatError(result.Error, true)
		} else {
			result.Message = fmt.Sprintf("cannot write new binary: %v", err)
		}
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

	if message, stop := upToDateMessage(remoteVersion, latestVersion); stop {
		result.Status = "up-to-date"
		result.NewVersion = remoteVersion
		result.Message = message
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
		// deploy installs through sudo where the account has it, so since
		// #304 a remote binary can be root's, and upgrade writes as the login
		// user. Saying "Permission denied" and stopping leaves the reader to
		// work out that deploy is the command that can replace it.
		if strings.Contains(err.Error(), "Permission denied") {
			result.Message += fmt.Sprintf("\n  %s is not writable by %s; run `homebutler deploy --server %s` to replace it, which uses sudo where the account has it", installPath, server.SSHUser(), server.Name)
		}
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

// upToDateMessage reports the status line when the installed version should
// be left in place. stop is false when latest should be downloaded, including
// when either string is not a version (a dev build, a commit hash).
func upToDateMessage(current, latest string) (message string, stop bool) {
	order, ok := compareVersions(current, latest)
	if current == latest || (ok && order == 0) {
		return fmt.Sprintf("already v%s", current), true
	}
	if ok && order > 0 {
		return fmt.Sprintf("v%s is newer than v%s", current, latest), true
	}
	return "", false
}

// compareVersions compares an installed version with the latest release.
// order is positive when current is newer, negative when it is older, and
// zero when they are the same version. ok is false when either string is not
// MAJOR.MINOR.PATCH, with an optional leading v, an optional -prerelease,
// and an optional +build suffix that is ignored.
func compareVersions(current, latest string) (order int, ok bool) {
	cur, curOK := parseVersion(current)
	lat, latOK := parseVersion(latest)
	if !curOK || !latOK {
		return 0, false
	}
	if cur.major != lat.major {
		return cmpInt(cur.major, lat.major), true
	}
	if cur.minor != lat.minor {
		return cmpInt(cur.minor, lat.minor), true
	}
	if cur.patch != lat.patch {
		return cmpInt(cur.patch, lat.patch), true
	}
	return comparePrerelease(cur.pre, lat.pre), true
}

type parsedVersion struct {
	major, minor, patch int
	pre                 []string // nil when this is a release, not a prerelease
}

func parseVersion(raw string) (parsedVersion, bool) {
	s := strings.TrimPrefix(raw, "v")
	if cut, _, found := strings.Cut(s, "+"); found {
		s = cut
	}
	var preRaw string
	if cut, after, found := strings.Cut(s, "-"); found {
		s = cut
		preRaw = after
		if preRaw == "" {
			return parsedVersion{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return parsedVersion{}, false
	}
	nums := [3]int{}
	for i, part := range parts {
		if !isDigits(part) {
			return parsedVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return parsedVersion{}, false
		}
		nums[i] = n
	}
	if preRaw == "" {
		return parsedVersion{major: nums[0], minor: nums[1], patch: nums[2]}, true
	}
	ids := strings.Split(preRaw, ".")
	for _, id := range ids {
		if !validIdent(id) {
			return parsedVersion{}, false
		}
	}
	return parsedVersion{major: nums[0], minor: nums[1], patch: nums[2], pre: ids}, true
}

// comparePrerelease follows semver precedence. A release is newer than the
// same triple with a prerelease. A longer run of equal identifiers is newer.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

// compareIdent compares one prerelease identifier. Numeric identifiers compare
// as integers and sort before non-numeric ones; everything else is byte order.
func compareIdent(a, b string) int {
	aNum, bNum := isDigits(a), isDigits(b)
	if aNum && bNum {
		return compareNumeric(a, b)
	}
	if aNum {
		return -1
	}
	if bNum {
		return 1
	}
	return strings.Compare(a, b)
}

func compareNumeric(a, b string) int {
	a = trimLeadingZeros(a)
	b = trimLeadingZeros(b)
	if len(a) != len(b) {
		return cmpInt(len(a), len(b))
	}
	return strings.Compare(a, b)
}

func trimLeadingZeros(s string) string {
	i := 0
	for i+1 < len(s) && s[i] == '0' {
		i++
	}
	return s[i:]
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

func cmpInt(a, b int) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}
