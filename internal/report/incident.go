package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

	// AnchoredAt is the moment the window is placed around, and AnchoredOn
	// the incident field it came from: curr_started_at when the incident has
	// one earlier than detected_at, otherwise detected_at. `watch check`
	// stamps detected_at when it runs, which can be long after the restart
	// and after a report that already saw the restarted container; the new
	// run's start time is no later than the crash's aftermath and is what
	// the monitor itself read.
	AnchoredAt string `json:"anchored_at"`
	AnchoredOn string `json:"anchored_on"`

	// Compared is false when nothing was compared: no snapshot before the
	// incident, one that could not be read, or no snapshot after it and no
	// current state to read. The two lists are then empty because nothing was
	// compared, not because nothing changed, and Note says which.
	Compared bool `json:"compared"`
	// Before is the timestamp of the newest snapshot taken before the anchor,
	// set whenever one was found, read or not.
	Before string `json:"before,omitempty"`
	// After is what Before was compared with: the first snapshot taken after
	// the anchor, or, when there is none, the moment the current state was
	// read for this answer. AfterIsCurrent says which.
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
	// Unattributed says why AboutTarget is empty by construction: the
	// incident is not known to be about a Docker container, so no change is
	// tied to it and every change is in OtherChanges.
	Unattributed string `json:"unattributed,omitempty"`

	// Note says what the comparison is and is not. Whenever something was
	// compared it says the window runs past the incident, because every
	// change between the two snapshots is listed whether it came before the
	// crash or after.
	Note string `json:"note,omitempty"`
	// Warnings are incident files that could not be read while looking for
	// a target's latest incident, one of which may be newer than this one.
	Warnings []string `json:"warnings,omitempty"`
}

// ContextOptions says where the snapshots are, how to read the current state
// when no snapshot follows the incident, and which container the incident is
// about.
type ContextOptions struct {
	SnapshotDir string // override snapshot directory (for testing)
	// Current reads the machine now. Nil means it is not read, and an
	// incident with no snapshot after it is then returned uncompared.
	Current func() (*Snapshot, error)
	// InContainer is that this binary runs in a container, where the current
	// state would be the container's and report is not run.
	InContainer bool
	// Container is the Docker name the incident is about, or empty when that
	// is not known for certain, with Unattributed saying why.
	Container    string
	Unattributed string
	// Inferred is a sentence saying the kind behind Container was read from
	// the watch list as it is now, for an incident that does not record it.
	// It is added to the note.
	Inferred string
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
	inc, warnings, err := watch.FindIncident(watchDir, id, target)
	if err != nil {
		return nil, err
	}
	if !logs {
		*inc = inc.WithoutLogs()
	}
	who := watch.IncidentContainer(watchDir, *inc)
	opts := ContextOptions{InContainer: system.InContainer(), Container: who.Container, Unattributed: who.Why, Inferred: who.Inferred}
	if !opts.InContainer {
		opts.Current = CurrentState(cfg, DefaultCollectFuncs())
	}
	out, err := Context(*inc, opts)
	if out != nil {
		out.Warnings = warnings
	}
	return out, err
}

// incidentAnchor is the moment an incident is placed at, and the field it
// came from. prev_started_at is not a candidate: it is when the run that
// died started, so the crash came after it, and a window placed there would
// end before the crash. curr_started_at equal to it is that same start, not
// a new run: the systemd monitor wrote it that way for a unit that failed
// without starting again, and incident files from then still carry it.
func incidentAnchor(inc watch.Incident) (time.Time, string) {
	at, on := inc.DetectedAt, "detected_at"
	t, err := time.Parse(time.RFC3339Nano, inc.CurrStarted)
	if err != nil || t.Year() <= 1 || !t.Before(at) {
		return at, on
	}
	if prev, err := time.Parse(time.RFC3339Nano, inc.PrevStarted); err == nil && prev.Equal(t) {
		return at, on
	}
	return t, "curr_started_at"
}

// Context places an incident between the snapshots around it.
//
// Before is the newest snapshot stamped before the anchor's second. After is
// the one right after it, so the changes are the ones the report taken next
// printed, and not everything that has happened since. Only when no report
// has run since the incident is the current state read instead.
func Context(inc watch.Incident, opts ContextOptions) (*IncidentContext, error) {
	out, err := placeIncident(inc, opts)
	if out != nil && opts.Inferred != "" {
		out.Note = strings.TrimSpace(out.Note + " " + opts.Inferred)
	}
	return out, err
}

func placeIncident(inc watch.Incident, opts ContextOptions) (*IncidentContext, error) {
	anchor, anchoredOn := incidentAnchor(inc)
	out := &IncidentContext{
		Incident:     inc,
		AnchoredAt:   anchor.UTC().Format(time.RFC3339Nano),
		AnchoredOn:   anchoredOn,
		AboutTarget:  []ChangeLine{},
		OtherChanges: []ChangeLine{},
		Unattributed: opts.Unattributed,
	}
	if opts.Container == "" && out.Unattributed == "" {
		out.Unattributed = "the incident is not known to be about a container"
	}
	dir := opts.SnapshotDir
	if dir == "" {
		dir = defaultSnapshotDir()
	}

	w, err := snapshotsAround(dir, anchor)
	if err != nil {
		return nil, fmt.Errorf("listing report snapshots: %w", withoutPath(err))
	}
	switch {
	case w.before == "" && w.oldest == "":
		out.Note = "No report snapshot exists here, so there is nothing to compare this incident with."
		if !opts.InContainer {
			out.Note += " Run report on a schedule so the next incident has one."
		}
		return out, nil
	case w.before == "":
		out.Note = "No snapshot kept here is older than this incident: the oldest is from " + stampTime(w.oldest) + ". Either report had not run before it, or the snapshots from before it were pruned; report keeps the newest --keep of them, 30 by default."
		return out, nil
	}
	out.Before = stampTime(w.before)
	if w.after != "" {
		out.After = stampTime(w.after)
	}
	before, err := readSnapshot(filepath.Join(dir, w.before))
	if err != nil {
		out.Note = "The snapshot from " + out.Before + ", the last before this incident, could not be read, so nothing was compared: " + err.Error()
		return out, nil
	}

	var after *Snapshot
	switch {
	case w.after != "":
		after, err = readSnapshot(filepath.Join(dir, w.after))
		if err != nil {
			out.Note = "The snapshot from " + out.After + ", the first after this incident, could not be read, so nothing was compared: " + err.Error()
			return out, nil
		}
	case opts.Current == nil:
		out.Note = "No snapshot was taken after this incident, and the current state is not read here, so nothing was compared."
		if opts.InContainer {
			out.Note = "No snapshot was taken after this incident, and inside a container the current state would be the container's, so nothing was compared."
		}
		return out, nil
	default:
		after, err = opts.Current()
		if err != nil {
			return nil, err
		}
		out.AfterIsCurrent = true
		out.After = after.Timestamp
	}

	out.Compared = true
	if out.AfterIsCurrent {
		out.Note = "No snapshot was taken after this incident, so these changes run from the snapshot before it to the current state, read now — not to the moment of the incident. A change listed here may have come after the incident, including whatever fixed it."
	} else {
		out.Note = "These are the changes between the report at " + out.Before + " and the next one at " + out.After + ", around the incident at " + out.AnchoredAt + " (" + anchoredOn + "). Any of them may have happened after the incident rather than before it."
	}

	r := buildReport(after, before)
	owned := portOwners(before, after)
	for _, c := range r.changes {
		line := c.line()
		switch {
		case c.Kind == kindSkipped:
			if opts.Container != "" && (c.noun == nounContainers || c.noun == nounPorts) {
				out.AboutTarget = append(out.AboutTarget, line)
			}
			out.OtherChanges = append(out.OtherChanges, line)
		case opts.Container != "" && aboutContainer(c, opts.Container, owned):
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

// window is the snapshot files around an anchor, and the oldest kept.
type window struct {
	before, after, oldest string
}

const snapshotStamp = "20060102T150405Z"

// snapshotsAround names the newest snapshot file stamped before the anchor
// and the first stamped after it. The stamp is read from the file name, which
// saveSnapshot writes from the snapshot's own timestamp, so finding the two
// does not mean reading the thirty.
//
// The stamp is to the second and the anchor is not. A snapshot stamped in the
// anchor's own second may have been taken after it, so it is not counted as
// before: it is the after side.
func snapshotsAround(dir string, anchor time.Time) (window, error) {
	var w window
	files, err := listSnapshotFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return w, nil
		}
		return w, err
	}
	cutoff := anchor.Truncate(time.Second)
	for _, f := range files {
		t, ok := fileStamp(f)
		if !ok {
			continue
		}
		if w.oldest == "" {
			w.oldest = f
		}
		if t.Before(cutoff) {
			w.before = f
			continue
		}
		w.after = f
		break
	}
	return w, nil
}

func fileStamp(name string) (time.Time, bool) {
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, "snapshot_"), ".json")
	t, err := time.Parse(snapshotStamp, stamp)
	return t, err == nil
}

// stampTime is a snapshot file's stamp in the form its timestamp field has.
func stampTime(name string) string {
	t, _ := fileStamp(name)
	return t.UTC().Format(time.RFC3339)
}

// readSnapshot reads one snapshot. Its errors leave the path out: they go
// into an answer that may be relayed anywhere, and the directory is the
// operator's home.
func readSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, withoutPath(err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func withoutPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
