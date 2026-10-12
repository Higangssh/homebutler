package alerts

import (
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
)

func drain(ch chan Event) []string {
	var out []string
	for {
		select {
		case e := <-ch:
			out = append(out, e.Severity+" "+e.Resource)
		default:
			return out
		}
	}
}

// #338: a container a cron job stops and starts every night is watched like any
// other unless it is listed. Listed, neither its going down nor its recovery
// is reported; every other container still is.
func TestIgnoredContainersAreNotReportedEitherWay(t *testing.T) {
	cfg := WatchConfig{Alert: config.AlertConfig{Ignore: []string{"nightly-job"}}}
	prev := newPrevState()
	ch := make(chan Event, 16)
	now := time.Now()
	state := func(job, web string) []docker.Container {
		return []docker.Container{{Name: "nightly-job", State: job}, {Name: "web", State: web}}
	}

	emitContainerChanges(cfg, prev, state("running", "running"), now, ch)
	emitContainerChanges(cfg, prev, state("exited", "exited"), now, ch)
	emitContainerChanges(cfg, prev, state("running", "running"), now, ch)

	got := drain(ch)
	want := []string{"critical container:web", "ok container:web"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("want only web's down and recovery %v, got %v", want, got)
	}
}

// What the watcher already did before ignore existed, pinned so the change
// leaves it alone: a container that stays stopped is reported once, and again
// only after it has run in between.
func TestAStoppedContainerIsReportedOncePerStop(t *testing.T) {
	cfg := WatchConfig{}
	prev := newPrevState()
	ch := make(chan Event, 16)
	now := time.Now()
	for _, s := range []string{"running", "exited", "exited", "exited", "running", "exited"} {
		emitContainerChanges(cfg, prev, []docker.Container{{Name: "web", State: s}}, now, ch)
	}
	got := drain(ch)
	want := []string{"critical container:web", "ok container:web", "critical container:web"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}
