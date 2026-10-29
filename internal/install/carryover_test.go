package install

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

// The compose file homebutler wrote for filebrowser before #327: only /srv is
// mapped, so /config and /database lived in anonymous volumes.
const legacyFilebrowserCompose = `services:
  filebrowser:
    image: filebrowser/filebrowser:latest
    container_name: filebrowser
    restart: unless-stopped
    ports:
      - "8081:80"
    volumes:
      - "/home/u/.homebutler/apps/filebrowser/data:/srv"
`

// What inspect prints with stateMountsFormat for the old container: it ran,
// and its state is in two anonymous volumes.
const legacyMounts = "2026-10-11T09:00:00.123456789Z\nbind /srv \nvolume /config cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\nvolume /database dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\n"

// fakeDocker answers docker commands from a script and records each call.
// A cp writes a file into its destination, the way the real one would, and
// volumes exist until they are removed.
type fakeDocker struct {
	calls    []string
	ids      string // what compose ps -a -q prints
	psErr    error
	mounts   string // what inspect prints for every container
	cpErr    map[string]error
	info     string // what docker info prints
	removed  map[string]bool
	rmRefuse map[string]bool // volumes rm fails on, as in use
}

func (f *fakeDocker) run(args ...string) (string, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case len(args) >= 4 && args[0] == "compose" && args[3] == "ps":
		return f.ids, f.psErr
	case args[0] == "info":
		return f.info, nil
	case args[0] == "inspect" && args[2] == volumeMountsFormat:
		// Purge's question: the names of the volume mounts only.
		var names []string
		for _, line := range strings.Split(f.mounts, "\n") {
			if fields := strings.Fields(line); len(fields) == 3 && fields[0] == "volume" {
				names = append(names, fields[2])
			}
		}
		return strings.Join(names, "\n"), nil
	case args[0] == "inspect":
		return f.mounts, nil
	case len(args) == 3 && args[0] == "volume" && args[1] == "rm":
		if f.rmRefuse[args[2]] {
			return "volume is in use", errors.New("exit status 1")
		}
		if f.removed == nil {
			f.removed = map[string]bool{}
		}
		f.removed[args[2]] = true
		return args[2], nil
	case len(args) == 3 && args[0] == "volume" && args[1] == "inspect":
		if f.removed[args[2]] {
			return "Error response from daemon: get " + args[2] + ": no such volume", errors.New("exit status 1")
		}
		return "[{}]", nil
	case args[0] == "cp":
		src, dst := args[1], args[2]
		path := src[strings.Index(src, ":")+1:]
		if err := f.cpErr[strings.TrimSuffix(path, "/.")]; err != nil {
			// A failed copy can leave part of what it copied behind.
			_ = os.WriteFile(filepath.Join(dst, "partial"), []byte("x"), 0o644)
			return "Error response from daemon: " + err.Error(), err
		}
		name := "settings.json"
		if strings.HasPrefix(path, "/database") {
			name = "filebrowser.db"
		}
		return "", os.WriteFile(filepath.Join(dst, name), []byte("old "+name), 0o644)
	}
	return "", nil
}

func (f *fakeDocker) saw(prefix string) bool { return f.index(prefix) >= 0 }

func (f *fakeDocker) index(prefix string) int {
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// withScriptedDocker points HOME at a temp dir, swaps the docker runner, and
// returns the filebrowser app dir and its compose command prefix.
func withScriptedDocker(t *testing.T, f *fakeDocker) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	orig := dockerCmd
	dockerCmd = f.run
	t.Cleanup(func() { dockerCmd = orig })
	appDir := AppDir("filebrowser")
	return appDir, "compose -f " + filepath.Join(appDir, "docker-compose.yml")
}

func writeLegacyInstall(t *testing.T, appDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(appDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte(legacyFilebrowserCompose), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestUninstallCopiesAnOldInstallsDatabaseOutBeforeDown(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	carried, err := Uninstall("filebrowser")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := readFile(t, filepath.Join(appDir, "database", "filebrowser.db")); got != "old filebrowser.db" {
		t.Errorf("database dir holds %q", got)
	}
	if got := readFile(t, filepath.Join(appDir, "config", "settings.json")); got != "old settings.json" {
		t.Errorf("config dir holds %q", got)
	}
	stop, cp, down := f.index(compose+" stop"), f.index("cp abc123:/database/."), f.index(compose+" down")
	if stop < 0 || stop > cp {
		t.Fatalf("the container was not stopped before the copy: %v", f.calls)
	}
	if cp < 0 || down < 0 || cp > down {
		t.Fatalf("the copy has to happen before down: %v", f.calls)
	}
	if strings.Join(carried.Copied, " ") != filepath.Join(appDir, "config")+" "+filepath.Join(appDir, "database") {
		t.Errorf("Copied = %v", carried.Copied)
	}
	if lines := strings.Join(carried.Lines("filebrowser", ""), "\n"); !strings.Contains(lines, filepath.Join(appDir, "database")) {
		t.Errorf("uninstall would not say where it copied the database: %q", lines)
	}
	// Uninstall keeps the volumes it copied out of; they are only recorded.
	if f.saw("volume rm") {
		t.Errorf("uninstall removed a volume: %v", f.calls)
	}
	if got := carriedVolumes(appDir); strings.Join(got, " ") != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" {
		t.Errorf("recorded volumes = %v, want [cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd]", got)
	}
}

func TestAFailedCopyStopsUninstallBeforeDown(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts, cpErr: map[string]error{"/database": errors.New("no space left on device")}}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	_, err := Uninstall("filebrowser")
	if err == nil {
		t.Fatal("Uninstall succeeded after the database copy failed")
	}
	if !strings.Contains(err.Error(), "/database") || !strings.Contains(err.Error(), "no space left on device") {
		t.Errorf("error does not say what failed: %v", err)
	}
	if f.saw(compose + " down") {
		t.Fatalf("down ran after a failed copy: %v", f.calls)
	}
	// What the failed copy left must not count as a copied database next
	// time, or a retry would skip it.
	entries, _ := os.ReadDir(filepath.Join(appDir, "database"))
	if len(entries) != 0 {
		t.Errorf("a failed copy left %d entries in the database dir", len(entries))
	}
}

func TestReinstallOverAStoppedOldContainerCopiesBeforeRewritingCompose(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	if _, err := Install(Registry["filebrowser"], InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readFile(t, filepath.Join(appDir, "database", "filebrowser.db")); got != "old filebrowser.db" {
		t.Errorf("database dir holds %q", got)
	}
	if cp, up := f.index("cp abc123:/database/."), f.index(compose+" up"); cp < 0 || up < 0 || cp > up {
		t.Fatalf("the copy has to happen before up: %v", f.calls)
	}
	if got := readFile(t, filepath.Join(appDir, "docker-compose.yml")); !strings.Contains(got, ":/database\"") {
		t.Errorf("the compose file was not rewritten:\n%s", got)
	}
}

// On main, purging a stopped old-style install removed its two anonymous
// volumes. Reinstalling with the new template first moves the state out and
// leaves those volumes attached to nothing, so the containers no longer name
// them, and a purge has to remove them from the record instead.
func TestPurgeAfterACarriedOverReinstallRemovesTheOldVolumes(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)
	if _, err := Install(Registry["filebrowser"], InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// The recreated container has only binds.
	f.mounts = "2026-10-29T09:00:00Z\nbind /srv \nbind /config \nbind /database \n"
	result, err := Purge("filebrowser")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	for _, v := range []string{"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"} {
		if !f.removed[v] {
			t.Errorf("purge left %s, which still holds the old install's state: %v", v, f.calls)
		}
	}
	if strings.Join(result.Removed, " ") != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" || !result.Complete() {
		t.Errorf("purge result = %+v, want cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc and dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd removed", result)
	}
}

func TestPurgeListsACarriedVolumeItCouldNotRemoveAsKept(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts, rmRefuse: map[string]bool{"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd": true}}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)
	if _, err := Uninstall("filebrowser"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	f.ids, f.mounts = "", ""
	result, err := Purge("filebrowser")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if strings.Join(result.Removed, " ") != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" || strings.Join(result.Kept, " ") != "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" || result.Complete() {
		t.Errorf("purge result = %+v, want cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc removed and dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd kept", result)
	}
}

// The record is a file in the app directory, and anything can end up in it.
// Purge only removes what reads as an anonymous volume's ID; a name in there
// could be someone's named volume, and is reported rather than removed.
func TestPurgeRemovesOnlyAnonymousVolumeIDsFromTheRecord(t *testing.T) {
	anon := strings.Repeat("ab", 32)
	f := &fakeDocker{}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)
	if err := os.WriteFile(filepath.Join(appDir, carriedVolumesFile), []byte(anon+"\npostgres-data\nmissing-named\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.removed = map[string]bool{"missing-named": true} // Docker has no such volume

	result, err := Purge("filebrowser")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	for _, name := range []string{"postgres-data", "missing-named"} {
		if f.saw("volume rm " + name) {
			t.Errorf("purge ran volume rm on %s, which is not an anonymous volume ID: %v", name, f.calls)
		}
	}
	if !f.saw("volume rm " + anon) {
		t.Errorf("purge did not remove the anonymous volume: %v", f.calls)
	}
	if strings.Join(result.Removed, " ") != anon {
		t.Errorf("Removed = %v, want only the anonymous one", result.Removed)
	}
	// Still there: kept. Not there, but purge did not remove it: unchecked,
	// never removed.
	if strings.Join(result.Kept, " ") != "postgres-data" || strings.Join(result.Unchecked, " ") != "missing-named" {
		t.Errorf("Kept = %v, Unchecked = %v", result.Kept, result.Unchecked)
	}
}

func TestAFailedCopyStopsInstallBeforeTheComposeFileIsRewritten(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts, cpErr: map[string]error{"/config": errors.New("permission denied")}}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	if _, err := Install(Registry["filebrowser"], InstallOptions{}); err == nil {
		t.Fatal("Install succeeded after the config copy failed")
	}
	if got := readFile(t, filepath.Join(appDir, "docker-compose.yml")); got != legacyFilebrowserCompose {
		t.Errorf("the old compose file was rewritten, which loses the only pointer to the container:\n%s", got)
	}
	if f.saw(compose + " up") {
		t.Fatalf("up ran after a failed copy: %v", f.calls)
	}
}

func TestADirThatAlreadyHoldsStateIsNotOverwritten(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: legacyMounts}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)
	db := filepath.Join(appDir, "database", "filebrowser.db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("recovered by hand"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall("filebrowser"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := readFile(t, db); got != "recovered by hand" {
		t.Errorf("an existing database was overwritten with %q", got)
	}
	if f.saw("cp abc123:/database") {
		t.Errorf("copied into a dir that already held a database: %v", f.calls)
	}
	if !f.saw("cp abc123:/config/.") {
		t.Errorf("the empty config dir was not filled: %v", f.calls)
	}
}

func TestReinstallAfterTheOldContainerIsGoneWarnsWhereTheDatabaseIs(t *testing.T) {
	f := &fakeDocker{}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	carried, err := Install(Registry["filebrowser"], InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	warning := carried.InstallWarning("filebrowser")
	for _, want := range []string{"/database", "anonymous Docker volumes", "started without them", "#327"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning does not mention %q:\n%s", want, warning)
		}
	}
	lines := strings.Join(carried.Lines("filebrowser", warning), "\n")
	// Docker 20.10 does not label anonymous volumes, so the hint cannot
	// filter on that label.
	if !strings.Contains(lines, "docker volume ls --filter dangling=true") || strings.Contains(lines, "label=") {
		t.Errorf("the CLI lines do not give a command that works on Docker 20.10:\n%s", lines)
	}
	if f.saw("cp ") || f.saw(compose+" stop") {
		t.Errorf("nothing to copy from, but docker was asked to: %v", f.calls)
	}

	// The compose file is new now, so a second reinstall has nothing to warn about.
	carried, err = Install(Registry["filebrowser"], InstallOptions{})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if w := carried.InstallWarning("filebrowser"); w != "" {
		t.Errorf("warned again over a new-style install: %q", w)
	}
}

// An uninstall that finds the state stranded keeps everything, as it says,
// so its warning must not read like an install's.
func TestUninstallSaysStrandedStateIsNotInTheAppDir(t *testing.T) {
	f := &fakeDocker{}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	carried, err := Uninstall("filebrowser")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	warning := carried.UninstallWarning("filebrowser", appDir)
	if !strings.Contains(warning, "not in "+appDir) || !strings.Contains(warning, "did not touch them") {
		t.Errorf("uninstall warning = %q", warning)
	}
	if strings.Contains(warning, "started without") {
		t.Errorf("uninstall warning is worded for an install: %q", warning)
	}
}

// A failed `up` leaves a container that never ran. Its volumes are new and
// empty, and copying them would hide the old install's real database.
func TestAContainerThatNeverRanIsNotTheOldInstall(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: "0001-01-01T00:00:00Z\nbind /srv \nvolume /config newcfg\nvolume /database newdb\n"}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	carried, err := Install(Registry["filebrowser"], InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(carried.Copied) != 0 || f.saw("cp ") || f.saw(compose+" stop") {
		t.Errorf("copied from a container that never ran: %+v %v", carried, f.calls)
	}
	if carried.InstallWarning("filebrowser") == "" {
		t.Error("no warning, though the old install's state is stranded")
	}
}

func TestAFreshInstallAsksDockerNothingAboutOldVolumes(t *testing.T) {
	f := &fakeDocker{}
	appDir, compose := withScriptedDocker(t, f)

	carried, err := Install(Registry["filebrowser"], InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if f.saw(compose + " ps") {
		t.Errorf("a fresh install looked for an old container: %v", f.calls)
	}
	if len(carried.Copied) != 0 || len(carried.Stranded) != 0 {
		t.Errorf("a fresh install carried %+v", carried)
	}
	// Docker would create a missing bind source as root, and filebrowser
	// could not write to it.
	for _, d := range []string{"config", "database", "data"} {
		if fi, err := os.Stat(filepath.Join(appDir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s was not created before compose up: %v", d, err)
		}
	}
}

func TestAnOldInstallThatDockerCannotBeAskedAboutIsNotReplaced(t *testing.T) {
	f := &fakeDocker{psErr: errors.New("Cannot connect to the Docker daemon")}
	appDir, _ := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	if _, err := Install(Registry["filebrowser"], InstallOptions{}); err == nil {
		t.Fatal("Install went ahead without knowing whether the old container held the database")
	}
	if got := readFile(t, filepath.Join(appDir, "docker-compose.yml")); got != legacyFilebrowserCompose {
		t.Errorf("the old compose file was rewritten")
	}
}

func TestBindMountedStateIsNotCopied(t *testing.T) {
	f := &fakeDocker{ids: "abc123\n", mounts: "2026-10-11T09:00:00Z\nbind /srv \nbind /config \nbind /database \n"}
	appDir, compose := withScriptedDocker(t, f)
	writeLegacyInstall(t, appDir)

	if _, err := Uninstall("filebrowser"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if f.saw("cp ") || f.saw(compose+" stop") {
		t.Errorf("copied state that was not in a volume: %v", f.calls)
	}
}

func renderFilebrowser(t *testing.T, f *fakeDocker) string {
	t.Helper()
	withScriptedDocker(t, f)
	user, _ := containerUser(Registry["filebrowser"])
	ctx := composeContext{Port: "8081", AppDir: "/a", DataDir: "/a/data", User: user}
	var b strings.Builder
	if err := mustParse(t, Registry["filebrowser"].ComposeFile).Execute(&b, ctx); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Under rootless Docker or Podman the container's root is the installing
// user, and the installing user's own uid would be a subordinate one that
// cannot write to the state dirs.
func TestRootlessDockerRunsFilebrowserAsContainerRoot(t *testing.T) {
	got := renderFilebrowser(t, &fakeDocker{info: "[name=seccomp,profile=builtin name=rootless name=cgroupns]"})
	if !strings.Contains(got, `user: "0:0"`) {
		t.Errorf("rootless render:\n%s", got)
	}
}

func TestFilebrowserRunsAsTheInstallingUser(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is the case below")
	}
	got := renderFilebrowser(t, &fakeDocker{info: "[name=seccomp,profile=builtin name=cgroupns]"})
	want := `user: "` + itoa(os.Getuid()) + ":" + itoa(os.Getgid()) + `"`
	if !strings.Contains(got, want) {
		t.Errorf("want %s in:\n%s", want, got)
	}
}

// Under sudo the installing user is root. Running filebrowser as root would
// give it more than its image asks for, so it runs as the image's own user,
// who is given the state dirs.
func TestUnderSudoFilebrowserKeepsTheImagesUser(t *testing.T) {
	if os.Geteuid() != 0 {
		user, _ := containerUser(App{StateDirs: []StateDir{{"x", "/x"}}, ImageUser: "1000:1000"})
		if user == "" {
			t.Fatal("a non-root install rendered no user")
		}
		t.Skip("not root; the root branch is covered when the suite runs as root")
	}
	user, chown := containerUser(Registry["filebrowser"])
	if user != "" || len(chown) != 2 || chown[0] != 1000 || chown[1] != 1000 {
		t.Errorf("as root: user %q, chown %v; want no user and 1000:1000", user, chown)
	}
}

func TestAnAppWithoutStateDirsAsksDockerNothingAboutItsUser(t *testing.T) {
	f := &fakeDocker{}
	withScriptedDocker(t, f)
	if user, chown := containerUser(Registry["vaultwarden"]); user != "" || chown != nil || f.saw("info") {
		t.Errorf("vaultwarden: user %q chown %v calls %v", user, chown, f.calls)
	}
}

// Every state dir a template declares has to be bound by that template, or
// Install creates a directory nothing uses and the volume stays anonymous.
func TestEveryStateDirIsBoundByItsTemplate(t *testing.T) {
	for name, app := range Registry {
		for _, d := range app.StateDirs {
			bind := "{{.AppDir}}/" + d.Name + ":" + d.Path + "\""
			if !strings.Contains(app.ComposeFile, bind) {
				t.Errorf("%s: template does not bind %s", name, bind)
			}
		}
	}
}

func TestFilebrowserKeepsItsStateBesideTheFilesItServes(t *testing.T) {
	app := Registry["filebrowser"]
	var paths []string
	for _, d := range app.StateDirs {
		paths = append(paths, d.Path)
	}
	if strings.Join(paths, " ") != "/config /database" {
		t.Errorf("filebrowser state dirs = %v", paths)
	}
	// data/ is /srv, which filebrowser serves: a database under it could be
	// browsed and downloaded.
	if strings.Contains(app.ComposeFile, "{{.DataDir}}/config") || strings.Contains(app.ComposeFile, "{{.DataDir}}/database") {
		t.Error("filebrowser state is under data/, which it serves as /srv")
	}
}

func mustParse(t *testing.T, src string) *template.Template {
	t.Helper()
	tmpl, err := template.New("compose").Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func itoa(n int) string { return strconv.Itoa(n) }
