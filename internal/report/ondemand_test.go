package report

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/system"
)

var (
	fwupd      = system.ProcessInfo{PID: 3100, PPID: 1, Name: "fwupd", Command: "/usr/libexec/fwupd/fwupd", OnDemand: true}
	packagekit = system.ProcessInfo{PID: 3200, PPID: 1, Name: "packagekitd", Command: "/usr/libexec/packagekitd", OnDemand: true}
	udisks     = system.ProcessInfo{PID: 3300, PPID: 1, Name: "udisksd", Command: "/usr/libexec/udisks2/udisksd"}
)

// unmarked is the same process on a run that could not tell what started it:
// no systemd, an unreadable cgroup, or a systemctl that did not answer.
func unmarked(p system.ProcessInfo) system.ProcessInfo {
	p.OnDemand = false
	return p
}

// #363: fwupd was not running at the baseline, and something asked it a
// question three hours later. That is D-Bus doing its job, not a change.
func TestAnOnDemandDaemonStartedSinceTheLastReportIsNotNew(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, udisks))
	next := r.after(3*time.Hour, with(aged(4*time.Hour, sshd, udisks), aged(10*time.Minute, fwupd)))

	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("an on-demand daemon that was started was reported: %v", got)
	}
}

// The soak got `gone: fwupd` the day after a report that happened to catch it
// running. Gone is judged from the older snapshot, since the process is not
// there at the newer one to say what it was.
func TestAnOnDemandDaemonThatExitedIsNotGone(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, fwupd, packagekit))
	if !savedOnDemand(r.latest())["fwupd"] {
		t.Fatalf("the snapshot did not record fwupd as on demand: %+v", r.latest().Processes)
	}
	next := r.after(24*time.Hour, aged(25*time.Hour, sshd))

	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("an on-demand daemon that exited once idle was reported: %v", got)
	}
}

// Coming and going over several reports, young or old, it never gets a line.
func TestAnOnDemandDaemonComingAndGoingIsNeverReported(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd))
	for i, run := range [][]system.ProcessInfo{
		with(aged(2*time.Hour, sshd), aged(20*time.Second, fwupd)),
		with(aged(3*time.Hour, sshd), aged(time.Hour+20*time.Second, fwupd)),
		aged(4*time.Hour, sshd),
		with(aged(5*time.Hour, sshd), aged(5*time.Minute, fwupd, packagekit)),
		with(aged(6*time.Hour, sshd), aged(5*time.Second, packagekit)),
		aged(7*time.Hour, sshd),
	} {
		if got := changeTargets(r.after(time.Hour, run)); len(got) != 0 {
			t.Fatalf("report %d: an on-demand daemon was reported: %v", i+1, got)
		}
	}
}

// What the daemon was started with is still compared: a different command
// line under the same name is the change the hash exists to see.
func TestAnOnDemandDaemonWithADifferentCommandLineIsStillReplaced(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, fwupd))
	changed := fwupd
	changed.Command = "/usr/libexec/fwupd/fwupd --verbose"
	next := r.after(time.Hour, aged(2*time.Hour, sshd, changed))

	if got := changeTargets(next); !reflect.DeepEqual(got, []string{"replaced fwupd"}) {
		t.Fatalf("want fwupd replaced, got %v", got)
	}
}

// An on-demand invocation under a name something else is running, such as a
// timer's python3 job beside a python3 service, gets no line either.
func TestAnOnDemandInvocationUnderARunningNameIsNotNew(t *testing.T) {
	service := system.ProcessInfo{PID: 3400, PPID: 1, Name: "python3", Command: "python3 /opt/app/serve.py"}
	job := system.ProcessInfo{PID: 3401, PPID: 1, Name: "python3", Command: "python3 /opt/jobs/refresh.py", OnDemand: true}

	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, service))
	if got := changeTargets(r.after(time.Hour, aged(2*time.Hour, sshd, service, job))); len(got) != 0 {
		t.Fatalf("an on-demand job was reported: %v", got)
	}
	if got := changeTargets(r.after(time.Hour, aged(3*time.Hour, sshd, service))); len(got) != 0 {
		t.Fatalf("an on-demand job that finished was reported: %v", got)
	}
}

// Whenever this run could not say what started a process, it is compared as
// it always was: fwupd with no unit known is new, and gone.
func TestADaemonWhoseUnitIsUnknownIsStillReported(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd))
	got := changeTargets(r.after(time.Hour, aged(2*time.Hour, sshd, unmarked(fwupd))))
	if !reflect.DeepEqual(got, []string{"new fwupd"}) {
		t.Fatalf("want new fwupd when its unit is unknown, got %v", got)
	}
	got = changeTargets(r.after(time.Hour, aged(3*time.Hour, sshd)))
	if !reflect.DeepEqual(got, []string{"gone fwupd"}) {
		t.Fatalf("want gone fwupd when its unit was unknown, got %v", got)
	}
}

// An enabled service is not on demand, and stopping it is reported while
// on-demand daemons come and go around it.
func TestAnEnabledServiceThatStoppedIsStillGone(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, udisks, fwupd))
	got := changeTargets(r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(time.Minute*5, packagekit))))
	if !reflect.DeepEqual(got, []string{"gone udisksd"}) {
		t.Fatalf("want only udisksd gone, got %v", got)
	}
}

// #362's crash-loop line is about services that should stay up. An enabled
// one restarting at every report is still reported while an on-demand daemon
// is started and exits beside it.
func TestACrashLoopIsStillReportedBesideAnOnDemandDaemon(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, agent))
	r.after(time.Hour, with(aged(2*time.Hour, sshd, fwupd), aged(5*time.Second, agent)))
	for i := range 2 {
		run := with(aged(time.Duration(i+3)*time.Hour, sshd), aged(5*time.Second, agent))
		if i == 1 {
			run = with(run, aged(10*time.Second, packagekit))
		}
		if got := changeTargets(r.after(time.Hour, run)); !reflect.DeepEqual(got, []string{"state agent"}) {
			t.Fatalf("report %d: want agent restarting, got %v", i+3, got)
		}
	}
}

// The young rule still holds back a process that is not on demand, and still
// reports it new once it has run for a minute.
func TestAYoungArrivalThatIsNotOnDemandIsStillNewOnceItHasRun(t *testing.T) {
	borg := system.ProcessInfo{PID: 4000, PPID: 1, Name: "borg", Command: "borg serve"}
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd))
	if got := changeTargets(r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(20*time.Second, borg, fwupd)))); len(got) != 0 {
		t.Fatalf("young processes were reported: %v", got)
	}
	got := changeTargets(r.after(time.Hour, with(aged(3*time.Hour, sshd), aged(time.Hour+20*time.Second, borg, fwupd))))
	if !reflect.DeepEqual(got, []string{"new borg"}) {
		t.Fatalf("want only borg new, got %v", got)
	}
}

// An identity is on demand only when every process sharing it is: one that
// systemd could not place is enough to compare it as before.
func TestAnIdentityIsOnDemandOnlyWhenEveryProcessSharingItIs(t *testing.T) {
	second := unmarked(fwupd)
	second.PID = 3101
	ids := processIdentities(aged(time.Hour, fwupd, second, packagekit))
	got := map[string]bool{}
	for _, id := range ids {
		got[id.Name] = id.OnDemand
	}
	if got["fwupd"] || !got["packagekitd"] {
		t.Fatalf("want fwupd compared as before and packagekitd on demand, got %v", got)
	}
}

// A snapshot written before on_demand existed has no such key, and an on-demand
// daemon it caught running is gone once, as it was before.
func TestASnapshotWithoutOnDemandComparesAsBefore(t *testing.T) {
	older := fromDisk(t, buildSnapshot(&inventory.Inventory{ServerName: "homelab", Processes: aged(time.Hour, sshd, unmarked(fwupd))}))
	curr := buildSnapshot(&inventory.Inventory{ServerName: "homelab", Processes: aged(time.Hour, sshd)})

	if got := changeTargets(buildReport(curr, older)); !reflect.DeepEqual(got, []string{"gone fwupd"}) {
		t.Fatalf("want gone fwupd against an older snapshot, got %v", got)
	}
}

// The key is written only where it is true, so a snapshot from a machine
// without systemd is byte-for-byte what it was.
func TestOnDemandIsWrittenOnlyWhereItIsTrue(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, fwupd))
	files, _ := filepath.Glob(filepath.Join(r.dir, "snapshot_*.json"))
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Processes []map[string]json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, p := range raw.Processes {
		name := strings.Trim(string(p["name"]), `"`)
		_, has := p["on_demand"]
		if has != (name == "fwupd") {
			t.Errorf("%s: on_demand present=%v", name, has)
		}
	}
}

func savedOnDemand(s Snapshot) map[string]bool {
	out := map[string]bool{}
	for _, p := range s.Processes {
		out[p.Name] = p.OnDemand
	}
	return out
}

// A different command line is replaced on the report that sees it, young or
// not. Held back for a minute it would never be reported: the young arrival
// is not saved, and by the next report the old command line is forgotten.
func TestAnOnDemandDaemonStartedWithADifferentCommandLineJustNowIsReplaced(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, fwupd))
	changed := fwupd
	changed.Command = "/usr/libexec/fwupd/fwupd --verbose"
	got := changeTargets(r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(10*time.Second, changed))))
	if !reflect.DeepEqual(got, []string{"replaced fwupd"}) {
		t.Fatalf("want fwupd replaced, got %v", got)
	}
	if got := changeTargets(r.after(time.Hour, aged(3*time.Hour, sshd, changed))); len(got) != 0 {
		t.Fatalf("the replacement was reported again: %v", got)
	}
}

// An on-demand daemon asked something shortly before two reports in a row is
// young at both. That is what it is for, not a crash loop.
func TestAnOnDemandDaemonActivatedBeforeEveryReportIsNotRestarting(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, fwupd))
	for i := range 3 {
		run := with(aged(time.Duration(i+2)*time.Hour, sshd), aged(20*time.Second, fwupd))
		if got := changeTargets(r.after(time.Hour, run)); len(got) != 0 {
			t.Fatalf("report %d: an on-demand daemon activated again was reported: %v", i+2, got)
		}
	}
}

// A timer's job is not on demand. hbjob.service is static and wanted by
// nothing, as fwupd is, but hbjob.timer starts it, and a run still there 100
// seconds later has hung. The rule runs here as the report runs it, on what
// systemctl says.
func TestAHungTimerJobIsStillNew(t *testing.T) {
	job := system.ProcessInfo{PID: 3500, PPID: 1, Name: "hbjob", Command: "/usr/local/bin/hbjob"}
	daemon := unmarked(fwupd)
	cgroup := map[int]string{
		sshd.PID:   "0::/system.slice/ssh.service\n",
		job.PID:    "0::/system.slice/hbjob.service\n",
		daemon.PID: "0::/system.slice/fwupd.service\n",
	}
	systemctl := `Id=ssh.service
LoadState=loaded
UnitFileState=enabled
WantedBy=multi-user.target

Id=hbjob.service
LoadState=loaded
UnitFileState=static
WantedBy=
RequiredBy=
BusName=
TriggeredBy=hbjob.timer

Id=fwupd.service
LoadState=loaded
UnitFileState=static
WantedBy=
RequiredBy=
BusName=org.freedesktop.fwupd
TriggeredBy=
`
	lookup := system.UnitLookup{
		Cgroup: func(pid int) (string, error) {
			c, ok := cgroup[pid]
			if !ok {
				return "", os.ErrNotExist
			}
			return c, nil
		},
		Systemctl: func(context.Context, ...string) (string, error) { return systemctl, nil },
		Timeout:   time.Second,
	}
	marked := func(list []system.ProcessInfo) []system.ProcessInfo {
		lookup.Mark(list)
		return list
	}

	r := newReports(t)
	r.after(0, marked(aged(time.Hour, sshd)))
	got := changeTargets(r.after(time.Hour, marked(with(aged(2*time.Hour, sshd), aged(100*time.Second, job, daemon)))))
	if !reflect.DeepEqual(got, []string{"new hbjob"}) {
		t.Fatalf("want the hung timer job new and fwupd not, got %v", got)
	}
}
