package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

const deployTestConfig = `servers:
  - name: local
    host: 127.0.0.1
    local: true
  - name: nas
    host: 192.0.2.10
  - name: rpi5
    host: 192.0.2.11
`

// runFleetCmd runs a deploy or upgrade command against a config with two
// remote servers, returning what it wrote to stdout and the error main would
// turn into the exit code.
func runFleetCmd(t *testing.T, cmd *cobra.Command, all bool, server string, json bool) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "homebutler.yaml")
	if err := os.WriteFile(path, []byte(deployTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	oldPath, oldJSON, oldCfg, oldAll, oldServer := cfgPath, jsonOutput, cfg, allServers, serverName
	oldFetch := fetchLatestVersion
	t.Cleanup(func() {
		cfgPath, jsonOutput, cfg, allServers, serverName = oldPath, oldJSON, oldCfg, oldAll, oldServer
		fetchLatestVersion = oldFetch
	})
	cfgPath, jsonOutput, cfg, allServers, serverName = path, json, nil, all, server
	fetchLatestVersion = func() (string, error) { return "9.9.9", nil }

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	cmd.SetArgs([]string{})
	runErr := cmd.Execute()
	os.Stdout = saved
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func fakeDeploy(t *testing.T, fn func(*config.ServerConfig, string, string) (*remote.DeployResult, error)) {
	t.Helper()
	old := deployToServer
	t.Cleanup(func() { deployToServer = old })
	deployToServer = fn
}

// deploy printed a ✗ for a server it could not reach and then exited 0, so a
// script that deployed and went on to use the server never noticed.
func TestDeployExitsNonZeroWhenAServerFails(t *testing.T) {
	var asked []string
	fakeDeploy(t, func(s *config.ServerConfig, _, _ string) (*remote.DeployResult, error) {
		asked = append(asked, s.Name)
		if s.Name == "rpi5" {
			return nil, errors.New("ssh connect to rpi5: connection refused")
		}
		return &remote.DeployResult{Server: s.Name, Arch: "linux/arm64", Source: "github", Status: "ok", Message: "installed"}, nil
	})

	out, err := runFleetCmd(t, newDeployCmd(), true, "", false)
	if err == nil {
		t.Fatal("rpi5 failed and deploy --all exited 0")
	}
	if got, want := err.Error(), "deploy failed on 1 of 2 servers: rpi5"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if len(asked) != 2 {
		t.Errorf("a failure must not stop the others: asked %v", asked)
	}

	// The JSON on stdout is still every server's result, in the same shape.
	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("stdout is not the results array: %v\n%s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("want both servers in the output, got %d", len(results))
	}
	if results[0]["server"] != "nas" || results[0]["status"] != "ok" {
		t.Errorf("nas result = %v", results[0])
	}
	if results[1]["server"] != "rpi5" || results[1]["status"] != "error" || results[1]["message"] != "ssh connect to rpi5: connection refused" {
		t.Errorf("rpi5 result = %v", results[1])
	}
}

// Deploy reports a binary that was uploaded but would not run as a result
// with status "error" rather than as an error, and that was printed with a ✓.
func TestDeployCountsAFailedVerificationAsAFailure(t *testing.T) {
	fakeDeploy(t, func(s *config.ServerConfig, _, _ string) (*remote.DeployResult, error) {
		return &remote.DeployResult{Server: s.Name, Status: "error", Message: "uploaded but verification failed: exit 126"}, nil
	})

	_, err := runFleetCmd(t, newDeployCmd(), false, "rpi5", false)
	if err == nil {
		t.Fatal("the binary on rpi5 did not run and deploy exited 0")
	}
	if got, want := err.Error(), "deploy failed on 1 of 1 server: rpi5"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestDeployExitsCleanWhenEveryServerTakesIt(t *testing.T) {
	fakeDeploy(t, func(s *config.ServerConfig, _, _ string) (*remote.DeployResult, error) {
		return &remote.DeployResult{Server: s.Name, Status: "ok", Message: "installed"}, nil
	})
	if _, err := runFleetCmd(t, newDeployCmd(), true, "", false); err != nil {
		t.Fatalf("every server took the binary, got %v", err)
	}
}

func fakeUpgrade(t *testing.T, local *remote.UpgradeResult, remotes map[string]*remote.UpgradeResult) {
	t.Helper()
	oldSelf, oldRemote := selfUpgrade, remoteUpgrade
	t.Cleanup(func() { selfUpgrade, remoteUpgrade = oldSelf, oldRemote })
	selfUpgrade = func(_, _ string) *remote.UpgradeResult { return local }
	remoteUpgrade = func(s *config.ServerConfig, _ string) *remote.UpgradeResult { return remotes[s.Name] }
}

// upgrade counted "1 failed" in its summary and still exited 0.
func TestUpgradeExitsNonZeroWhenATargetFails(t *testing.T) {
	fakeUpgrade(t,
		&remote.UpgradeResult{Target: "local", Status: "up-to-date", Message: "already v9.9.9"},
		map[string]*remote.UpgradeResult{
			"nas":  {Target: "nas", Status: "upgraded", Message: "v9.9.8 → v9.9.9"},
			"rpi5": {Target: "rpi5", Status: "error", Message: "ssh connect: connection refused"},
		})

	out, err := runFleetCmd(t, newUpgradeCmd(), false, "", true)
	if err == nil {
		t.Fatal("rpi5 failed and upgrade exited 0")
	}
	if got, want := err.Error(), "upgrade failed on 1 of 3 targets: rpi5"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	var report remote.UpgradeReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json must still print the report: %v\n%s", err, out)
	}
	if len(report.Results) != 3 || report.LatestVersion != "9.9.9" {
		t.Errorf("report = %+v", report)
	}
}

func TestUpgradeExitsCleanWhenNothingFailed(t *testing.T) {
	fakeUpgrade(t,
		&remote.UpgradeResult{Target: "local", Status: "up-to-date"},
		map[string]*remote.UpgradeResult{
			"nas":  {Target: "nas", Status: "upgraded"},
			"rpi5": {Target: "rpi5", Status: "up-to-date"},
		})
	if _, err := runFleetCmd(t, newUpgradeCmd(), false, "", false); err != nil {
		t.Fatalf("up to date is not a failure, got %v", err)
	}
}
