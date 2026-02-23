package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Servers []ServerConfig `yaml:"servers"`
	Wake    []WakeTarget   `yaml:"wake"`
	Alerts  AlertConfig    `yaml:"alerts"`
	Output  string         `yaml:"output"`
}

type ServerConfig struct {
	Name string `yaml:"name"`
	Host string `yaml:"host"`
	SSH  string `yaml:"ssh,omitempty"`
}

type WakeTarget struct {
	Name      string `yaml:"name"`
	MAC       string `yaml:"mac"`
	Broadcast string `yaml:"ip,omitempty"`
}

type AlertConfig struct {
	CPU    float64 `yaml:"cpu"`
	Memory float64 `yaml:"memory"`
	Disk   float64 `yaml:"disk"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Alerts: AlertConfig{
			CPU:    90,
			Memory: 85,
			Disk:   90,
		},
		Output: "json",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // use defaults
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

func (c *Config) FindWakeTarget(name string) *WakeTarget {
	for _, t := range c.Wake {
		if t.Name == name {
			return &t
		}
	}
	return nil
}
