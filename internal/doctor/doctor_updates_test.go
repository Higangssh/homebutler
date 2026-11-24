package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
)

// Lines as apt-get --simulate dist-upgrade prints them on Ubuntu 24.04 and
// Debian 12: two security upgrades, one ordinary upgrade, one from a
// repository whose name has "Security" in it and whose suite is not a
// security one, one package newly pulled in as a dependency (no [old
// version], so not an update), a Conf line for each, and a package kept back.
const aptSimulation = `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
Reading package lists...
Calculating upgrade...
The following packages have been kept back:
  linux-generic linux-headers-generic
The following upgrades have been deferred due to phasing:
  systemd
The following packages will be upgraded:
  docker-ce libssl3t64 openssl tools
Inst libssl3t64 [3.0.13-0ubuntu3.5] (3.0.13-0ubuntu3.6 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst openssl [3.0.15-1~deb12u1] (3.0.16-1~deb12u1 Debian-Security:12/stable-security [arm64])
Inst docker-ce [5:29.9.3-1~ubuntu.24.04~noble] (5:29.10.0-1~ubuntu.24.04~noble Docker CE:noble [arm64])
Inst tools [1.0] (1.1 Acme-Security-Tools:stable/main-securityfixes [arm64])
Inst linux-image-6.8.0-1066-raspi (6.8.0-1066.70 Ubuntu:24.04/noble-updates [arm64])
Conf libssl3t64 (3.0.13-0ubuntu3.6 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Conf docker-ce (5:29.10.0-1~ubuntu.24.04~noble Docker CE:noble [arm64])
4 upgraded, 1 newly installed, 0 to remove and 3 not upgraded.
`

// dnf5 5.2.17 on Fedora 41, captured in a fedora:41 container: check-update,
// and updateinfo with its Name Type Severity Package Issued header and an
// issue time in two fields. The fourth updateinfo line is added by hand: xz
// under a second advisory, which counts once.
const (
	dnf5CheckUpdate = "xz.aarch64      1:5.8.1-4.fc41 updates\nxz-libs.aarch64 1:5.8.1-4.fc41 updates\n"
	dnf5Updateinfo  = `Name                   Type     Severity                               Package              Issued
FEDORA-2025-051becf4f2 security Low                  xz-1:5.8.1-2.fc41.aarch64 2025-05-10 01:58:21
FEDORA-2025-051becf4f2 security Low             xz-libs-1:5.8.1-2.fc41.aarch64 2025-05-10 01:58:21
FEDORA-2025-29c6186ffb security Important        sudo-1.9.17-2.p1.fc41.aarch64 2025-07-10 16:29:02
FEDORA-2025-7d1e0c2a11 security Moderate             xz-1:5.8.1-2.fc41.aarch64 2025-06-01 10:00:00
`
	// dnf 4.14 on Rocky Linux 9, captured the same way, as a user that is
	// not root: a leading blank line, and advisory, type, package.
	dnf4CheckUpdate = "\ncurl-minimal.aarch64                7.76.1-44.el9_9.1                     baseos\nexpat.aarch64                       2.5.0-9.el9_9.8                       baseos\n"
	dnf4Updateinfo  = `RLSA-2026:100686 Important/Sec. curl-minimal-7.76.1-44.el9_9.1.aarch64
RLSA-2026:100679 Important/Sec. expat-2.5.0-9.el9_9.8.aarch64
RLSA-2026:100686 Important/Sec. libcurl-minimal-7.76.1-44.el9_9.1.aarch64
`
)

type fakeCommand struct {
	out  string
	code int
	err  error
}

// debianProbe is a Linux machine with the given tools, its filesystem under a
// temp directory, answering commands from table.
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
	touch(t, path, mtime)
}

func touch(t *testing.T, path string, mtime time.Time) {
	t.Helper()
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

// Only a suite that ends in -security counts: a repository with the word in
// its name, or a suite that merely starts with it, does not.
func TestCountAptUpgrades(t *testing.T) {
	upgradable, security := countAptUpgrades([]byte(aptSimulation))
	if upgradable != 4 || security != 2 {
		t.Fatalf("countAptUpgrades = %d upgradable, %d security; want 4, 2", upgradable, security)
	}
}

func TestAptHeldBackPackagesAreNamed(t *testing.T) {
	got := strings.Join(aptHeldBack([]byte(aptSimulation)), " ")
	if got != "linux-generic linux-headers-generic systemd" {
		t.Fatalf("held = %q", got)
	}
	r := &Result{}
	checkUpdates(r, UpdateState{Manager: "apt", RebootKnown: true, Security: 0, Upgradable: 1, Held: aptHeldBack([]byte(aptSimulation)), CacheAge: time.Hour})
	if len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Detail, "3 more held back and not counted") ||
		!strings.Contains(r.Findings[0].Detail, "linux-generic, linux-headers-generic, systemd") {
		t.Fatalf("findings = %+v", r.Findings)
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
	if s.NotRun != "" || !s.RebootRequired || !s.RebootSince.Equal(since) || len(s.Held) != 3 {
		t.Fatalf("state = %+v", s)
	}

	r := &Result{}
	checkUpdates(r, s)

	reboot := findingTitled(r, "A reboot is pending")
	if reboot == nil || reboot.Severity != SeverityWarn {
		t.Fatalf("no reboot warning: %+v", r.Findings)
	}
	if !strings.Contains(reboot.Detail, "linux-image-6.8.0-1065-raspi, linux-base.") {
		t.Errorf("reboot detail = %q", reboot.Detail)
	}
	if reboot.Command != "cat /var/run/reboot-required.pkgs" || reboot.Runner != RunnerShell {
		t.Errorf("reboot command = %q (%s)", reboot.Command, reboot.Runner)
	}

	sec := findingTitled(r, "2 security update(s) pending")
	if sec == nil || sec.Severity != SeverityWarn {
		t.Fatalf("no security update warning: %+v", r.Findings)
	}
	if !strings.Contains(sec.Detail, "2 of 4") || !strings.Contains(sec.Detail, "refreshed 20h ago.") {
		t.Errorf("security detail = %q", sec.Detail)
	}
}

// The date is the one the machine shows: a flag written at 06:12 in Seoul
// is the 1st there and still the 30th in UTC.
func TestRebootPendingSinceIsTheLocalDate(t *testing.T) {
	seoul := time.FixedZone("KST", 9*3600)
	r := &Result{}
	checkUpdates(r, UpdateState{Manager: "apt", RebootKnown: true, RebootRequired: true,
		RebootSince: time.Date(2026, 5, 1, 6, 12, 0, 0, seoul), CacheAge: time.Hour})
	if f := findingTitled(r, "A reboot is pending"); f == nil || !strings.HasPrefix(f.Detail, "Pending since 2026-05-01.") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

func TestNoRebootAndOrdinaryUpdatesPass(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {out: "Inst docker-ce [1] (2 Docker CE:noble [arm64])\n"},
	}, "apt-get")
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp"), "", fixedNow.Add(-time.Hour))

	r := &Result{}
	checkUpdates(r, p.read())
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityPass ||
		r.Findings[0].Title != "1 package update(s) available, none from a security repository" {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// A failed apt update still touches lists/partial. The success stamp is what
// says when the lists were last refreshed, and partial must not outvote it.
func TestFailedAptUpdateDoesNotMakeTheListLookFresh(t *testing.T) {
	p := debianProbe(t, fixedNow, nil)
	month := fixedNow.Add(-30 * 24 * time.Hour)
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp"), "", month)
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/lists/partial/x"), "", fixedNow.Add(-time.Minute))
	touch(t, filepath.Join(p.root, "/var/lib/apt/lists/partial"), fixedNow.Add(-time.Minute))
	touch(t, filepath.Join(p.root, "/var/lib/apt/lists"), fixedNow.Add(-time.Minute))

	if age := p.aptListAge(); age != 30*24*time.Hour {
		t.Fatalf("list age = %s, want the stamp's 720h", age)
	}

	// Without the stamp, the lists directory answers, and partial still
	// does not.
	if err := os.Remove(filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp")); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(p.root, "/var/lib/apt/lists"), month)
	if age := p.aptListAge(); age != 30*24*time.Hour {
		t.Fatalf("list age without a stamp = %s, want the lists directory's 720h", age)
	}
}

// A count from a list nobody has refreshed in a month says so in its own
// detail. It is not a separate warning: nothing about it is a fault doctor
// should turn --strict red for.
func TestStalePackageListIsSaidInTheCount(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"apt-get --simulate --quiet dist-upgrade": {out: ""},
	}, "apt-get")
	writeAt(t, filepath.Join(p.root, "/var/lib/apt/periodic/update-success-stamp"), "", fixedNow.Add(-30*24*time.Hour))

	r := &Result{}
	checkUpdates(r, p.read())
	if len(r.Findings) != 1 {
		t.Fatalf("a stale list produced more than the count: %+v", r.Findings)
	}
	f := r.Findings[0]
	if f.Severity != SeverityPass || f.Title != "No pending package updates" ||
		!strings.Contains(f.Detail, "refreshed 30d ago, so it may be missing everything released since") {
		t.Fatalf("finding = %+v", f)
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

func withDNFCache(t *testing.T, p updateProbe, dir string, age time.Duration) {
	t.Helper()
	writeAt(t, filepath.Join(p.root, dir, "updates-1234/repodata/repomd.xml"), "<repomd/>", p.now.Add(-age))
}

func TestCountDNFSecurityReadsBothDNFs(t *testing.T) {
	if n := countDNFSecurity([]byte(dnf5Updateinfo)); n != 3 {
		t.Errorf("dnf5 updateinfo = %d packages, want 3 (and the header not counted)", n)
	}
	if n := countDNFSecurity([]byte(dnf4Updateinfo)); n != 3 {
		t.Errorf("dnf4 updateinfo = %d packages, want 3", n)
	}
	if n := countDNFUpgrades([]byte(dnf5CheckUpdate)); n != 2 {
		t.Errorf("dnf5 check-update = %d, want 2", n)
	}
	if n := countDNFUpgrades([]byte(dnf4CheckUpdate)); n != 2 {
		t.Errorf("dnf4 check-update = %d, want 2", n)
	}
}

func TestFedoraRebootAndUpdates(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"needs-restarting -r":                            {out: "Reboot is required to fully utilize these updates.\n", code: 1},
		"dnf -C -q check-update":                         {out: dnf4CheckUpdate, code: 100},
		"dnf -C -q updateinfo list --security --updates": {out: dnf4Updateinfo},
	}, "dnf", "needs-restarting")
	withDNFCache(t, p, "/var/cache/dnf", 3*time.Hour)

	s := p.read()
	if s.Manager != "dnf" || !s.RebootKnown || !s.RebootRequired || s.Upgradable != 2 || s.Security != 3 || s.CacheAge != 3*time.Hour {
		t.Fatalf("state = %+v", s)
	}
	r := &Result{}
	checkUpdates(r, s)
	if f := findingTitled(r, "A reboot is pending"); f == nil || f.Command != "needs-restarting -r" {
		t.Fatalf("findings = %+v", r.Findings)
	}
	if findingTitled(r, "3 security update(s) pending") == nil {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// dnf5 as root reads its own cache in /var/cache/libdnf5.
func TestDNF5AsRootCountsFromLibdnf5(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"dnf -C -q check-update":                         {out: dnf5CheckUpdate, code: 100},
		"dnf -C -q updateinfo list --security --updates": {out: dnf5Updateinfo},
	}, "dnf", "dnf5")
	p.asRoot = true
	withDNFCache(t, p, "/var/cache/libdnf5", 2*time.Hour)

	s := p.read()
	if s.NotCounted != "" || s.Upgradable != 2 || s.Security != 3 || s.CacheAge != 2*time.Hour {
		t.Fatalf("state = %+v", s)
	}
}

// dnf5 as a user that is not root copies its whole cache into
// ~/.cache/libdnf5 before it answers, so it is not run; the finding says why.
func TestDNF5AsAUserIsNotRun(t *testing.T) {
	p := debianProbe(t, fixedNow, nil, "dnf", "dnf5")
	withDNFCache(t, p, "/var/cache/libdnf5", time.Hour)

	r := &Result{}
	checkUpdates(r, p.read())
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityPass || r.Findings[0].Title != "Pending updates were not counted" ||
		!strings.Contains(r.Findings[0].Detail, "~/.cache/libdnf5") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// A fresh system with no dnf cache has nothing to count from, which is not
// something anyone has to clear.
func TestEmptyDNFCacheIsAPass(t *testing.T) {
	p := debianProbe(t, fixedNow, nil, "dnf")
	r := &Result{}
	checkUpdates(r, p.read())
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityPass || !strings.Contains(r.Findings[0].Detail, "the package cache is empty") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// An exit code dnf does not document for check-update is a failure, and is
// said as one.
func TestUnknownDNFExitIsAFailure(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"dnf -C -q check-update": {code: 2},
	}, "dnf")
	withDNFCache(t, p, "/var/cache/dnf", time.Hour)
	r := &Result{}
	checkUpdates(r, p.read())
	f := findingTitled(r, "Could not count pending updates")
	if f == nil || f.Severity != SeverityWarn || !strings.Contains(f.Detail, "exited 2") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// Without needs-restarting there is no way to ask, and the finding says so
// rather than implying no reboot is needed.
func TestFedoraWithoutNeedsRestartingSaysTheRebootWasNotChecked(t *testing.T) {
	p := debianProbe(t, fixedNow, map[string]fakeCommand{
		"dnf -C -q check-update": {code: 0},
	}, "dnf")
	withDNFCache(t, p, "/var/cache/dnf", time.Hour)
	r := &Result{}
	checkUpdates(r, p.read())
	f := findingTitled(r, "No pending package updates")
	if f == nil || !strings.Contains(f.Detail, "whether a reboot is pending was not checked") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// runCommand is what readUpdates runs with: it has to pass LC_ALL=C, so the
// output it parses is the untranslated one, and keep a non-zero exit code as
// an answer rather than an error.
func TestRunCommandPassesCLocaleAndKeepsTheExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"LC_ALL=$LC_ALL\"\necho 'Inst openssl [1] (2 Debian-Security:12/stable-security [arm64])'\nexit 7\n"
	if err := os.WriteFile(filepath.Join(dir, "apt-get"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LC_ALL", "ko_KR.UTF-8")

	out, code, err := runCommand(context.Background(), "apt-get", "--simulate")
	if err != nil || code != 7 || !strings.Contains(string(out), "LC_ALL=C\n") {
		t.Fatalf("runCommand = %q, %d, %v", out, code, err)
	}

	// And through the real probe: a non-zero exit is a failure to count.
	p := updateProbe{goos: "linux", root: t.TempDir(), lookPath: exec.LookPath, run: runCommand, now: fixedNow}
	if s := p.read(); s.Manager != "apt" || s.UpgradableErr == nil || !strings.Contains(s.UpgradableErr.Error(), "exited 7") {
		t.Fatalf("state = %+v", s)
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
