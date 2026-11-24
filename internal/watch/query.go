package watch

import (
	"fmt"
	"strings"
)

// WatchedTarget is a target joined with whatever the last poll recorded about
// it. `watch list` printed that join as a table and had no --json, so an agent
// asking what is being watched had to be told to read a table. This is the
// shape both paths now return.
type WatchedTarget struct {
	Container    string `json:"container"`
	Kind         string `json:"kind"`
	AddedAt      string `json:"added_at"`
	RestartCount int    `json:"restart_count"`
	LastChecked  string `json:"last_checked,omitempty"`
}

// ListWatched returns the watch list joined with the recorded state. A target
// with no state yet is still returned: it is being watched, it simply has not
// been polled.
func ListWatched(dir string) ([]WatchedTarget, error) {
	targets, err := LoadTargets(dir)
	if err != nil {
		return nil, err
	}
	// State is best effort. A missing or unreadable state file means nothing
	// has been polled yet, which is not a reason to refuse to list the targets.
	states, _ := LoadState(dir)

	out := make([]WatchedTarget, 0, len(targets))
	for _, t := range targets {
		w := WatchedTarget{
			Container: t.Container,
			Kind:      t.EffectiveKind(),
			AddedAt:   t.AddedAt.Format("2006-01-02 15:04:05"),
		}
		if s, ok := states[t.Container]; ok {
			w.RestartCount = s.RestartCount
			if !s.LastChecked.IsZero() {
				w.LastChecked = s.LastChecked.Format("2006-01-02 15:04:05")
			}
		}
		out = append(out, w)
	}
	return out, nil
}

// HistoryOptions narrows what ListIncidents returns.
//
// Logs are excluded unless asked for. Every incident carries PreLogs and
// PostLogs, which are a hundred lines of container output each; thirty
// incidents of that buries the caller in text it did not ask for. The caller
// opts in to the expensive shape.
type HistoryOptions struct {
	Limit     int    // most recent N, 0 for all
	Container string // exact container name, empty for all
	Logs      bool   // include PreLogs and PostLogs
}

// History returns recorded incidents, newest first, narrowed by opts.
func History(dir string, opts HistoryOptions) ([]Incident, error) {
	incidents, err := ListIncidents(dir)
	if err != nil {
		return nil, err
	}

	if opts.Container != "" {
		filtered := incidents[:0:0]
		for _, inc := range incidents {
			if strings.EqualFold(inc.Container, opts.Container) {
				filtered = append(filtered, inc)
			}
		}
		incidents = filtered
	}

	// ListIncidents already sorts newest first, so a limit keeps the incidents
	// most likely to matter. TestHistoryKeepsTheNewestIncidents pins that: if
	// the sort there ever flips, a limit would silently return the oldest.
	if opts.Limit > 0 && len(incidents) > opts.Limit {
		incidents = incidents[:opts.Limit]
	}

	if !opts.Logs {
		// Copy before blanking: ListIncidents returns freshly parsed values
		// today, but a caller should not have to know that to be safe from
		// having the logs removed underneath it.
		stripped := make([]Incident, len(incidents))
		for i, inc := range incidents {
			stripped[i] = inc.WithoutLogs()
		}
		incidents = stripped
	}

	return incidents, nil
}

// WithoutLogs is the incident as History returns it when logs were not asked
// for. A proxmox incident keeps PostLogs, because that is where its one line
// of explanation lives rather than captured output.
func (inc Incident) WithoutLogs() Incident {
	inc.PreLogs = ""
	if inc.Source != "proxmox" {
		inc.PostLogs = ""
	}
	return inc
}

// FindIncident returns the incident with this ID or, given a target instead,
// the latest incident recorded for it. Exactly one of the two is expected.
func FindIncident(dir, id, target string) (*Incident, error) {
	switch {
	case id != "" && target != "":
		return nil, fmt.Errorf("give an incident ID or a target, not both")
	case id != "":
		return LoadIncident(dir, id)
	case target == "":
		return nil, fmt.Errorf("an incident ID or a target is required")
	}
	latest, err := History(dir, HistoryOptions{Container: target, Limit: 1, Logs: true})
	if err != nil {
		return nil, err
	}
	if len(latest) == 0 {
		return nil, fmt.Errorf("no incident recorded for %q", target)
	}
	return &latest[0], nil
}

// IsContainerIncident reports whether the incident is about a Docker
// container, which is the only kind of target a report change can be tied to.
// A target removed from the list since is taken to have been a container,
// docker being the kind a target has when none is given.
func IsContainerIncident(dir string, inc Incident) bool {
	if inc.Source != "" {
		return false
	}
	targets, _ := LoadTargets(dir)
	for _, t := range targets {
		if t.Container == inc.Container || t.EffectiveUnit() == inc.Container {
			return t.EffectiveKind() == KindDocker
		}
	}
	return true
}
