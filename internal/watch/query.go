package watch

import "strings"

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
		// A docker target watched under a unit has incidents under both of
		// its names; see dockerAliases. The list is best effort: without it
		// the name alone still filters.
		targets, _ := LoadTargets(dir)
		aliases := dockerAliases(targets, opts.Container)
		filtered := incidents[:0:0]
		for _, inc := range incidents {
			if strings.EqualFold(inc.Container, opts.Container) ||
				(aliases[inc.Container] && (inc.Kind == "" || inc.Kind == KindDocker)) {
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
