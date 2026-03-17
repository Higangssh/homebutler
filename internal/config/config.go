package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path      string         `yaml:"-"` // resolved config file path (not serialized)
	Servers   []ServerConfig `yaml:"servers"`
	Wake      []WakeTarget   `yaml:"wake,omitempty"`
	Alerts    AlertConfig    `yaml:"alerts"`
	BackupDir string         `yaml:"backup_dir,omitempty"`
}

// ResolveBackupDir returns the backup directory from config or the default ~/.homebutler/backups/.
func (c *Config) ResolveBackupDir() string {
	if c.BackupDir != "" {
		return c.BackupDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".homebutler/backups"
	}
	return filepath.Join(home, ".homebutler", "backups")
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

	if path == "" {
		return cfg, nil // no config file, use defaults
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

	// Warn if config contains passwords and file permissions are too open (non-Windows).
	if runtime.GOOS != "windows" && hasSecrets(cfg) {
		if info, err := os.Stat(path); err == nil {
			perm := info.Mode().Perm()
			if perm&0o077 != 0 {
				fmt.Fprintf(os.Stderr, "⚠️  Config file %s contains passwords but has open permissions (%04o).\n", path, perm)
				fmt.Fprintf(os.Stderr, "   Run: chmod 600 %s\n\n", path)
			}
		}
	}

	return cfg, nil
}

// hasSecrets returns true if any server uses password auth.
func hasSecrets(cfg *Config) bool {
	for _, s := range cfg.Servers {
		if s.Password != "" {
			return true
		}
	}
	return false
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

// ValidationError describes a single configuration validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate checks the config for missing required fields and invalid values.
// It returns all validation errors found (not just the first).
func (c *Config) Validate() []ValidationError {
	var errs []ValidationError

	for i, s := range c.Servers {
		prefix := fmt.Sprintf("servers[%d]", i)
		if s.Name == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".name",
				Message: "required field is missing",
			})
		}
		if !s.Local && s.Host == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".host",
				Message: "required for remote servers",
			})
		}
		if s.Port != 0 && (s.Port < 1 || s.Port > 65535) {
			errs = append(errs, ValidationError{
				Field:   prefix + ".port",
				Message: fmt.Sprintf("must be between 1 and 65535, got %d", s.Port),
			})
		}
		if s.KeyFile != "" {
			if _, err := os.Stat(s.KeyFile); os.IsNotExist(err) {
				errs = append(errs, ValidationError{
					Field:   prefix + ".key",
					Message: fmt.Sprintf("file not found: %s", s.KeyFile),
				})
			}
		}
	}

	for i, w := range c.Wake {
		prefix := fmt.Sprintf("wake[%d]", i)
		if w.Name == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".name",
				Message: "required field is missing",
			})
		}
		if w.MAC == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".mac",
				Message: "required field is missing",
			})
		}
	}

	return errs
}

func (c *Config) FindWakeTarget(name string) *WakeTarget {
	for _, t := range c.Wake {
		if t.Name == name {
			return &t
		}
	}
	return nil
}
