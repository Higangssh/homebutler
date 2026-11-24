package doctor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/system"
)

// updatesTimeout bounds each package manager call. apt-get --simulate takes a
// couple of seconds on a Raspberry Pi; anything past this is a package
// manager that is stuck, which doctor should not be stuck behind.
const updatesTimeout = 30 * time.Second

// staleCacheAge is when a package list is old enough that its update count
// says more about the day it was refreshed than about today.
const staleCacheAge = 7 * 24 * time.Hour

// UpdateState is what the package manager already knows about pending
// updates and a pending reboot, read without root and without the network.
type UpdateState struct {
	// NotRun names the system the check does not apply to, such as "macOS"
	// or "Alpine Linux v3.20". Empty means the check ran.
	NotRun string
	// InContainer is set with NotRun when the system is a container image,
	// whose packages are not the host's.
	InContainer bool

	// Manager is "apt" or "dnf".
	Manager string

	// RebootRequired is true when the system says it needs one. RebootKnown
	// is false when there was no way to ask.
	RebootRequired bool
	RebootKnown    bool
	// RebootSince is when the flag appeared, where the system records it.
	RebootSince time.Time
	// RebootPackages are the packages that asked for it, where known.
	RebootPackages []string
	// RebootCommand shows the same answer by hand.
	RebootCommand string

	// Upgradable is the number of packages with a newer version in the
	// cache, and Security how many of those come from a security pocket;
	// Security is -1 when the manager could not say. UpgradableErr is set
	// when the count could not be read.
	Upgradable    int
	Security      int
	UpgradableErr error
	// UpgradableCommand lists the same packages by hand.
	UpgradableCommand string

	// CacheAge is how long ago the package lists were last refreshed, or zero
	// when that could not be told.
	CacheAge time.Duration
}

// updateProbe holds everything the probe touches, so a test can be a Debian
// machine with a pending reboot without being one.
type updateProbe struct {
	goos        string
	inContainer bool
	root        string // prefix for every absolute path read
	lookPath    func(string) (string, error)
	// run returns stdout and the exit code. err is set only when the command
	// could not be run or was killed, not for a non-zero exit.
	run func(ctx context.Context, name string, args ...string) ([]byte, int, error)
	now time.Time
}

func defaultUpdateProbe() updateProbe {
	return updateProbe{
		goos:        runtime.GOOS,
		inContainer: system.InContainer(),
		lookPath:    exec.LookPath,
		run:         runCommand,
		now:         time.Now(),
	}
}

// readUpdates is the default UpdatesFn.
func readUpdates() UpdateState {
	return defaultUpdateProbe().read()
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, updatesTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// The output is parsed, so it has to be the untranslated one.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return out, exit.ExitCode(), nil
	}
	if err != nil {
		return out, -1, err
	}
	return out, 0, nil
}

func (p updateProbe) path(abs string) string {
	return filepath.Join(p.root, abs)
}

func (p updateProbe) has(name string) bool {
	_, err := p.lookPath(name)
	return err == nil
}

func (p updateProbe) read() UpdateState {
	switch {
	case p.goos == "darwin":
		return UpdateState{NotRun: "macOS"}
	case p.goos == "windows":
		return UpdateState{NotRun: "Windows"}
	case p.goos != "linux":
		return UpdateState{NotRun: p.goos}
	case p.inContainer:
		// The package database in here is the image's, not the host's, and
		// answering for the host with it would be answering for the wrong
		// machine.
		return UpdateState{NotRun: "the container image", InContainer: true}
	case p.has("apt-get"):
		return p.readApt()
	case p.has("dnf"):
		return p.readDNF()
	}
	return UpdateState{NotRun: p.distribution()}
}

// distribution names a Linux system by its os-release PRETTY_NAME, such as
// "Alpine Linux v3.20" or "Arch Linux", falling back to "this Linux system".
func (p updateProbe) distribution() string {
	for _, abs := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := os.ReadFile(p.path(abs))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				if name := strings.Trim(strings.TrimSpace(value), `"'`); name != "" {
					return name
				}
			}
		}
	}
	return "this Linux system"
}

// readApt reads Debian and Ubuntu. reboot-required is written by the package
// scripts that need one, and apt-get --simulate reads the lists apt already
// has: it needs no root, takes no lock, and fetches nothing.
func (p updateProbe) readApt() UpdateState {
	s := UpdateState{Manager: "apt", Security: -1, RebootKnown: true}

	flag := p.path("/var/run/reboot-required")
	if info, err := os.Stat(flag); err == nil {
		s.RebootRequired = true
		s.RebootSince = info.ModTime()
		s.RebootPackages = readLines(p.path("/var/run/reboot-required.pkgs"))
		s.RebootCommand = "cat /var/run/reboot-required.pkgs"
	}

	s.UpgradableCommand = "apt list --upgradable"
	out, code, err := p.run(context.Background(), "apt-get", "--simulate", "--quiet", "dist-upgrade")
	switch {
	case err != nil:
		s.UpgradableErr = err
	case code != 0:
		s.UpgradableErr = fmt.Errorf("apt-get --simulate dist-upgrade exited %d", code)
	default:
		s.Upgradable, s.Security = countAptUpgrades(out)
	}

	s.CacheAge = p.age(
		"/var/lib/apt/periodic/update-success-stamp",
		"/var/lib/apt/lists/partial",
		"/var/lib/apt/lists",
	)
	return s
}

// countAptUpgrades counts the Inst lines of a simulated dist-upgrade that
// replace an installed version. A line with no [old version] is a package
// that would be newly installed as a dependency, which is not an update.
func countAptUpgrades(out []byte) (upgradable, security int) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.HasPrefix(fields[2], "[") {
			continue
		}
		upgradable++
		// Debian spells it Debian-Security:12/stable-security, Ubuntu
		// Ubuntu:24.04/noble-security; both end the suite in -security.
		if open := strings.Index(line, "("); open >= 0 && strings.Contains(line[open:], "-security") {
			security++
		}
	}
	return upgradable, security
}

// readDNF reads Fedora and RHEL. -C keeps dnf on the cache it already has.
func (p updateProbe) readDNF() UpdateState {
	s := UpdateState{Manager: "dnf", Security: -1}

	if p.has("needs-restarting") {
		_, code, err := p.run(context.Background(), "needs-restarting", "-r")
		if err == nil && (code == 0 || code == 1) {
			s.RebootKnown = true
			s.RebootRequired = code == 1
			s.RebootCommand = "needs-restarting -r"
		}
	}

	s.UpgradableCommand = "dnf -C check-update"
	out, code, err := p.run(context.Background(), "dnf", "-C", "-q", "check-update")
	switch {
	case err != nil:
		s.UpgradableErr = err
	case code == 0:
		s.Upgradable = 0
	case code == 100:
		s.Upgradable = countDNFUpgrades(out)
	default:
		s.UpgradableErr = errors.New("dnf check-update could not read the cache")
	}
	if s.UpgradableErr == nil && s.Upgradable > 0 {
		if out, code, err := p.run(context.Background(), "dnf", "-C", "-q", "updateinfo", "list", "--security", "--updates"); err == nil && code == 0 {
			s.Security = countDNFSecurity(out)
		}
	}

	s.CacheAge = p.newestUnder("/var/cache/dnf", "repomd.xml")
	return s
}

// countDNFUpgrades counts package lines of check-update, which are
// name.arch version repo, and stops at the "Obsoleting Packages" section.
func countDNFUpgrades(out []byte) int {
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Obsoleting") {
			break
		}
		if len(strings.Fields(line)) == 3 && !strings.HasPrefix(line, " ") {
			n++
		}
	}
	return n
}

// countDNFSecurity counts distinct packages in updateinfo's advisory, type,
// package lines; one package can appear under several advisories.
func countDNFSecurity(out []byte) int {
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 3 {
			seen[fields[len(fields)-1]] = true
		}
	}
	return len(seen)
}

// age is how long ago the newest of paths was modified.
func (p updateProbe) age(paths ...string) time.Duration {
	var newest time.Time
	for _, abs := range paths {
		if info, err := os.Stat(p.path(abs)); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return 0
	}
	return p.now.Sub(newest)
}

// newestUnder is age for the newest file called name anywhere under dir.
func (p updateProbe) newestUnder(dir, name string) time.Duration {
	var newest time.Time
	_ = filepath.WalkDir(p.path(dir), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != name {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if newest.IsZero() {
		return 0
	}
	return p.now.Sub(newest)
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

// listAge says how old a package list is to the precision that matters:
// hours for the first two days, days after that.
func listAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
