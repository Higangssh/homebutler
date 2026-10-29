package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/system"
)

// reports is a sequence of report runs against one snapshot directory, on a
// clock the test moves. Each run is stamped later than the one before, so it
// writes a snapshot of its own and the next run reads it back from disk.
type reports struct {
	t   *testing.T
	dir string
	at  time.Time
}

func newReports(t *testing.T) *reports {
	t.Helper()
	r := &reports{t: t, dir: t.TempDir(), at: time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)}
	saved := now
	now = func() time.Time { return r.at }
	t.Cleanup(func() { now = saved })
	return r
}

// after runs a report that much later than the last, on a machine whose
// processes are list.
func (r *reports) after(d time.Duration, list []system.ProcessInfo) *Report {
	r.t.Helper()
	return r.slow(d, 0, list)
}

// slow is after with a collection that spends collecting long before it reads
// the process list, as a slow Docker daemon makes it.
func (r *reports) slow(d, collecting time.Duration, list []system.ProcessInfo) *Report {
	r.t.Helper()
	r.at = r.at.Add(d)
	fns := fakeFuncs(nil, nil)
	status := fns.StatusFn
	fns.StatusFn = func() (*system.StatusInfo, error) {
		r.at = r.at.Add(collecting)
		return status()
	}
	fns.ProcessesFn = func() ([]system.ProcessInfo, error) { return list, nil }
	rep, err := Run(&config.Config{}, fns, Options{SnapshotDir: r.dir, Keep: DefaultKeep})
	if err != nil {
		r.t.Fatalf("Run: %v", err)
	}
	return rep
}

// latest is the snapshot the sequence wrote last, read back from disk.
func (r *reports) latest() Snapshot {
	r.t.Helper()
	files, err := filepath.Glob(filepath.Join(r.dir, "snapshot_*.json"))
	if err != nil || len(files) == 0 {
		r.t.Fatalf("no snapshots: %v", err)
	}
	data, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		r.t.Fatal(err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		r.t.Fatal(err)
	}
	return s
}

// snapshots is how many files the sequence has written. A test that means to
// compare distinct snapshots checks it, so an overwrite cannot pass for one.
func (r *reports) snapshots() int {
	r.t.Helper()
	files, err := filepath.Glob(filepath.Join(r.dir, "snapshot_*.json"))
	if err != nil {
		r.t.Fatal(err)
	}
	return len(files)
}

func aged(age time.Duration, entries ...system.ProcessInfo) []system.ProcessInfo {
	out := make([]system.ProcessInfo, 0, len(entries))
	for _, e := range entries {
		e.Elapsed = age
		out = append(out, e)
	}
	return out
}

func with(lists ...[]system.ProcessInfo) []system.ProcessInfo {
	var out []system.ProcessInfo
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

var (
	sshd       = system.ProcessInfo{PID: 700, PPID: 1, Name: "sshd", Command: "sshd: /usr/sbin/sshd -D"}
	userSystem = system.ProcessInfo{PID: 2001, PPID: 1, Name: "systemd", Command: "/lib/systemd/systemd --user"}
	sdPam      = system.ProcessInfo{PID: 2002, PPID: 2001, Name: "(sd-pam)", Command: "(sd-pam)"}
	cron       = system.ProcessInfo{PID: 500, PPID: 1, Name: "cron", Command: "/usr/sbin/cron -f"}
	agent      = system.ProcessInfo{PID: 800, PPID: 1, Name: "agent", Command: "/usr/local/bin/agent --serve"}
)

// #315, case 3: the first report taken from an ssh session that had just
// logged in left out the session's systemd --user and (sd-pam), and the next
// report called them new though they had been running all along. The same
// happened to a container recreated half a minute before the baseline.
func TestABaselineTakenJustAfterSomethingStartedDoesNotCallItNewLater(t *testing.T) {
	r := newReports(t)
	r.after(0, with(aged(time.Hour, sshd), aged(30*time.Second, userSystem, sdPam)))
	next := r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(time.Hour+30*time.Second, userSystem, sdPam)))

	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("nothing started since the baseline, got %v", got)
	}
	if r.snapshots() != 2 {
		t.Fatalf("want two snapshots, found %d", r.snapshots())
	}
}

// A first report run from inside tmux that had just been started: the tmux
// server and its shell are young at the baseline, and were running all along
// when the next day's report sees them.
func TestATmuxServerAtTheBaselineIsNotNewTheNextDay(t *testing.T) {
	r := newReports(t)
	tmux := []system.ProcessInfo{
		{PID: 5000, PPID: 1, Name: "tmux: server", Command: "tmux new -s ops"},
		{PID: 5001, PPID: 5000, Name: "bash", Command: "-bash"},
	}
	r.after(0, with(aged(time.Hour, sshd), aged(20*time.Second, tmux...)))
	next := r.after(24*time.Hour, with(aged(25*time.Hour, sshd), aged(24*time.Hour, tmux...)))

	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("the tmux session the baseline ran in was called a change: %v", got)
	}
}

// The report's own invocation is young at every report: the ssh session it
// is run from, the shell, and the jq reading it. Wherever it is run from next,
// none of that is ever gone, because none of it was ever seen a minute old.
func TestTheReportsOwnShellAndPipelineAreNeverGone(t *testing.T) {
	listener := system.ProcessInfo{PID: 650, PPID: 1, Name: "sshd", Command: "sshd: /usr/sbin/sshd -D [listener]"}
	fromSSH := []system.ProcessInfo{
		{PID: 9000, PPID: 650, Name: "sshd-session", Command: "sshd-session: sanghee [priv]"},
		{PID: 9001, PPID: 9000, Name: "sshd-session", Command: "sshd-session: sanghee@notty"},
		{PID: 9002, PPID: 9001, Name: "bash", Command: "bash -c homebutler report --json | jq ."},
		{PID: 9003, PPID: 9002, Name: "homebutler", Command: "homebutler report --json"},
		{PID: 9004, PPID: 9002, Name: "jq", Command: "jq ."},
	}
	fromCron := []system.ProcessInfo{
		{PID: 9100, PPID: 500, Name: "sh", Command: "/bin/sh -c homebutler report > /var/log/report.log"},
		{PID: 9101, PPID: 9100, Name: "homebutler", Command: "homebutler report"},
	}
	machine := func(age time.Duration) []system.ProcessInfo { return aged(age, listener, cron) }

	r := newReports(t)
	r.after(0, with(machine(time.Hour), aged(2*time.Second, fromSSH...)))
	for i, run := range [][]system.ProcessInfo{
		with(machine(2*time.Hour), aged(2*time.Second, fromSSH...)),
		with(machine(3*time.Hour), aged(time.Second, fromCron...)),
		with(machine(4*time.Hour), aged(time.Second, fromCron...)),
		with(machine(5*time.Hour), aged(2*time.Second, fromSSH...)),
		with(machine(6 * time.Hour)),
	} {
		if got := changeTargets(r.after(time.Hour, run)); len(got) != 0 {
			t.Fatalf("report %d: the report's own invocation was reported: %v", i+1, got)
		}
	}
}

// `ssh host 'nohup ./server & homebutler report'`: the server starts in the
// same command as the report. At a baseline it is unconfirmed and is not new
// the next day; restarted that way when it was already running, it is a
// restart, not new.
func TestADaemonStartedJustBeforeTheReportIsNotNew(t *testing.T) {
	server := system.ProcessInfo{PID: 6000, PPID: 1, Name: "server", Command: "./server --port 8080"}

	r := newReports(t)
	r.after(0, with(aged(time.Hour, sshd), aged(time.Second, server)))
	if got := changeTargets(r.after(24*time.Hour, with(aged(25*time.Hour, sshd), aged(24*time.Hour, server)))); len(got) != 0 {
		t.Fatalf("a server running since the baseline was called a change: %v", got)
	}

	restarted := r.after(24*time.Hour, with(aged(49*time.Hour, sshd), aged(time.Second, server)))
	if got := changeTargets(restarted); len(got) != 0 {
		t.Fatalf("restarting a server that was running is not new: %v", got)
	}
	for _, p := range r.latest().Processes {
		if p.Name == "server" && p.Young != youngRestarted {
			t.Fatalf("want the server saved as restarted, got %q", p.Young)
		}
	}
}

// What the baseline kept as unconfirmed may have been passing through, such
// as the pipeline reading the report. It leaving is not a change.
func TestAYoungProcessAtTheBaselineThatLeftIsNotGone(t *testing.T) {
	r := newReports(t)
	head := system.ProcessInfo{PID: 3000, PPID: 1, Name: "head", Command: "head -20"}
	r.after(0, with(aged(time.Hour, sshd), aged(time.Second, head)))
	next := r.after(time.Hour, aged(2*time.Hour, sshd))

	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("a process that lived for a moment earned a line: %v", got)
	}
}

// A job that runs for half a minute every minute can be young at several
// reports in a row and missing from the next. It has never been seen running
// for a minute, so neither is a change.
func TestAShortPeriodicJobIsNeverReported(t *testing.T) {
	r := newReports(t)
	job := system.ProcessInfo{PID: 7000, PPID: 500, Name: "poll.sh", Command: "/bin/sh /opt/poll.sh"}
	r.after(0, with(aged(time.Hour, sshd), aged(10*time.Second, job)))
	for i, run := range [][]system.ProcessInfo{
		with(aged(2*time.Hour, sshd), aged(20*time.Second, job)),
		with(aged(3*time.Hour, sshd), aged(5*time.Second, job)),
		aged(4*time.Hour, sshd),
		with(aged(5*time.Hour, sshd), aged(25*time.Second, job)),
	} {
		if got := changeTargets(r.after(time.Hour, run)); len(got) != 0 {
			t.Fatalf("report %d: a job that never ran for a minute was reported: %v", i+1, got)
		}
	}
}

// Keeping young processes in the baseline must not swallow a real arrival.
// Something that starts shortly before a later report is held back on that
// run and is still new on the one after.
func TestAYoungArrivalAfterTheBaselineIsStillNewOnTheNextRun(t *testing.T) {
	r := newReports(t)
	borg := system.ProcessInfo{PID: 4000, PPID: 1, Name: "borg", Command: "borg serve"}
	r.after(0, aged(time.Hour, sshd))
	held := r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(20*time.Second, borg)))
	if got := changeTargets(held); len(got) != 0 {
		t.Fatalf("a process twenty seconds old was reported: %v", got)
	}
	next := r.after(time.Hour, with(aged(3*time.Hour, sshd), aged(time.Hour+20*time.Second, borg)))

	got := changeTargets(next)
	if len(got) != 1 || got[0] != "new borg" {
		t.Fatalf("want borg reported new once it had run for a minute, got %v", got)
	}
	if r.snapshots() != 3 {
		t.Fatalf("want three snapshots, found %d", r.snapshots())
	}
}

// A daemon restarted with the same command line just before a report is the
// daemon it was. It used to be held back as young, which made it gone on that
// report and new on the next.
func TestADaemonRestartedJustBeforeAReportIsNeitherGoneNorNew(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, cron))
	restarted := r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(10*time.Second, cron)))
	if got := changeTargets(restarted); len(got) != 0 {
		t.Fatalf("a restart with the same command line was reported: %v", got)
	}
	next := r.after(time.Hour, with(aged(3*time.Hour, sshd), aged(time.Hour+10*time.Second, cron)))
	if got := changeTargets(next); len(got) != 0 {
		t.Fatalf("the restarted daemon came back as a change: %v", got)
	}
}

// The restarted daemon keeps its place, so if it then stops for good, that is
// still reported.
func TestARestartedDaemonThatThenStopsIsStillGone(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, cron))
	r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(10*time.Second, cron)))
	next := r.after(time.Hour, aged(3*time.Hour, sshd))

	got := changeTargets(next)
	if len(got) != 1 || got[0] != "gone cron" {
		t.Fatalf("want cron gone, got %v", got)
	}
}

// A daemon that has been seen running and is then seconds old at every
// report is crash-looping. 1.3.0 said `gone` once, the morning it started;
// keeping the restart's place must not turn that into nothing at all.
func TestACrashLoopingDaemonIsReported(t *testing.T) {
	r := newReports(t)
	r.after(0, aged(time.Hour, sshd, agent))
	first := r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(5*time.Second, agent)))
	if got := changeTargets(first); len(got) != 0 {
		t.Fatalf("one restart is not a change, got %v", got)
	}
	for i := range 3 {
		again := r.after(time.Hour, with(aged(time.Duration(i+3)*time.Hour, sshd), aged(5*time.Second, agent)))
		got := changeTargets(again)
		if len(got) != 1 || got[0] != "state agent" {
			t.Fatalf("report %d: want agent reported as restarting, got %v", i+3, got)
		}
		if d := again.NotableChanges[0].Detail; d != "restarting — under a minute old at this report and the last" {
			t.Fatalf("report %d: the line does not say what happened: %q", i+3, d)
		}
	}
	settled := r.after(time.Hour, with(aged(7*time.Hour, sshd), aged(time.Hour, agent)))
	if got := changeTargets(settled); len(got) != 0 {
		t.Fatalf("a daemon that stayed up is not restarting, got %v", got)
	}
}

// Two reports less than a minute apart can both see the same young process.
// That is one restart, not two — however long each collection took before it
// read the process list, and whatever ps rounded away.
func TestOneRestartSeenByTwoReportsIsNotCountedTwice(t *testing.T) {
	cases := map[string]struct {
		firstCollecting, gap, secondCollecting time.Duration
		secondAge                              time.Duration
	}{
		// The first collection spent 20s on Docker before reading processes,
		// the second none: 60s between the two starts, 40s between the two
		// process lists, and the process, 5s old at the first, is 45s old.
		"slow first collection": {20 * time.Second, 40 * time.Second, 0, 45 * time.Second},
		// 20.4s between lists, and ps prints the 20.6s-old process as 20s.
		"etime rounded down": {0, 20*time.Second + 400*time.Millisecond, 0, 20 * time.Second},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReports(t)
			r.after(0, aged(time.Hour, sshd, agent))
			r.slow(time.Hour, c.firstCollecting, with(aged(2*time.Hour, sshd), aged(5*time.Second, agent)))
			soon := r.slow(c.gap, c.secondCollecting, with(aged(2*time.Hour+c.gap, sshd), aged(c.secondAge, agent)))

			if got := changeTargets(soon); len(got) != 0 {
				t.Fatalf("the same process seen twice is not a restart, got %v", got)
			}
		})
	}
}

// An age ps printed in a form this cannot read is unknown, and an unknown age
// is compared as an old one. Read as young, every process on such a machine
// would be held back, and the report would be silent about all of them.
func TestAProcessOfUnknownAgeIsStillReportedGone(t *testing.T) {
	r := newReports(t)
	unknown := func(list ...system.ProcessInfo) []system.ProcessInfo {
		return aged(system.ElapsedUnknown, list...)
	}
	r.after(0, unknown(sshd, agent))
	got := changeTargets(r.after(time.Hour, unknown(sshd)))
	if len(got) != 1 || got[0] != "gone agent" {
		t.Fatalf("want agent gone, got %v", got)
	}
}

// A snapshot written before the young field existed reads as every process
// old, and compares as it always did.
func TestASnapshotWithoutYoungComparesAsBefore(t *testing.T) {
	older := fromDisk(t, buildSnapshot(&inventory.Inventory{ServerName: "homelab", Processes: aged(time.Hour, sshd)}))
	for _, p := range older.Processes {
		if p.Young != "" {
			t.Fatalf("an old process was written as young: %+v", p)
		}
	}
	curr := buildSnapshot(&inventory.Inventory{ServerName: "homelab", Processes: aged(time.Hour, sshd, userSystem)})

	got := changeTargets(buildReport(curr, older))
	if len(got) != 1 || got[0] != "new systemd" {
		t.Fatalf("want only the new systemd, got %v", got)
	}
}

// The snapshot keeps what each young process was known to be, and when the
// process list was read.
func TestASnapshotRecordsWhatAYoungProcessWas(t *testing.T) {
	r := newReports(t)
	r.after(0, with(aged(time.Hour, sshd, agent), aged(5*time.Second, cron)))
	r.after(time.Hour, with(aged(2*time.Hour, sshd), aged(5*time.Second, agent, cron)))

	snap := r.latest()
	young := map[string]string{}
	for _, p := range snap.Processes {
		young[p.Name] = p.Young
	}
	if young["sshd"] != "" || young["agent"] != youngRestarted || young["cron"] != youngUnconfirmed {
		t.Fatalf("want sshd old, agent restarted, cron unconfirmed, got %v", young)
	}
	if snap.ProcessesReadAt != "2026-10-29T04:00:00Z" {
		t.Fatalf("want the process list's read time saved, got %q", snap.ProcessesReadAt)
	}
}
