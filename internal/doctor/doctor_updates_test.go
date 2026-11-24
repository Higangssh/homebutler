package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
)

// Lines as apt-get --simulate dist-upgrade prints them on Ubuntu 24.04 and
// Debian 12: two security upgrades, one ordinary upgrade, one package newly
// pulled in as a dependency (no [old version], so not an update), and a Conf
// line for each.
const aptSimulation = `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
Reading package lists...
Inst libssl3t64 [3.0.13-0ubuntu3.5] (3.0.13-0ubuntu3.6 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst openssl [3.0.15-1~deb12u1] (3.0.16-1~deb12u1 Debian-Security:12/stable-security [arm64])
Inst docker-ce [5:29.9.3-1~ubuntu.24.04~noble] (5:29.10.0-1~ubuntu.24.04~noble Docker CE:noble [arm64])
Inst linux-image-6.8.0-1066-raspi (6.8.0-1066.70 Ubuntu:24.04/noble-updates [arm64])
Conf libssl3t64 (3.0.13-0ubuntu3.6 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Conf docker-ce (5:29.10.0-1~ubuntu.24.04~noble Docker CE:noble [arm64])
`

type fakeCommand struct {
	out  string
	code int
	err  error
}

// debianProbe is a Linux machine with apt-get, its filesystem under a temp
// directory, answering commands from table.
func debianProbe(t *testing.T, now time.Time, table map[string]fakeCommand, tools ...string) updateProbe {
	t.Helper()
	have := map[string]bool{}
	for _, tool := range tools {
		have[tool] = true
	}
	return updateProbe{
		goos: "linux",
		root: t.TempDir(),
		lookPath: func(name string) (string, error) {
			if have[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, int, error) {
			c, ok := table[strings.Join(append([]string{name}, args...), " ")]
			if !ok {
				t.Errorf("ran %s %v, which the test did not expect", name, args)
				return nil, -1, errors.New("unexpected")
			}
			return []byte(c.out), c.code, c.err
		},
		now: now,
	}
}

func writeAt(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func findingTitled(r *Result, prefix string) *Finding {
	for i := range r.Findings {
		if strings.HasPrefix(r.Findings[i].Title, prefix) {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestCountAptUpgrades(t *testing.T) {
	upgradable, security := countAptUpgrades([]byte(aptSimulation))
	if upgradable != 3 || security != 2 {
		t.Fatalf("countAptUpgrades = %d upgradable, %d security; want 3, 2", upgradable, security)
	}
}

func TestPendingRebootAndSecurityUpdatesOnDebian(t *testing.T) {
	now := fixedNow
	p := debianProbe(t, now, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {out: aptSimulation},
	}, "apt-get")
	since := now.Add(-9 * 24 * time.Hour)
	writeAt(t, filepath.Join(p.root, "/var/run/reboot-required"), "*** System restart required ***\n", since)
	writeAt(t, filepath.Join(p.root, "/var/run/reboot-required.pkgs"), "linux-image-6.8.0-1065-raspi\nlinux-base\nlinux-base\n", since)
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp"), "", now.Add(-20*time.Hour))

	s := p.read()
	if s.NotRun != "" || !s.RebootRequired || !s.RebootSince.Equal(since) {
		t.Fatalf("state = %+v", s)
	}

	r := &Result{}
	checkUpdates(r, s)

	reboot := findingTitled(r, "A reboot is pending")
	if reboot == nil || reboot.Severity != SeverityWarn {
		t.Fatalf("no reboot warning: %+v", r.Findings)
	}
	if !strings.Contains(reboot.Detail, "Pending since 2026-05-01") ||
		!strings.Contains(reboot.Detail, "linux-image-6.8.0-1065-raspi, linux-base.") {
		t.Errorf("reboot detail = %q", reboot.Detail)
	}
	if reboot.Command != "cat /var/run/reboot-required.pkgs" || reboot.Runner != RunnerShell {
		t.Errorf("reboot command = %q (%s)", reboot.Command, reboot.Runner)
	}

	sec := findingTitled(r, "2 security update(s) pending")
	if sec == nil || sec.Severity != SeverityWarn {
		t.Fatalf("no security update warning: %+v", r.Findings)
	}
	if !strings.Contains(sec.Detail, "2 of 3") || !strings.Contains(sec.Detail, "refreshed 20h ago") {
		t.Errorf("security detail = %q", sec.Detail)
	}
	if findingTitled(r, "The package list is") != nil {
		t.Errorf("a list refreshed 20h ago was called stale: %+v", r.Findings)
	}
}

func TestNoRebootAndOrdinaryUpdatesPass(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {out: "Inst docker-ce [1] (2 Docker CE:noble [arm64])\n"},
	}, "apt-get")
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/lists/partial/x"), "", fixedNow.Add(-time.Hour))

	r := &Result{}
	checkUpdates(r, p.read())
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityPass ||
		r.Findings[0].Title != "1 package update(s) available, none from a security repository" {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// A count read from a list nobody has refreshed in a month is a count from a
// month ago, and saying "no updates" from it would be the reassuring lie.
func TestStalePackageListIsSaidToBeStale(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {out: ""},
	}, "apt-get")
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp"), "", fixedNow.Add(-30*24*time.Hour))
	if err := os.Chtimes(filepath.Join(p.root, "/var/lib/apt/periodic"), fixedNow.Add(-30*24*time.Hour), fixedNow.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	r := &Result{}
	checkUpdates(r, p.read())

	stale := findingTitled(r, "The package list is 30d old")
	if stale == nil || stale.Severity != SeverityWarn || stale.Command != "sudo apt update" {
		t.Fatalf("no stale warning: %+v", r.Findings)
	}
	none := findingTitled(r, "No pending package updates")
	if none == nil || !strings.Contains(none.Detail, "30d ago") {
		t.Errorf("the count does not say how old it is: %+v", r.Findings)
	}
}

func TestAptThatCannotSimulateIsNotCountedAsUpToDate(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {code: 100},
	}, "apt-get")

	r := &Result{}
	checkUpdates(r, p.read())
	if f := findingTitled(r, "Could not count pending updates"); f == nil || f.Severity != SeverityWarn {
		t.Fatalf("a failed simulation was not reported: %+v", r.Findings)
	}
	if findingTitled(r, "No pending") != nil {
		t.Errorf("a failed simulation was reported as up to date: %+v", r.Findings)
	}
}

func TestFedoraRebootAndUpdates(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"needs-restarting -r":                            {out: "Reboot is required to fully utilize these updates.\n", code: 1},
		"dnf -C -q check-update":                         {out: "\nkernel.x86_64    6.11.4-301.fc41    updates\nopenssl.x86_64    1:3.2.2-9.fc41    updates\n", code: 100},
		"dnf -C -q updateinfo list --security --updates": {out: "FEDORA-2024-1 Important/Sec. openssl-1:3.2.2-9.fc41.x86_64\n"},
	}, "dnf", "needs-restarting")

	s := p.read()
	if s.Manager != "dnf" || !s.RebootKnown || !s.RebootRequired || s.Upgradable != 2 || s.Security != 1 {
		t.Fatalf("state = %+v", s)
	}
	r := &Result{}
	checkUpdates(r, s)
	if f := findingTitled(r, "A reboot is pending"); f == nil || f.Command != "needs-restarting -r" {
		t.Fatalf("findings = %+v", r.Findings)
	}
	if findingTitled(r, "1 security update(s) pending") == nil {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// Without needs-restarting there is no way to ask, and the finding says so
// rather than implying no reboot is needed.
func TestFedoraWithoutNeedsRestartingSaysTheRebootWasNotChecked(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"dnf -C -q check-update": {code: 0},
	}, "dnf")
	r := &Result{}
	checkUpdates(r, p.read())
	f := findingTitled(r, "No pending package updates")
	if f == nil || !strings.Contains(f.Detail, "whether a reboot is pending was not checked") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// Where there is nothing to read, the finding says so by name and is a pass:
// nothing on that machine could clear a warning, and `doctor --strict` in a
// Mac cron job or the container image would exit 1 forever.
func TestUpdatesNotReadWhereThereIsNothingToRead(t *testing.T) {
	noTools := func(string) (string, error) { return "", errors.New("not found") }
	alpine := t.TempDir()
	writeAt(t, filepath.Join(alpine, "/etc/os-release"), "NAME=\"Alpine Linux\"\nPRETTY_NAME=\"Alpine Linux v3.20\"\n", fixedNow)

	cases := []struct {
		name   string
		probe  updateProbe
		title  string
		detail string
	}{
		{"macOS", updateProbe{goos: "darwin"}, "Pending updates are not read on macOS", "macOS has none of them"},
		{"Windows", updateProbe{goos: "windows"}, "Pending updates are not read on Windows", "Windows has none of them"},
		{"container", updateProbe{goos: "linux", inContainer: true}, "Pending updates are not read in the container image", "Check the host with --server"},
		{"Alpine", updateProbe{goos: "linux", root: alpine, lookPath: noTools}, "Pending updates are not read on Alpine Linux v3.20", "Alpine Linux v3.20 has none of them"},
		{"no os-release", updateProbe{goos: "linux", root: t.TempDir(), lookPath: noTools}, "Pending updates are not read on this Linux system", "this Linux system has none of them"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.probe
			p.run = func(context.Context, string, ...string) ([]byte, int, error) {
				t.Fatal("ran a command on a system the check does not apply to")
				return nil, 0, nil
			}
			r := &Result{}
			checkUpdates(r, p.read())
			if len(r.Findings) != 1 {
				t.Fatalf("findings = %+v", r.Findings)
			}
			f := r.Findings[0]
			if f.Severity != SeverityPass || f.Category != "system" || f.Title != tc.title {
				t.Fatalf("finding = %+v, want pass %q", f, tc.title)
			}
			if !strings.Contains(f.Detail, tc.detail) || !strings.Contains(f.Detail, "apt or dnf package cache") {
				t.Errorf("detail %q does not say what is read and that %q", f.Detail, tc.detail)
			}
			if f.Command != "" || f.Action != "" {
				t.Errorf("a check that does not apply offers a fix: %+v", f)
			}
		})
	}
}

// The reason it is a pass: on an otherwise clean Mac, doctor's status stays
// pass, which is what `--strict` exits on.
func TestStrictStaysGreenWhereUpdatesAreNotRead(t *testing.T) {
	fns := doctorFuncs(healthyStatus(), nil, nil, nil,
		[]backup.ListEntry{{Name: "backup.tar.gz", CreatedAt: fixedNow.Add(-time.Hour).Format(time.RFC3339)}})
	fns.SnapshotDir = t.TempDir()
	writeSnapshotMarker(t, fns.SnapshotDir)
	fns.UpdatesFn = func() UpdateState { return UpdateState{NotRun: "macOS"} }

	cfg := &config.Config{}
	cfg.Notify.Webhook = &notify.WebhookConfig{URL: "https://example.test/webhook"}
	drilled := backup.DrillRecord{App: "app", Archive: "backup.tar.gz", Passed: true,
		At: fixedNow.Add(-30 * time.Minute).UTC().Format(time.RFC3339)}

	r, err := Run(cfg, fns, Options{Now: fixedNow, DrillRecordsFn: staticDrills(drilled)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != SeverityPass {
		t.Fatalf("status = %s on a clean Mac, so --strict would exit 1: %+v", r.Status, r.Findings)
	}
}

// Run calls the probe it is given: a doctor run on a pending-reboot machine
// reports the reboot.
func TestRunReportsAPendingReboot(t *testing.T) {
	fns := doctorFuncs(healthyStatus(), nil, nil, nil, nil)
	fns.UpdatesFn = func() UpdateState {
		return UpdateState{Manager: "apt", RebootKnown: true, RebootRequired: true, RebootCommand: "cat /var/run/reboot-required.pkgs"}
	}
	r, err := Run(nil, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(r, "A reboot is pending") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}
