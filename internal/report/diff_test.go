package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

func snapshotWith(containers []docker.Container, listeners []ports.PortInfo) *Snapshot {
	s := &Snapshot{
		Containers: containers,
		Ports:      listeners,
		// One stable process on both sides, so the process comparison is a
		// no-op for the tests that are not about processes.
		Processes: []ProcessIdentity{{Name: "init", Hash: "000000000000"}},
	}
	for _, c := range containers {
		if c.State == "running" {
			s.RunningCount++
		} else {
			s.StoppedCount++
		}
	}
	return s
}

// The case this whole diff exists for: one container leaves, another arrives,
// every count is identical, and the old report said nothing at all.
func TestSwapWithUnchangedCountIsReported(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "aaa", Name: "vaultwarden", Image: "vaultwarden:1.32", State: "running"},
		{ID: "bbb", Name: "nginx", Image: "nginx:1.27", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "ccc", Name: "gitea", Image: "gitea:1.24", State: "running"},
		{ID: "bbb", Name: "nginx", Image: "nginx:1.27", State: "running"},
	}, nil)

	if prev.RunningCount != curr.RunningCount {
		t.Fatalf("fixture is not the swap case: %d vs %d", prev.RunningCount, curr.RunningCount)
	}

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")

	if !strings.Contains(got, "gone: vaultwarden") {
		t.Errorf("the departed container was not named: %s", got)
	}
	if !strings.Contains(got, "new: gitea") {
		t.Errorf("the arrived container was not named: %s", got)
	}
	if strings.Contains(got, "No significant changes") {
		t.Errorf("a swap was reported as no change: %s", got)
	}
}

func TestSameNameDifferentContainerIsReplaced(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:1.27", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "91be0322bb22", Name: "nginx", Image: "nginx:1.27", State: "running"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22") {
		t.Errorf("a replaced container was not reported: %s", got)
	}
}

func TestImageChangeIsReported(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "same", Name: "jellyfin", Image: "jellyfin:10.9.11", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "same", Name: "jellyfin", Image: "jellyfin:10.10.0", State: "running"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "image: jellyfin — jellyfin:10.9.11 → jellyfin:10.10.0") {
		t.Errorf("an image change was not reported: %s", got)
	}
}

func TestPortOwnerChangeIsReported(t *testing.T) {
	prev := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "vaultwarden"},
	})
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "gitea"},
	})

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "port: :8080/tcp — vaultwarden → gitea") {
		t.Errorf("a port that changed owner was not reported: %s", got)
	}
}

// Status is "Up 4 hours" and moves on every single run. Diffing it would put
// noise in every report, so it is deliberately not compared.
func TestVolatileStatusIsSuppressed(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "same", Name: "nginx", Image: "nginx:1.27", State: "running", Status: "Up 4 hours"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "same", Name: "nginx", Image: "nginx:1.27", State: "running", Status: "Up 9 hours"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "No significant changes") {
		t.Errorf("an uptime string change earned a line: %s", got)
	}
}

// Diffing against a snapshot taken while docker was down would report every
// container as gone. The Failed field exists to stop that.
func TestFailedCollectorSkipsTheDiff(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "aaa", Name: "vaultwarden", Image: "vaultwarden:1.32", State: "running"},
	}, nil)
	curr := snapshotWith(nil, nil)
	curr.Failed = []string{inventory.CollectorDocker}

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "gone: vaultwarden") {
		t.Errorf("a container was reported gone because docker was down: %s", got)
	}
	// The caveat has to be in the section itself. An agent reading
	// notable_changes must not be handed an all-clear the report cannot
	// stand behind.
	if !strings.Contains(got, "skipped: containers — not compared") {
		t.Errorf("the skipped comparison is not in notable_changes: %s", got)
	}
	if strings.Contains(got, "No significant changes") {
		t.Errorf("a skipped comparison was reported as no change: %s", got)
	}
}

// The human section collapses; the JSON list does not. A person needs the
// section readable, an agent needs every entry.
func TestGroupingIsHumanOnly(t *testing.T) {
	var prevList, currList []docker.Container
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		prevList = append(prevList, docker.Container{ID: "old-" + name, Name: name, Image: "img", State: "exited"})
		currList = append(currList, docker.Container{ID: "old-" + name, Name: name, Image: "img", State: "running"})
	}

	r := buildReport(snapshotWith(currList, nil), snapshotWith(prevList, nil))

	if len(r.NotableChanges) != 7 {
		t.Errorf("--json should carry every change ungrouped, got %d: %v", len(r.NotableChanges), r.NotableChanges)
	}

	grouped := groupChanges(r.changes)
	if len(grouped) != 1 {
		t.Fatalf("seven changes of one kind should collapse to one line, got %d", len(grouped))
	}
	if !strings.Contains(grouped[0].Subject, "7 containers") {
		t.Errorf("the collapsed line does not count what it collapsed: %+v", grouped[0])
	}
	if !strings.Contains(grouped[0].Detail, "+4") {
		t.Errorf("the collapsed line names three and counts the rest: %+v", grouped[0])
	}
}

func TestBelowThresholdIsNotGrouped(t *testing.T) {
	var prevList, currList []docker.Container
	for _, name := range []string{"a", "b", "c"} {
		prevList = append(prevList, docker.Container{ID: "id-" + name, Name: name, Image: "img", State: "exited"})
		currList = append(currList, docker.Container{ID: "id-" + name, Name: name, Image: "img", State: "running"})
	}

	r := buildReport(snapshotWith(currList, nil), snapshotWith(prevList, nil))
	if got := len(groupChanges(r.changes)); got != 3 {
		t.Errorf("three changes should stay three lines, got %d", got)
	}
}

// Found by running it: "gone" describes both a container and a listener, so
// grouping by kind alone produced one line reading "11 containers" over a
// list of ports.
func TestCollapsedLinesCountWhatTheyCollapsed(t *testing.T) {
	var prevC []docker.Container
	var prevP []ports.PortInfo
	for i := 0; i < 6; i++ {
		n := string(rune('a' + i))
		prevC = append(prevC, docker.Container{ID: n, Name: "container-" + n, Image: "img", State: "running"})
		prevP = append(prevP, ports.PortInfo{Protocol: "tcp", Port: "900" + n, Process: "proc-" + n})
	}

	r := buildReport(snapshotWith(nil, nil), snapshotWith(prevC, prevP))
	grouped := groupChanges(r.changes)

	if len(grouped) != 2 {
		t.Fatalf("gone containers and gone ports should not share a line, got %d: %+v", len(grouped), grouped)
	}
	var sawContainers, sawPorts bool
	for _, c := range grouped {
		switch {
		case strings.Contains(c.Subject, "6 containers"):
			sawContainers = true
			if strings.Contains(c.Detail, ":900") {
				t.Errorf("the container line lists ports: %+v", c)
			}
		case strings.Contains(c.Subject, "6 ports"):
			sawPorts = true
			if strings.Contains(c.Detail, "container-") {
				t.Errorf("the port line lists containers: %+v", c)
			}
		default:
			t.Errorf("unexpected collapsed line: %+v", c)
		}
	}
	if !sawContainers || !sawPorts {
		t.Errorf("expected one line per collector, got %+v", grouped)
	}
}

// A listener is identified by its bind address as well as its port. Without
// the address, a service moving from loopback to every interface — the change
// with the largest consequence in this whole section — reads as no change at
// all, while Suggested Actions simultaneously announces a new public port.
func TestLoopbackToWildcardIsReported(t *testing.T) {
	prev := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "127.0.0.1", Port: "8080", Process: "nginx"},
	})
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "nginx"},
	})

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "No significant changes") {
		t.Fatalf("a port that became public was reported as no change: %s", got)
	}
	if !strings.Contains(got, "gone: 127.0.0.1:8080/tcp") || !strings.Contains(got, "new: :8080/tcp") {
		t.Errorf("the bind that moved was not named on both sides: %s", got)
	}
}

// Two listeners on the same port and different addresses are two listeners.
// Collapsing them means whichever the platform tool printed last wins, and a
// reordering of that output invents an owner change.
func TestSamePortDifferentAddressesDoNotCollide(t *testing.T) {
	listeners := []ports.PortInfo{
		{Protocol: "tcp", Address: "127.0.0.53", Port: "53", Process: "systemd-resolve"},
		{Protocol: "tcp", Address: "0.0.0.0", Port: "53", Process: "dnsmasq"},
	}
	reordered := []ports.PortInfo{listeners[1], listeners[0]}

	r := buildReport(snapshotWith(nil, reordered), snapshotWith(nil, listeners))
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "No significant changes") {
		t.Errorf("reordering the collector's output invented a change: %s", got)
	}
}

// The kind switch is exclusive, so a redeploy whose new container crashed
// would otherwise report only that it was replaced.
func TestReplacedContainerAlsoNamesItsState(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "aaa111", Name: "gitea", Image: "gitea:1.23", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "bbb222", Name: "gitea", Image: "gitea:1.24", State: "exited"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "running → exited") {
		t.Errorf("a redeploy that came back down did not say so: %s", got)
	}
}

// df can print the same mountpoint twice. Without a break the join is a cross
// product and one mount reported four times; the identical rows that survive
// the join are then collapsed by dedupeChanges, so one mount is one line.
func TestDuplicateMountReportsOnce(t *testing.T) {
	prev := snapshotWith(nil, nil)
	curr := snapshotWith(nil, nil)
	prev.System = &system.StatusInfo{Disks: []system.DiskInfo{
		{Mount: "/", UsedGB: 10}, {Mount: "/", UsedGB: 10},
	}}
	curr.System = &system.StatusInfo{Disks: []system.DiskInfo{
		{Mount: "/", UsedGB: 20}, {Mount: "/", UsedGB: 20},
	}}

	r := buildReport(curr, prev)
	var diskLines int
	for _, c := range r.NotableChanges {
		if strings.HasPrefix(c, "disk: /") {
			diskLines++
		}
	}
	if diskLines != 1 {
		t.Errorf("one mount should report once, got %d lines: %v", diskLines, r.NotableChanges)
	}
}

func procs(entries ...system.ProcessInfo) []system.ProcessInfo {
	out := make([]system.ProcessInfo, 0, len(entries))
	for _, e := range entries {
		if e.Elapsed == 0 {
			e.Elapsed = time.Hour
		}
		out = append(out, e)
	}
	return out
}

func snapshotWithProcesses(list []system.ProcessInfo) *Snapshot {
	s := snapshotWith(nil, nil)
	s.Processes = processIdentities(list)
	return s
}

// A name running several invocations is not "replaced" when one of them
// exits — nothing was replaced, and the departure has to be reported as one.
func TestOneOfSeveralInvocationsLeaving(t *testing.T) {
	prev := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "python3", Command: "python3 /opt/a.py"},
		system.ProcessInfo{PID: 2, Name: "python3", Command: "python3 /opt/b.py"},
	))
	curr := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "python3", Command: "python3 /opt/a.py"},
	))

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "replaced: python3") {
		t.Errorf("one of two invocations exiting was called a replacement: %s", got)
	}
	if !strings.Contains(got, "gone: python3 — 1 invocation, others still running") {
		t.Errorf("the departed invocation was not reported: %s", got)
	}
}

// A snapshot written before process tracking existed has no processes. The
// first run after upgrading must not report the whole machine as new.
func TestPreviousSnapshotWithoutProcessesIsNotAnEmptyMachine(t *testing.T) {
	prev := snapshotWith(nil, nil)
	prev.Processes = nil
	curr := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "nginx", Command: "nginx -g daemon off"},
		system.ProcessInfo{PID: 2, Name: "postgres", Command: "postgres -D /data"},
	))

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "new: nginx") || strings.Contains(got, "new: postgres") {
		t.Errorf("the first run after upgrading reported every process as new: %s", got)
	}
	if !strings.Contains(got, "not compared — the previous snapshot has none") {
		t.Errorf("the skipped comparison was not explained: %s", got)
	}
}

// A process the invocation lookup missed has an unknown age, not a young one.
// Dropping it would make a running daemon disappear and come back.
func TestUnknownAgeIsKept(t *testing.T) {
	ids := processIdentities([]system.ProcessInfo{
		{PID: 1, Name: "sshd", Command: "", Elapsed: system.ElapsedUnknown},
		{PID: 2, Name: "sed", Command: "sed -n", Elapsed: 2 * time.Second},
	})
	if len(ids) != 1 || ids[0].Name != "sshd" {
		t.Errorf("expected the unknown-age process kept and the young one dropped, got %+v", ids)
	}
}

func TestProcessAppearedAndDisappeared(t *testing.T) {
	prev := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "nginx", Command: "nginx -g daemon off"},
	))
	curr := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 2, Name: "borgbackup", Command: "borg create ::daily /data"},
	))

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "gone: nginx") {
		t.Errorf("a process that exited was not named: %s", got)
	}
	if !strings.Contains(got, "new: borgbackup") {
		t.Errorf("a process that started was not named: %s", got)
	}
}

// Same executable, different invocation. The interesting fact is that what is
// running under that name changed, not that a line moved.
func TestProcessInvocationChangeIsReplaced(t *testing.T) {
	prev := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "python3", Command: "python3 /opt/old.py"},
	))
	curr := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 2, Name: "python3", Command: "python3 /opt/new.py"},
	))

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if !strings.Contains(got, "replaced: python3 — same name, different invocation") {
		t.Errorf("an invocation change was not reported: %s", got)
	}
}

// Measured, not guessed: two runs thirty seconds apart on an idle machine
// reported the shell pipeline that was reading the report.
func TestShortLivedProcessesAreNotReported(t *testing.T) {
	prev := snapshotWithProcesses(nil)
	curr := snapshotWithProcesses([]system.ProcessInfo{
		{PID: 9, Name: "sed", Command: "sed -n 1,10p", Elapsed: 2 * time.Second},
		{PID: 10, Name: "head", Command: "head -20", Elapsed: time.Second},
	})

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "sed") || strings.Contains(got, "head") {
		t.Errorf("a process that lived for a moment earned a line: %s", got)
	}
}

// The command line carries secrets in flags and a snapshot is a file on disk.
func TestSnapshotNeverHoldsACommandLine(t *testing.T) {
	secret := "--api-token=hunter2-should-never-be-persisted"
	snap := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "agent", Command: "agent " + secret},
	))

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Fatalf("the snapshot persisted a command line: %s", data)
	}
	if len(snap.Processes) != 1 || snap.Processes[0].Hash == "" {
		t.Errorf("the invocation was not reduced to a hash: %+v", snap.Processes)
	}
}

// Eight workers sharing one invocation are one thing running.
func TestIdenticalProcessesCollapse(t *testing.T) {
	var list []system.ProcessInfo
	for i := 0; i < 8; i++ {
		list = append(list, system.ProcessInfo{PID: i + 1, Name: "worker", Command: "worker --pool", Elapsed: time.Hour})
	}
	if got := len(processIdentities(list)); got != 1 {
		t.Errorf("eight identical workers became %d identities", got)
	}
}

// If ps fails, AllProcesses returns an error and inventory records the
// collector as failed. Without that, every process on the machine reads as
// having disappeared — the same shape as the Docker case, reachable through
// a different door.
func TestFailedProcessCollectorDoesNotEmptyTheMachine(t *testing.T) {
	prev := snapshotWithProcesses(procs(
		system.ProcessInfo{PID: 1, Name: "nginx", Command: "nginx -g daemon off"},
		system.ProcessInfo{PID: 2, Name: "postgres", Command: "postgres -D /data"},
	))
	curr := snapshotWithProcesses(nil)
	curr.Failed = []string{inventory.CollectorProcesses}

	r := buildReport(curr, prev)
	got := strings.Join(r.NotableChanges, " | ")
	if strings.Contains(got, "gone: nginx") || strings.Contains(got, "gone: postgres") {
		t.Errorf("a failed collector reported every process as gone: %s", got)
	}
	if !strings.Contains(got, "skipped: processes — not compared") {
		t.Errorf("the skipped comparison is not in notable_changes: %s", got)
	}
}

// Found on a Raspberry Pi: docker publishes a port on both address families,
// so one container starting printed "new :8099/tcp" twice with nothing to
// tell the two lines apart.
func TestOnePortOpeningIsOneLine(t *testing.T) {
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8099", Process: "docker-proxy"},
		{Protocol: "tcp", Address: "[::]", Port: "8099", Process: "docker-proxy"},
	})

	r := buildReport(curr, snapshotWith(nil, nil))
	var lines int
	for _, c := range r.NotableChanges {
		if strings.Contains(c, ":8099") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("one port opening produced %d lines: %v", lines, r.NotableChanges)
	}
}

// The case the section exists for: a service that stopped being local. It is
// a departure and an arrival in Notable Changes with nothing correlating
// them, and no threshold on the current reading can see it at all.
func TestPortBecomingPublicNeedsAttention(t *testing.T) {
	prev := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "127.0.0.1", Port: "8080", Process: "vaultwarden"},
	})
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "vaultwarden"},
	})

	r := buildReport(curr, prev)
	got := strings.Join(r.NeedsAttention, " | ")
	if !strings.Contains(got, ":8080/tcp is now reachable from every interface") {
		t.Errorf("a port that became public did not need attention: %v", r.NeedsAttention)
	}
	if !strings.Contains(got, "vaultwarden") {
		t.Errorf("the entry does not name what is answering: %v", r.NeedsAttention)
	}
}

// A port that was already public is not news the second time.
func TestAlreadyPublicPortIsNotFlagged(t *testing.T) {
	listeners := []ports.PortInfo{{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "nginx"}}
	r := buildReport(snapshotWith(nil, listeners), snapshotWith(nil, listeners))
	if len(r.NeedsAttention) != 0 {
		t.Errorf("an unchanged public port was flagged: %v", r.NeedsAttention)
	}
}

// A redeploy that did not come back is the failure a person most needs told,
// and it was previously a change line whose detail happened to end in exited.
func TestRedeployThatDidNotComeBackNeedsAttention(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "aaa111", Name: "gitea", Image: "gitea:1.23", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "bbb222", Name: "gitea", Image: "gitea:1.24", State: "exited"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NeedsAttention, " | ")
	if !strings.Contains(got, "gitea was recreated and is exited, not running") {
		t.Errorf("a failed redeploy did not need attention: %v", r.NeedsAttention)
	}
}

func TestContainerStoppedSinceLastReportIsNamed(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "same", Name: "postgres", Image: "postgres:16", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "same", Name: "postgres", Image: "postgres:16", State: "exited"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.NeedsAttention, " | ")
	if !strings.Contains(got, "postgres stopped since the last report") {
		t.Errorf("the stopped container was not named: %v", r.NeedsAttention)
	}
}

// Attention is derived from the snapshots, never from the rendered strings —
// the detail column is prose and nothing may depend on its wording.
func TestAttentionDoesNotDependOnDetailWording(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "aaa", Name: "app", Image: "app:1", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "bbb", Name: "app", Image: "app:1", State: "exited"},
	}, nil)

	r := buildReport(curr, prev)
	if len(r.NeedsAttention) == 0 {
		t.Fatal("nothing needed attention")
	}
	for _, c := range r.changes {
		c.Detail = "wording that no longer says anything"
		_ = c
	}
	again := buildReport(curr, prev)
	if len(again.NeedsAttention) != len(r.NeedsAttention) {
		t.Error("attention changed with the detail wording")
	}
}

func TestActionNamesThePortAndTheProcess(t *testing.T) {
	prev := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "127.0.0.1", Port: "8080", Process: "vaultwarden"},
	})
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "gitea"},
	})

	r := buildReport(curr, prev)
	got := strings.Join(r.SuggestedActions, " | ")
	if !strings.Contains(got, ":8080/tcp") || !strings.Contains(got, "gitea") {
		t.Errorf("the action names neither the port nor the process: %v", r.SuggestedActions)
	}
	if strings.Contains(got, "port(s)") {
		t.Errorf("the action still speaks in counts: %v", r.SuggestedActions)
	}
}

// The count-based action went silent here: same number of public ports, a
// different process answering. It is the blind spot #58 closed one section up.
func TestPortChangingHandsStillSuggestsSomething(t *testing.T) {
	prev := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "vaultwarden"},
	})
	curr := snapshotWith(nil, []ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "gitea"},
	})

	r := buildReport(curr, prev)
	if len(r.SuggestedActions) == 0 {
		t.Error("a port that changed hands suggested nothing")
	}
}

func TestActionNamesTheContainerAndTheCommand(t *testing.T) {
	prev := snapshotWith([]docker.Container{
		{ID: "same", Name: "postgres", Image: "postgres:16", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "same", Name: "postgres", Image: "postgres:16", State: "exited"},
	}, nil)

	r := buildReport(curr, prev)
	got := strings.Join(r.SuggestedActions, " | ")
	if !strings.Contains(got, "homebutler docker logs postgres") {
		t.Errorf("the action does not name the command to run: %v", r.SuggestedActions)
	}
}

// A reboot stops everything at once. The section a person reads first must
// not become the longest one on the page.
func TestManyStoppedContainersCollapse(t *testing.T) {
	var prevList, currList []docker.Container
	for i := 0; i < 30; i++ {
		n := fmt.Sprintf("svc-%02d", i)
		prevList = append(prevList, docker.Container{ID: n, Name: n, Image: "img", State: "running"})
		currList = append(currList, docker.Container{ID: n, Name: n, Image: "img", State: "exited"})
	}

	r := buildReport(snapshotWith(currList, nil), snapshotWith(prevList, nil))

	var stoppedLines int
	for _, a := range r.NeedsAttention {
		if strings.Contains(a, "stopped since the last report") {
			stoppedLines++
		}
	}
	if stoppedLines != 1 {
		t.Errorf("thirty stopped containers produced %d attention lines: %v", stoppedLines, r.NeedsAttention)
	}
	if !strings.Contains(strings.Join(r.NeedsAttention, " "), "30 containers stopped") {
		t.Errorf("the collapsed line does not count what it collapsed: %v", r.NeedsAttention)
	}
	if len(r.SuggestedActions) > groupNamed {
		t.Errorf("thirty stopped containers produced %d actions", len(r.SuggestedActions))
	}
}
