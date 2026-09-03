package report

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

func TestRenderedChangeBlock(t *testing.T) {
	var prevC, currC []docker.Container
	prevC = append(prevC,
		docker.Container{ID: "aaa", Name: "vaultwarden", Image: "vaultwarden:1.32", State: "running"},
		docker.Container{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:1.27", State: "running"},
		docker.Container{ID: "jjj", Name: "jellyfin", Image: "jellyfin:10.9.11", State: "running"},
	)
	currC = append(currC,
		docker.Container{ID: "ccc", Name: "gitea", Image: "gitea:1.24", State: "running"},
		docker.Container{ID: "91be0322bb22", Name: "nginx", Image: "nginx:1.27", State: "running"},
		docker.Container{ID: "jjj", Name: "jellyfin", Image: "jellyfin:10.10.0", State: "running"},
	)
	for i := 0; i < 7; i++ {
		n := fmt.Sprintf("svc-%d", i)
		prevC = append(prevC, docker.Container{ID: n, Name: n, Image: "img", State: "exited"})
		currC = append(currC, docker.Container{ID: n, Name: n, Image: "img", State: "running"})
	}

	prev := snapshotWith(prevC, []ports.PortInfo{{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "vaultwarden"}})
	curr := snapshotWith(currC, []ports.PortInfo{{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "gitea"}})
	prev.Processes = processIdentities(procs(
		system.ProcessInfo{PID: 1, Name: "python3", Command: "python3 /opt/old.py"},
	))
	curr.Processes = processIdentities(procs(
		system.ProcessInfo{PID: 1, Name: "python3", Command: "python3 /opt/new.py"},
		system.ProcessInfo{PID: 2, Name: "borgbackup", Command: "borg create ::daily /data"},
	))
	prev.System = &system.StatusInfo{Disks: []system.DiskInfo{{Mount: "/", UsedGB: 45, TotalGB: 128}}}
	curr.System = &system.StatusInfo{Disks: []system.DiskInfo{{Mount: "/", UsedGB: 47.4, TotalGB: 128}}}
	curr.ServerName = "homelab"

	r := buildReport(curr, prev)
	human := FormatHuman(r)

	// The block README.md ships as assets/report-card.svg. If this changes,
	// the card is out of date — assets/README.md says to keep them in sync.
	want := []string{
		"gone      vaultwarden   was running",
		"new       gitea         now running",
		"replaced  nginx         recreated, 7d4a91f0aa11 → 91be0322bb22",
		"image     jellyfin      jellyfin:10.9.11 → jellyfin:10.10.0",
		"state     7 containers  svc-0, svc-1, svc-2, +4",
		"port      :8080/tcp     vaultwarden → gitea",
		"disk      /             +2.4 GB since last report",
		"new       borgbackup",
		"replaced  python3       same name, different invocation",
	}
	for _, line := range want {
		if !strings.Contains(human, line) {
			t.Errorf("rendered report is missing:\n  %q\ngot:\n%s", line, human)
		}
	}

	// Changes lead. Current Status is context, not the headline.
	if strings.Index(human, "Notable Changes") > strings.Index(human, "Current Status") {
		t.Error("Current Status is printed before Notable Changes")
	}
}
