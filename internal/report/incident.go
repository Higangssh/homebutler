package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/watch"
)

// IncidentContext is a watch incident with what report saw change around it.
//
// An agent asked why something stopped at 03:12 had to call watch_history,
// then report, then decide for itself which changes came before 03:12 and
// which of them touched the container. The second half is the part it could
// only guess at: a change line names a port or a container, and which
// container a port belongs to is something the snapshot knows and the
// sentence does not say.
type IncidentContext struct {
	Incident watch.Incident `json:"incident"`

	// Compared is false when no snapshot was taken before the incident, or
	// none could be read. The two lists are then empty because nothing was
	// compared, not because nothing changed, and Note says which.
	Compared bool `json:"compared"`
	// Before is the timestamp of the newest snapshot taken before the
	// incident was detected.
	Before string `json:"before,omitempty"`
	// After is what Before was compared with: the first snapshot taken after
	// the incident, or, when there is none, the moment the current state was
	// read for this answer. AfterIsCurrent says which, because the current
	// state is not the machine at the moment of the crash, and a change since
	// then can be the fix rather than the cause.
	After          string `json:"after,omitempty"`
	AfterIsCurrent bool   `json:"after_is_current"`

	// AboutTarget is the changes to the incident's own container and to the
	// ports it published, decided from the snapshots rather than from the
	// text. OtherChanges is everything else. A skipped line for containers or
	// ports, a collector that did not answer, is in both: it qualifies both,
	// and an empty AboutTarget beside a skipped containers line elsewhere
	// would read as nothing having happened to the container.
	AboutTarget  []ChangeLine `json:"about_target"`
	OtherChanges []ChangeLine `json:"other_changes"`

	Note string `json:"note,omitempty"`
}

// ContextOptions says where the snapshots are and how to read the current
// state when no snapshot follows the incident.
type ContextOptions struct {
	SnapshotDir string // override snapshot directory (for testing)
	// Current reads the machine now. Nil means it cannot be read here, as
	// inside a container, and an incident with no snapshot after it is then
	// returned uncompared.
	Current func() (*Snapshot, error)
	// Container says whether the incident's target is a container. Changes
	// can only be tied to a target by container identity, so for a systemd
	// unit or a pm2 app every change is an other change.
	Container bool
}

// CurrentState returns a Current that collects inventory the way Run does,
// without saving or pruning anything.
func CurrentState(cfg *config.Config, fns CollectFuncs) func() (*Snapshot, error) {
	return func() (*Snapshot, error) {
		var readAt time.Time
		if collect := fns.ProcessesFn; collect != nil {
			fns.ProcessesFn = func() ([]system.ProcessInfo, error) {
				readAt = now()
				return collect()
			}
		}
		inv, err := inventory.Collect(cfg, fns)
		if err != nil {
			return nil, fmt.Errorf("collecting inventory: %w", err)
		}
		snap := buildSnapshot(inv)
		if !readAt.IsZero() && !snap.collectorFailed(inventory.CollectorProcesses) {
			snap.ProcessesReadAt = readAt.UTC().Format(time.RFC3339Nano)
		}
		return snap, nil
	}
}

// ContextFor is what `watch show --context` and the incident_context tool
// answer: the incident with this ID, or the latest one for target, placed
// between this machine's snapshots. Logs are left out unless asked for, as
// watch_history leaves them out. Inside a container the current state is the
// container's, so it is not read there.
func ContextFor(cfg *config.Config, watchDir, id, target string, logs bool) (*IncidentContext, error) {
	inc, err := watch.FindIncident(watchDir, id, target)
	if err != nil {
		return nil, err
	}
	if !logs {
		*inc = inc.WithoutLogs()
	}
	opts := ContextOptions{Container: watch.IsContainerIncident(watchDir, *inc)}
	if !system.InContainer() {
		opts.Current = CurrentState(cfg, DefaultCollectFuncs())
	}
	return Context(*inc, opts)
}

// Context places an incident between the snapshots around it.
//
// Before is the newest snapshot strictly before the incident was detected.
// After is the one right after it, so the changes are the ones the report
// taken next printed, and not everything that has happened since. Only when
// no report has run since the incident is the current state read instead.
func Context(inc watch.Incident, opts ContextOptions) (*IncidentContext, error) {
	out := &IncidentContext{
		Incident:     inc,
		AboutTarget:  []ChangeLine{},
		OtherChanges: []ChangeLine{},
	}
	dir := opts.SnapshotDir
	if dir == "" {
		dir = defaultSnapshotDir()
	}

	beforeFile, afterFile, err := snapshotsAround(dir, inc.DetectedAt)
	if err != nil {
		return nil, err
	}
	if beforeFile == "" {
		out.Note = "No snapshot was taken before this incident, so there is nothing to compare it with. Run report on a schedule so the next incident has one."
		return out, nil
	}
	before, err := readSnapshot(filepath.Join(dir, beforeFile))
	if err != nil {
		out.Note = "The snapshot taken before this incident could not be read, so nothing was compared: " + err.Error()
		return out, nil
	}

	var after *Snapshot
	switch {
	case afterFile != "":
		after, err = readSnapshot(filepath.Join(dir, afterFile))
		if err != nil {
			out.Note = "The snapshot taken after this incident could not be read, so nothing was compared: " + err.Error()
			return out, nil
		}
	case opts.Current == nil:
		out.Before = before.Timestamp
		out.Note = "No snapshot was taken after this incident, and the current state cannot be read from here, so nothing was compared."
		return out, nil
	default:
		after, err = opts.Current()
		if err != nil {
			return nil, err
		}
		out.AfterIsCurrent = true
	}

	out.Compared = true
	out.Before = before.Timestamp
	out.After = after.Timestamp
	if out.AfterIsCurrent {
		out.Note = "No snapshot was taken after this incident, so these changes run from the snapshot before it to the current state, read now — not to the moment it was detected. A change listed here may have come after the incident, including whatever fixed it."
	}

	r := buildReport(after, before)
	owned := portOwners(before, after)
	for _, c := range r.changes {
		line := c.line()
		switch {
		case c.Kind == kindSkipped:
			if opts.Container && (c.noun == nounContainers || c.noun == nounPorts) {
				out.AboutTarget = append(out.AboutTarget, line)
			}
			out.OtherChanges = append(out.OtherChanges, line)
		case opts.Container && aboutContainer(c, inc.Container, owned):
			out.AboutTarget = append(out.AboutTarget, line)
		default:
			out.OtherChanges = append(out.OtherChanges, line)
		}
	}
	return out, nil
}

// aboutContainer is whether a change is about the named container: the
// container itself, by name as diffContainers matches it, or a listener it
// published, by the attribution inventory made when the snapshot was taken.
func aboutContainer(c Change, name string, owners map[string]map[string]bool) bool {
	switch c.noun {
	case nounContainers:
		return c.Subject == name
	case nounPorts:
		return owners[c.Subject][name]
	}
	return false
}

// portOwners maps each listener's display subject to the containers that
// published it in either snapshot. A port that changed hands belongs to both
// sides, and a subject covers both address families Docker binds.
func portOwners(snaps ...*Snapshot) map[string]map[string]bool {
	owners := map[string]map[string]bool{}
	for _, s := range snaps {
		for _, p := range s.Ports {
			name, _, _ := strings.Cut(p.Container, " (")
			if name == "" {
				continue
			}
			subject := portSubject(p)
			if owners[subject] == nil {
				owners[subject] = map[string]bool{}
			}
			owners[subject][name] = true
		}
	}
	return owners
}

// snapshotsAround names the newest snapshot file stamped strictly before at
// and the first stamped at or after it. The stamp is read from the file name,
// which saveSnapshot writes from the snapshot's own timestamp, so finding the
// two does not mean reading the thirty.
func snapshotsAround(dir string, at time.Time) (before, after string, err error) {
	files, err := listSnapshotFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	for _, f := range files {
		stamp := strings.TrimSuffix(strings.TrimPrefix(f, "snapshot_"), ".json")
		t, perr := time.Parse("20060102T150405Z", stamp)
		if perr != nil {
			continue
		}
		if t.Before(at) {
			before = f
			continue
		}
		after = f
		break
	}
	return before, after, nil
}

func readSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &snap, nil
}
