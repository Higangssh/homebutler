package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type WatchConfig struct {
	Notify   NotifySettings `json:"notify"`
	Flapping FlappingConfig `json:"flapping"`
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
		Flapping: DefaultFlappingConfig(),
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
