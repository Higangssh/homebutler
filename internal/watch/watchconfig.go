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
	Enabled    bool   `json:"enabled"`
	OnIncident bool   `json:"on_incident"`
	OnFlapping bool   `json:"on_flapping"`
	Cooldown   string `json:"cooldown"`
}

func DefaultWatchConfig() WatchConfig {
	return WatchConfig{
		Notify: NotifySettings{
			Enabled:    false,
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
	return &cfg, nil
}

func SaveWatchConfig(dir string, cfg *WatchConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(dir), data, 0644)
}
