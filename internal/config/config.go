package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path    string         `yaml:"-"` // resolved config file path (not serialized)
	Servers []ServerConfig `yaml:"servers"`
	Wake    []WakeTarget   `yaml:"wake,omitempty"`
	Alerts  AlertConfig    `yaml:"alerts"`
}

type ServerConfig struct {
	Name     string `yaml:"name"`
	Host     string `yaml:"host"`
	Local    bool   `yaml:"local,omitempty"`
	User     string `yaml:"user,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	KeyFile  string `yaml:"key,omitempty"`
	Password string `yaml:"password,omitempty"`
	AuthMode string `yaml:"auth,omitempty"` // "key" (default) or "password"
	BinPath  string `yaml:"bin,omitempty"`  // remote homebutler path (default: homebutler)
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

// Resolve finds the config file path using the following priority:
//  1. Explicit path (--config flag)
//  2. $HOMEBUTLER_CONFIG environment variable
//  3. ~/.config/homebutler/config.yaml (XDG standard)
//  4. ./homebutler.yaml (current directory)
//
// Returns empty string if no config file is found (defaults will be used).
func Resolve(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("HOMEBUTLER_CONFIG"); env != "" {
		return env
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdg := filepath.Join(home, ".config", "homebutler", "config.yaml")
		if _, err := os.Stat(xdg); err == nil {
			return xdg
		}
	}
	if _, err := os.Stat("homebutler.yaml"); err == nil {
		return "homebutler.yaml"
	}
	return ""
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Alerts: AlertConfig{
			CPU:    90,
			Memory: 85,
			Disk:   90,
		},
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

	cfg.Path = path
	return cfg, nil
}

// FindServer returns the server config by name, or nil if not found.
func (c *Config) FindServer(name string) *ServerConfig {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			return &c.Servers[i]
		}
	}
	return nil
}

// SSHPort returns the configured port or default 22.
func (s *ServerConfig) SSHPort() int {
	if s.Port > 0 {
		return s.Port
	}
	return 22
}

// SSHUser returns the configured user or default "root".
func (s *ServerConfig) SSHUser() string {
	if s.User != "" {
		return s.User
	}
	return "root"
}

// UseKeyAuth returns true if key-based auth should be used (default).
func (s *ServerConfig) UseKeyAuth() bool {
	return s.AuthMode != "password"
}

// SSHBinPath returns the remote homebutler binary path.
func (s *ServerConfig) SSHBinPath() string {
	if s.BinPath != "" {
		return s.BinPath
	}
	return "homebutler"
}

func (c *Config) FindWakeTarget(name string) *WakeTarget {
	for _, t := range c.Wake {
		if t.Name == name {
			return &t
		}
	}
	return nil
}
