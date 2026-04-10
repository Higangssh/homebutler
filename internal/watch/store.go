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

type Target struct {
	Container string    `json:"container"`
	Kind      string    `json:"kind,omitempty"` // "docker" | "systemd" | "pm2"
	Unit      string    `json:"unit,omitempty"` // actual container/service/app name (defaults to Container)
	AddedAt   time.Time `json:"added_at"`
}

// EffectiveKind returns the target kind, defaulting to "docker".
func (t Target) EffectiveKind() string {
	if t.Kind == "" {
		return "docker"
	}
	return t.Kind
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
	ID            string          `json:"id"`
	Container     string          `json:"container"`
	DetectedAt    time.Time       `json:"detected_at"`
	RestartCount  int             `json:"restart_count"`
	PrevStarted   string          `json:"prev_started_at"`
	CurrStarted   string          `json:"curr_started_at"`
	PreLogs       string          `json:"pre_logs"`
	PostLogs      string          `json:"post_logs"`
	Flapping      *FlappingResult `json:"flapping,omitempty"`
	CrashAnalysis *CrashSummary   `json:"crash_analysis,omitempty"`
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
	validKinds := map[string]bool{"": true, "docker": true, "systemd": true, "pm2": true}
	for i, t := range targets {
		if !validKinds[t.Kind] {
			fmt.Fprintf(os.Stderr, "warning: target %q has unknown kind %q, defaulting to docker\n", t.Container, t.Kind)
			targets[i].Kind = "docker"
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

func SaveIncident(dir string, inc *Incident) error {
	idir := incidentsDir(dir)
	if err := ensureDir(idir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(inc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(idir, inc.ID+".json")
	return os.WriteFile(path, data, 0o644)
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
