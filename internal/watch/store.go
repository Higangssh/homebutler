package watch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The kinds of thing homebutler can watch. Spelled out here rather than as
// bare strings in each place that compares one, so adding a kind is a change
// in one file rather than a search.
const (
	KindDocker  = "docker"
	KindSystemd = "systemd"
	KindPM2     = "pm2"
)

// Kinds returns every valid target kind.
func Kinds() []string { return []string{KindDocker, KindSystemd, KindPM2} }

type Target struct {
	Container string    `json:"container"`
	Kind      string    `json:"kind,omitempty"` // "docker" | "systemd" | "pm2"
	Unit      string    `json:"unit,omitempty"` // actual container/service/app name (defaults to Container)
	AddedAt   time.Time `json:"added_at"`
}

// EffectiveKind returns the target kind, defaulting to "docker".
func (t Target) EffectiveKind() string {
	if t.Kind == "" {
		return KindDocker
	}
	return t.Kind
}

// CheckSupported reports whether a one-shot check can inspect this target.
//
// Docker records restart count and start time on the container itself, so a
// single inspect call is enough. Systemd and pm2 state is only meaningful when
// compared against a previous poll, which is what `watch start` does.
func (t Target) CheckSupported() bool {
	return t.EffectiveKind() == KindDocker
}

// EffectiveUnit returns the unit name, defaulting to Container.
func (t Target) EffectiveUnit() string {
	if t.Unit == "" {
		return t.Container
	}
	return t.Unit
}

type ContainerState struct {
	Container    string    `json:"container"`
	RestartCount int       `json:"restart_count"`
	StartedAt    string    `json:"started_at"`
	LastChecked  time.Time `json:"last_checked"`
}

type Incident struct {
	ID           string    `json:"id"`
	Container    string    `json:"container"`
	DetectedAt   time.Time `json:"detected_at"`
	RestartCount int       `json:"restart_count"`
	PrevStarted  string    `json:"prev_started_at"`
	CurrStarted  string    `json:"curr_started_at"`
	PreLogs      string    `json:"pre_logs"`
	PostLogs     string    `json:"post_logs"`
	// ExitCode is how the process ended, when the backend reported it. Nil
	// means it was not reported — zero is a real exit code, so the two cannot
	// share a representation (#108).
	ExitCode      *int            `json:"exit_code,omitempty"`
	OOMKilled     bool            `json:"oom_killed,omitempty"`
	Flapping      *FlappingResult `json:"flapping,omitempty"`
	CrashAnalysis *CrashSummary   `json:"crash_analysis,omitempty"`

	// Source distinguishes an incident that did not come from a restart
	// monitor. Empty means docker/systemd/pm2, as it always has; a non-empty
	// value opts the incident out of restart-shaped handling (crash analysis,
	// flapping) in cmd/watch.go's print loop.
	Source string `json:"source,omitempty"`
	// ProxmoxState is the state the incident transitioned into: "unavailable",
	// "acl_filtered", or "guest_down". Empty outside Source == "proxmox".
	ProxmoxState string `json:"proxmox_state,omitempty"`
	// ProxmoxClass is the specific failure behind ProxmoxState — tls,
	// authentication, authorization, transport, or empty_result — kept
	// alongside the coarser state per #104.
	ProxmoxClass string `json:"proxmox_class,omitempty"`
	// Recovered marks a transition back to healthy rather than into failure.
	Recovered bool `json:"recovered,omitempty"`
}

func WatchDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".homebutler", "watch"), nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func targetsPath(dir string) string {
	return filepath.Join(dir, "targets.json")
}

func statePath(dir string) string {
	return filepath.Join(dir, "state.json")
}

func incidentsDir(dir string) string {
	return filepath.Join(dir, "incidents")
}

func LoadTargets(dir string) ([]Target, error) {
	data, err := os.ReadFile(targetsPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var targets []Target
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("corrupt targets.json: %w", err)
	}
	// Validate kind values
	validKinds := map[string]bool{"": true}
	for _, k := range Kinds() {
		validKinds[k] = true
	}
	for i, t := range targets {
		if !validKinds[t.Kind] {
			fmt.Fprintf(os.Stderr, "warning: target %q has unknown kind %q, defaulting to docker\n", t.Container, t.Kind)
			targets[i].Kind = KindDocker
		}
	}
	return targets, nil
}

func SaveTargets(dir string, targets []Target) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(targetsPath(dir), data, 0o644)
}

func LoadState(dir string) (map[string]*ContainerState, error) {
	data, err := os.ReadFile(statePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*ContainerState), nil
		}
		return nil, err
	}
	var states map[string]*ContainerState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("corrupt state.json: %w", err)
	}
	return states, nil
}

func SaveState(dir string, states map[string]*ContainerState) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(dir), data, 0o644)
}

// SaveIncident writes an incident and then enforces the retention cap.
//
// keep is the number of incidents to retain; zero or less keeps everything.
// Pruning happens here rather than at the call sites because every monitor
// writes through this function, and a cap that each caller had to remember to
// apply would be a cap that one of them eventually forgets.
func SaveIncident(dir string, inc *Incident, keep int) error {
	idir := incidentsDir(dir)
	if err := ensureDir(idir); err != nil {
		return err
	}

	// Bounded here rather than at each capture site. There are five writers —
	// the three monitors, CheckTargets, and the enrichment re-save in
	// cmd/watch.go — and a cap each of them had to remember to apply is a cap
	// one of them eventually forgets. Same reasoning as the file count below.
	inc.PreLogs = TruncateLog(inc.PreLogs)
	inc.PostLogs = TruncateLog(inc.PostLogs)

	data, err := json.MarshalIndent(inc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(idir, inc.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}

	// The incident is already on disk. A failed prune is a housekeeping problem,
	// not a reason to tell the caller the incident was lost.
	if _, err := PruneIncidents(dir, keep); err != nil {
		fmt.Fprintf(os.Stderr, "warning: prune incidents: %v\n", err)
	}
	return nil
}

// PruneIncidents deletes the oldest incidents until at most keep remain, and
// reports how many it removed. keep of zero or less keeps everything.
//
// Files whose names do not fit the incident format are left alone:
// ListIncidentRefs skips them, so they are never selected for deletion.
// Refusing to delete a file we do not understand is the safer half of that
// trade, and the criterion moved from "cannot be unmarshalled" to "cannot be
// named" only because pruning no longer opens anything.
func PruneIncidents(dir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	refs, err := ListIncidentRefs(dir)
	if err != nil {
		return 0, err
	}
	if len(refs) <= keep {
		return 0, nil
	}

	idir := incidentsDir(dir)
	removed := 0
	for _, ref := range refs[keep:] { // ListIncidentRefs sorts newest first
		if err := os.Remove(filepath.Join(idir, ref.ID+".json")); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// MaxLogBytes bounds each captured log on an incident. docker logs --tail and
// journalctl -n bound the number of *lines*, which says nothing about their
// length: one stack trace, JSON document or base64 payload on a single line
// makes an incident file arbitrarily large, and a cap on the number of files
// does not bound a directory whose files have no size.
//
// The failure mode selects for itself. A container being OOM-killed is exactly
// the one likely to dump something enormous on the way out, which is the moment
// an incident gets written.
const MaxLogBytes = 64 << 10

// TruncateLog bounds a captured log, keeping the end. The tail is what explains
// a crash — the last thing a process said before it stopped — so a log too long
// to keep loses its beginning, and says so where a reader will see it.
func TruncateLog(s string) string {
	if len(s) <= MaxLogBytes {
		return s
	}
	dropped := len(s) - MaxLogBytes
	kept := s[dropped:]
	// Start at a line boundary so the first surviving line is not a fragment.
	if i := strings.IndexByte(kept, '\n'); i >= 0 && i < len(kept)-1 {
		dropped += i + 1
		kept = kept[i+1:]
	}
	return fmt.Sprintf("… truncated %d bytes …\n%s", dropped, kept)
}

// IncidentRef is what an incident's filename already tells us: which container
// it belongs to and when it was detected. GenerateIncidentID builds the name as
// container-timestamp-suffix, so pruning and flapping — the two things that run
// on every single save — can be answered without opening a file.
//
// This matters because those two used to call ListIncidents, which reads and
// unmarshals every incident in the directory including its captured logs. One
// incident cost two full reads of the history, and the moment that happens most
// often is a service restarting in a loop, which is the case flapping detection
// exists for.
type IncidentRef struct {
	ID         string
	Container  string
	DetectedAt time.Time
}

// parseIncidentRef recovers the container and time from an incident filename.
// A name that does not fit the format is not ours to interpret, so it is
// reported as unparseable rather than guessed at; callers skip those, which is
// also what keeps PruneIncidents from deleting a file it does not understand.
func parseIncidentRef(name string) (IncidentRef, bool) {
	id := strings.TrimSuffix(name, ".json")
	if id == name {
		return IncidentRef{}, false
	}
	// The suffix is 6 hex characters and the timestamp is fixed width, so the
	// container name is whatever precedes them, dashes and all.
	const tsLayout = "20060102-150405.000"
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		return IncidentRef{}, false
	}
	tsStr := parts[len(parts)-3] + "-" + parts[len(parts)-2]
	detected, err := time.Parse(tsLayout, tsStr)
	if err != nil {
		return IncidentRef{}, false
	}
	container := strings.Join(parts[:len(parts)-3], "-")
	if container == "" {
		return IncidentRef{}, false
	}
	return IncidentRef{ID: id, Container: container, DetectedAt: detected}, true
}

// ListIncidentRefs returns one ref per parseable incident file, newest first,
// without opening any of them.
func ListIncidentRefs(dir string) ([]IncidentRef, error) {
	entries, err := os.ReadDir(incidentsDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	refs := make([]IncidentRef, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ref, ok := parseIncidentRef(e.Name()); ok {
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].DetectedAt.After(refs[j].DetectedAt) })
	return refs, nil
}

func ListIncidents(dir string) ([]Incident, error) {
	idir := incidentsDir(dir)
	entries, err := os.ReadDir(idir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var incidents []Incident
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(idir, e.Name()))
		if err != nil {
			continue
		}
		var inc Incident
		if err := json.Unmarshal(data, &inc); err != nil {
			continue
		}
		incidents = append(incidents, inc)
	}

	sort.Slice(incidents, func(i, j int) bool {
		return incidents[i].DetectedAt.After(incidents[j].DetectedAt)
	})
	return incidents, nil
}

func LoadIncident(dir string, id string) (*Incident, error) {
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return nil, fmt.Errorf("invalid incident ID: %s", id)
		}
	}
	path := filepath.Join(incidentsDir(dir), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("incident %q not found", id)
		}
		return nil, err
	}
	var inc Incident
	if err := json.Unmarshal(data, &inc); err != nil {
		return nil, err
	}
	return &inc, nil
}

func GenerateIncidentID(container string, t time.Time) string {
	ts := t.Format("20060102-150405.000")
	// Add a short random suffix to avoid collisions
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	suffix := hex.EncodeToString(b)
	return container + "-" + ts + "-" + suffix
}
