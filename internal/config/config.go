package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/watch"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Path      string                `yaml:"-"` // resolved config file path (not serialized)
	Servers   []ServerConfig        `yaml:"servers"`
	Proxmox   []ProxmoxConfig       `yaml:"proxmox,omitempty"`
	Wake      []WakeTarget          `yaml:"wake,omitempty"`
	Alerts    AlertConfig           `yaml:"alerts"`
	Notify    notify.ProviderConfig `yaml:"notify,omitempty"`
	Watch     WatchRuntimeConfig    `yaml:"watch,omitempty"`
	BackupDir string                `yaml:"backup_dir,omitempty"`
}

type WatchRuntimeConfig struct {
	Notify    watch.NotifySettings  `yaml:"notify,omitempty"`
	Flapping  watch.FlappingConfig  `yaml:"flapping,omitempty"`
	Retention watch.RetentionConfig `yaml:"retention,omitempty"`
}

// watchRuntimeYAML is the decode target for WatchRuntimeConfig. It carries the
// canonical nested shape plus the flat keys, as pointers so that "absent" and
// "set to the zero value" stay distinguishable.
type watchRuntimeYAML struct {
	Notify    watch.NotifySettings  `yaml:"notify,omitempty"`
	Flapping  watch.FlappingConfig  `yaml:"flapping,omitempty"`
	Retention watch.RetentionConfig `yaml:"retention,omitempty"`

	Enabled    *bool   `yaml:"enabled,omitempty"`
	NotifyOn   *string `yaml:"notify_on,omitempty"`
	OnIncident *bool   `yaml:"on_incident,omitempty"`
	OnFlapping *bool   `yaml:"on_flapping,omitempty"`
	Cooldown   *string `yaml:"cooldown,omitempty"`
}

// UnmarshalYAML accepts both spellings of the watch notification settings:
//
//	watch:            # canonical
//	  notify:
//	    enabled: true
//
//	watch:            # flat, as the README documented it
//	  enabled: true
//
// The flat keys were never part of the schema, so a config copied from the
// README parsed without complaint and then ran with notifications off — the
// same symptom as #31, from a different cause. Correcting only the docs would
// have left those configs silently broken, so both spellings are read.
//
// A file containing both forms gets the nested block; the flat keys are a
// compatibility path, not an override.
func (w *WatchRuntimeConfig) UnmarshalYAML(node *yaml.Node) error {
	// Seeded with the current value so that defaults applied before decoding
	// survive keys the file does not mention.
	raw := watchRuntimeYAML{Notify: w.Notify, Flapping: w.Flapping, Retention: w.Retention}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	w.Notify = raw.Notify
	w.Flapping = raw.Flapping
	w.Retention = raw.Retention

	if hasMappingKey(node, "notify") {
		return nil
	}
	if raw.Enabled != nil {
		w.Notify.Enabled = *raw.Enabled
	}
	if raw.NotifyOn != nil {
		w.Notify.NotifyOn = *raw.NotifyOn
	}
	if raw.OnIncident != nil {
		w.Notify.OnIncident = *raw.OnIncident
	}
	if raw.OnFlapping != nil {
		w.Notify.OnFlapping = *raw.OnFlapping
	}
	if raw.Cooldown != nil {
		w.Notify.Cooldown = *raw.Cooldown
	}
	return nil
}

// hasMappingKey reports whether a YAML mapping contains the given key.
func hasMappingKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
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
	Password string `yaml:"password,omitempty" json:"-" secret:"true"`
	AuthMode string `yaml:"auth,omitempty"` // "key" (default) or "password"
	BinPath  string `yaml:"bin,omitempty"`  // remote homebutler path (default: homebutler)
}

// ProxmoxConfig describes one Proxmox VE API endpoint. It is separate from
// ServerConfig because Proxmox uses its HTTP API rather than SSH.
type ProxmoxConfig struct {
	Name        string `yaml:"name"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port,omitempty"`
	TokenID     string `yaml:"token_id"`
	Token       string `yaml:"token,omitempty" json:"-" secret:"true"`
	TokenFile   string `yaml:"token_file,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty"`
	CAFile      string `yaml:"ca_file,omitempty"`
	Insecure    bool   `yaml:"insecure,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
}

// APIPort returns the configured API port or Proxmox's default HTTPS port.
func (p ProxmoxConfig) APIPort() int {
	if p.Port != 0 {
		return p.Port
	}
	return 8006
}

// TimeoutDuration returns the configured timeout or the default 10 seconds.
// Validation reports malformed values before a client is constructed.
func (p ProxmoxConfig) TimeoutDuration() time.Duration {
	if p.Timeout != "" {
		if timeout, err := time.ParseDuration(p.Timeout); err == nil && timeout > 0 {
			return timeout
		}
	}
	return 10 * time.Second
}

// TokenFilePath expands a leading ~/ in the configured token file path.
func (p ProxmoxConfig) TokenFilePath() string {
	return expandHome(p.TokenFile)
}

// TokenValue returns the API token from the configured file or inline value.
// Validation rejects configuring both sources, but the file takes precedence
// here to keep token_file the preferred source for direct callers.
func (p ProxmoxConfig) TokenValue() (string, error) {
	if p.TokenFile == "" {
		if p.Token == "" {
			return "", fmt.Errorf("proxmox token is not configured")
		}
		return p.Token, nil
	}

	data, err := os.ReadFile(p.TokenFilePath())
	if err != nil {
		return "", fmt.Errorf("read Proxmox token file %s: %w", p.TokenFile, err)
	}
	if token := strings.TrimSpace(string(data)); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("proxmox token file %s is empty", p.TokenFile)
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

// Source identifies which resolution rule produced the config path.
type Source string

const (
	SourceFlag Source = "flag" // --config
	SourceEnv  Source = "env"  // $HOMEBUTLER_CONFIG
	SourceXDG  Source = "xdg"  // ~/.config/homebutler/config.yaml
	SourceCwd  Source = "cwd"  // ./homebutler.yaml
	SourceNone Source = "none" // nothing found; built-in defaults apply
)

// Describe returns a human-readable form of the rule that produced the path.
func (s Source) Describe() string {
	switch s {
	case SourceFlag:
		return "--config flag"
	case SourceEnv:
		return "$HOMEBUTLER_CONFIG"
	case SourceXDG:
		return "~/.config/homebutler/config.yaml (XDG)"
	case SourceCwd:
		return "./homebutler.yaml"
	default:
		return "no config file found (using defaults)"
	}
}

// ResolveWithSource finds the config file path using the following priority:
//  1. Explicit path (--config flag)
//  2. $HOMEBUTLER_CONFIG environment variable
//  3. ~/.config/homebutler/config.yaml (XDG standard)
//  4. ./homebutler.yaml (current directory)
//
// Returns an empty path and SourceNone if no config file is found (defaults
// will be used). Note that the first two rules do not check that the file
// exists: a path the user named explicitly is returned as-is, so that a
// mistyped one can be reported rather than silently falling through to the
// next rule.
func ResolveWithSource(explicit string) (string, Source) {
	if explicit != "" {
		return explicit, SourceFlag
	}
	if env := os.Getenv("HOMEBUTLER_CONFIG"); env != "" {
		return env, SourceEnv
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdg := filepath.Join(home, ".config", "homebutler", "config.yaml")
		if _, err := os.Stat(xdg); err == nil {
			return xdg, SourceXDG
		}
	}
	if _, err := os.Stat("homebutler.yaml"); err == nil {
		return "homebutler.yaml", SourceCwd
	}
	return "", SourceNone
}

// Resolve finds the config file path. See ResolveWithSource for the priority.
func Resolve(explicit string) string {
	path, _ := ResolveWithSource(explicit)
	return path
}

// newDefaultConfig returns the config Load starts from before a file is
// applied. Validate uses it too, so both see the same baseline.
func newDefaultConfig() *Config {
	defaultWatch := watch.DefaultWatchConfig()
	return &Config{
		Alerts: AlertConfig{
			CPU:    90,
			Memory: 85,
			Disk:   90,
		},
		Watch: WatchRuntimeConfig{
			Notify:    defaultWatch.Notify,
			Flapping:  defaultWatch.Flapping,
			Retention: defaultWatch.Retention,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := newDefaultConfig()

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

	cfg.Watch.Notify.Normalize()
	cfg.Watch.Retention.Normalize()

	cfg.Path = path

	// Refuse unsafe config permissions when plaintext secrets are present (non-Windows).
	if runtime.GOOS != "windows" && hasSecrets(cfg) {
		if info, err := os.Stat(path); err == nil {
			perm := info.Mode().Perm()
			if perm&0o077 != 0 {
				return nil, fmt.Errorf("config file %s contains plaintext secrets but permissions are too open (%04o); run: chmod 600 %s", path, perm, path)
			}
		}
	}

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

// FindProxmox returns the Proxmox endpoint config by name, or nil if absent.
func (c *Config) FindProxmox(name string) *ProxmoxConfig {
	for i := range c.Proxmox {
		if c.Proxmox[i].Name == name {
			return &c.Proxmox[i]
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
