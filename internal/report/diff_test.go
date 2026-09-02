package report

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
)

func snapshotWith(containers []docker.Container, listeners []ports.PortInfo) *Snapshot {
	s := &Snapshot{Containers: containers, Ports: listeners}
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
	if !strings.Contains(got, "replaced: nginx — 7d4a91f0aa11 → 91be0322bb22") {
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
	if len(r.Warnings) == 0 {
		t.Error("skipping the container diff was not reported to the operator")
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
