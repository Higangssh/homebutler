package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// defaultMaxIncidents caps the incident directory by default. Each incident
// file carries up to 100 lines of captured logs, so an uncapped directory grows
// fastest exactly when something is wrong — a service restarting every 30s
// writes thousands of them a day.
const defaultMaxIncidents = 200

type WatchConfig struct {
	Notify    NotifySettings  `json:"notify"`
	Flapping  FlappingConfig  `json:"flapping"`
	Retention RetentionConfig `json:"retention"`
}

// RetentionConfig bounds how much incident history is kept on disk.
type RetentionConfig struct {
	// MaxIncidents is how many incidents to keep, newest first.
	//
	// Zero means "not configured" and takes the default, so that config files
	// written before this setting existed do not silently read as unlimited.
	// A negative value is the explicit way to ask for unlimited history.
	MaxIncidents int `yaml:"max_incidents,omitempty" json:"max_incidents"`
}

func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{MaxIncidents: defaultMaxIncidents}
}

// Normalize resolves the sentinel values into the number PruneIncidents wants,
// where zero or less means keep everything.
func (r *RetentionConfig) Normalize() {
	switch {
	case r.MaxIncidents < 0:
		r.MaxIncidents = 0
	case r.MaxIncidents == 0:
		r.MaxIncidents = defaultMaxIncidents
	}
}

type NotifySettings struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	NotifyOn   string `yaml:"notify_on,omitempty" json:"notify_on,omitempty"`
	OnIncident bool   `yaml:"on_incident,omitempty" json:"on_incident"`
	OnFlapping bool   `yaml:"on_flapping,omitempty" json:"on_flapping"`
	Cooldown   string `yaml:"cooldown" json:"cooldown"`
}

func DefaultWatchConfig() WatchConfig {
	return WatchConfig{
		Notify: NotifySettings{
			Enabled:    false,
			NotifyOn:   "flapping",
			OnIncident: false,
			OnFlapping: true,
			Cooldown:   "5m",
		},
		Flapping:  DefaultFlappingConfig(),
		Retention: DefaultRetentionConfig(),
	}
}

func configPath(dir string) string {
	return filepath.Join(dir, "config.json")
}

func LoadWatchConfig(dir string) (*WatchConfig, error) {
	data, err := os.ReadFile(configPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultWatchConfig()
			return &cfg, nil
		}
		return nil, err
	}

	var cfg WatchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Notify.Normalize()
	cfg.Retention.Normalize()
	return &cfg, nil
}

func SaveWatchConfig(dir string, cfg *WatchConfig) error {
	if cfg != nil {
		cfg.Notify.Normalize()
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(dir), data, 0644)
}

func (n *NotifySettings) Normalize() {
	if n.NotifyOn == "" {
		switch {
		case n.OnIncident && n.OnFlapping:
			n.NotifyOn = "all"
		case n.OnIncident:
			n.NotifyOn = "incident"
		case n.OnFlapping:
			n.NotifyOn = "flapping"
		default:
			n.NotifyOn = "off"
		}
	}

	switch n.NotifyOn {
	case "all":
		n.OnIncident = true
		n.OnFlapping = true
	case "incident":
		n.OnIncident = true
		n.OnFlapping = false
	case "flapping":
		n.OnIncident = false
		n.OnFlapping = true
	case "off":
		n.OnIncident = false
		n.OnFlapping = false
		if !n.Enabled {
			return
		}
	default:
		n.NotifyOn = "flapping"
		n.OnIncident = false
		n.OnFlapping = true
	}
}
