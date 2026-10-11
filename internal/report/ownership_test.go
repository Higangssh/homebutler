package report

import (
	"encoding/json"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

// A host as the demo machine showed it on 2026-10-11: two containers, each
// under its own shim, two published ports, and the host's own processes.
func demoHost(filebrowserID, kumaID, kumaImage, kumaProcess, kumaEntrypoint string) *inventory.Inventory {
	return &inventory.Inventory{
		ServerName: "homelab",
		Containers: []docker.Container{
			{ID: filebrowserID, Name: "filebrowser", Image: "filebrowser/filebrowser:latest", State: "running"},
			{ID: kumaID, Name: "uptime-kuma", Image: kumaImage, State: "running"},
		},
		Ports: []ports.PortInfo{
			{Protocol: "tcp", Address: "0.0.0.0", Port: "8081", Container: "filebrowser (filebrowser/filebrowser:latest)"},
			{Protocol: "tcp", Address: "0.0.0.0", Port: "3001", Container: "uptime-kuma (" + kumaImage + ")"},
		},
		Processes: procs(
			system.ProcessInfo{PID: 400, PPID: 1, Name: "dockerd", Command: "/usr/bin/dockerd -H fd://"},
			system.ProcessInfo{PID: 410, PPID: 400, Name: "docker-proxy", Command: "/usr/bin/docker-proxy -proto tcp -host-port 8081 -container-ip 172.17.0.2"},
			system.ProcessInfo{PID: 411, PPID: 400, Name: "docker-proxy", Command: "/usr/bin/docker-proxy -proto tcp -host-port 3001 -container-ip 172.17.0.3"},
			system.ProcessInfo{PID: 500, PPID: 1, Name: "containerd-shim", Command: "/usr/bin/containerd-shim-runc-v2 -namespace moby -id " + filebrowserID},
			system.ProcessInfo{PID: 501, PPID: 500, Name: "filebrowser", Command: "/bin/filebrowser"},
			system.ProcessInfo{PID: 600, PPID: 1, Name: "containerd-shim", Command: "/usr/bin/containerd-shim-runc-v2 -namespace moby -id " + kumaID},
			system.ProcessInfo{PID: 601, PPID: 600, Name: "dumb-init", Command: kumaEntrypoint},
			system.ProcessInfo{PID: 602, PPID: 601, Name: kumaProcess, Command: kumaProcess + " server/server.js"},
			system.ProcessInfo{PID: 700, PPID: 1, Name: "sshd", Command: "sshd: /usr/sbin/sshd -D"},
		),
	}
}

// writtenBefore is the snapshot a release before this split would have
// written for inv: the same machine, with every process kept.
func writtenBefore(t *testing.T, inv *inventory.Inventory, list []system.ProcessInfo) *Snapshot {
	t.Helper()
	s := fromDisk(t, buildSnapshot(inv))
	s.Processes = processIdentities(list)
	return s
}

// fromDisk is a snapshot as the next run reads it back: only what is written.
func fromDisk(t *testing.T, s *Snapshot) *Snapshot {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func changeTargets(r *Report) []string {
	var out []string
	for _, c := range r.NotableChanges {
		out = append(out, c.Kind+" "+c.Target)
	}
	return out
}

// The demo recording that was not published: filebrowser recreated and
// uptime-kuma moved from :1 to :2 printed seven lines, five of them the same
// two changes seen through the process table. Two lines are the change.
func TestARecreatedContainerIsReportedOnceNotAgainAsProcesses(t *testing.T) {
	prev := fromDisk(t, buildSnapshot(demoHost(
		"fe76a47604d2", "bba200f215ef", "louislam/uptime-kuma:1",
		"node", "/usr/bin/dumb-init -- node server/server.js")))
	curr := buildSnapshot(demoHost(
		"19edbc4e9034", "13ce1ac7fd3b", "louislam/uptime-kuma:2",
		"uptime-kuma", "/usr/bin/dumb-init -- uptime-kuma server/server.js"))

	got := changeTargets(buildReport(curr, prev))
	want := []string{"replaced filebrowser", "replaced uptime-kuma"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("want only the two container lines %v, got %v", want, got)
	}
}

// Leaving docker-proxy out of the process comparison must not touch ports:
// a port whose owner changes is still reported, under the container's name.
func TestAPortStillChangesHandsUnderTheContainerName(t *testing.T) {
	inv := demoHost("fe76a47604d2", "bba200f215ef", "louislam/uptime-kuma:1",
		"node", "/usr/bin/dumb-init -- node server/server.js")
	prev := fromDisk(t, buildSnapshot(inv))
	inv.Ports[0].Container = "gitea (gitea/gitea:1.24)"
	curr := buildSnapshot(inv)

	var port *ChangeLine
	for _, c := range buildReport(curr, prev).NotableChanges {
		if c.Kind == "port" {
			port = &c
		}
	}
	if port == nil || port.Target != ":8081/tcp" || port.Detail != "filebrowser (filebrowser/filebrowser:latest) → gitea (gitea/gitea:1.24)" {
		t.Fatalf("the port line should name both containers, got %+v", port)
	}
}

// A snapshot from before this release holds every container's processes. The
// first report after upgrading must not call them all gone.
func TestUpgradingDoesNotReportContainerProcessesAsGone(t *testing.T) {
	inv := demoHost("fe76a47604d2", "bba200f215ef", "louislam/uptime-kuma:1",
		"node", "/usr/bin/dumb-init -- node server/server.js")
	older := writtenBefore(t, inv, inv.Processes)
	curr := buildSnapshot(inv)

	if got := changeTargets(buildReport(curr, older)); len(got) != 0 {
		t.Fatalf("nothing changed, got %v", got)
	}
}

// The transition must not hide a host process that shares a name with one in
// a container: a host node that stopped is still gone.
func TestAHostProcessSharingAContainerProcessNameIsStillCompared(t *testing.T) {
	inv := demoHost("fe76a47604d2", "bba200f215ef", "louislam/uptime-kuma:1",
		"node", "/usr/bin/dumb-init -- node server/server.js")
	hostNode := system.ProcessInfo{PID: 800, PPID: 1, Name: "node", Command: "node /srv/dashboard/index.js"}
	older := writtenBefore(t, inv, append(procs(hostNode), inv.Processes...))
	curr := buildSnapshot(inv)

	got := changeTargets(buildReport(curr, older))
	if len(got) != 1 || got[0] != "gone node" {
		t.Fatalf("the host node stopping should be the one line, got %v", got)
	}
}

// When a host process has exactly the command line of a containerised one,
// the two share an identity. It is still running on the host, so the
// transition must not drop it from the older side and call it new.
func TestAHostProcessIdenticalToAContainerProcessIsNotCalledNew(t *testing.T) {
	inv := demoHost("fe76a47604d2", "bba200f215ef", "louislam/uptime-kuma:1",
		"node", "/usr/bin/dumb-init -- node server/server.js")
	inv.Processes = append(inv.Processes, procs(
		system.ProcessInfo{PID: 800, PPID: 1, Name: "node", Command: "node server/server.js"},
	)...)
	older := writtenBefore(t, inv, inv.Processes)
	curr := buildSnapshot(inv)

	if got := changeTargets(buildReport(curr, older)); len(got) != 0 {
		t.Fatalf("nothing changed on the host, got %v", got)
	}
}

func watchHost(childCommand string, watching bool) *inventory.Inventory {
	list := []system.ProcessInfo{
		{PID: 700, PPID: 1, Name: "sshd", Command: "sshd: /usr/sbin/sshd -D"},
	}
	if watching {
		list = append(list,
			system.ProcessInfo{PID: 900, PPID: 1, Name: "homebutler", Command: "/usr/local/bin/homebutler watch start"},
			system.ProcessInfo{PID: 901, PPID: 900, Name: "docker", Command: childCommand},
		)
	}
	return &inventory.Inventory{ServerName: "homelab", Processes: procs(list...)}
}

// #295 added --filter event=oom to the watch process's docker events child,
// and the morning after the upgrade the report said `replaced: docker`.
func TestAnUpgradeThatChangesTheWatchChildIsNotReported(t *testing.T) {
	prev := fromDisk(t, buildSnapshot(watchHost("docker events --filter event=die --format {{json .}}", true)))
	curr := buildSnapshot(watchHost("docker events --filter event=die --filter event=oom --format {{json .}}", true))

	if got := changeTargets(buildReport(curr, prev)); len(got) != 0 {
		t.Fatalf("homebutler's own helper changing is not a change to the machine, got %v", got)
	}
}

// The watch process itself is the operator's, and it stopping is news.
func TestTheWatchProcessStoppingIsStillReported(t *testing.T) {
	prev := fromDisk(t, buildSnapshot(watchHost("docker events --filter event=die", true)))
	curr := buildSnapshot(watchHost("", false))

	got := changeTargets(buildReport(curr, prev))
	if len(got) != 1 || got[0] != "gone homebutler" {
		t.Fatalf("want only the watch process gone, got %v", got)
	}
}

// A parent chain that loops, which a recycled pid between the two ps calls
// can produce, must end rather than hang.
func TestALoopingParentChainEnds(t *testing.T) {
	kept, owned := splitOwnedProcesses(procs(
		system.ProcessInfo{PID: 10, PPID: 11, Name: "a"},
		system.ProcessInfo{PID: 11, PPID: 10, Name: "b"},
	))
	if len(kept) != 2 || len(owned) != 0 {
		t.Fatalf("a loop owns nothing, got kept=%d owned=%d", len(kept), len(owned))
	}
}
