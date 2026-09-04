package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"gopkg.in/yaml.v3"
)

// Severity ranks a validation finding. Errors mean the config is wrong and
// something will not work; warnings mean it parses but probably does not do
// what the user intended.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is one problem found in the config.
type Finding struct {
	Severity string `json:"severity"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

// Section reports what homebutler made of one top-level config key.
//
// This is the half of the output that answers "why is my notify config being
// ignored?". A key the file never set reads as "not set", and a key that was
// set but did not match the schema shows up in Findings instead of here — so
// the two together account for every line of the file.
type Section struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Summary string `json:"summary"`
}

// ValidationResult is the outcome of checking a config file.
type ValidationResult struct {
	Path     string    `json:"path"`
	Source   string    `json:"source"`
	Exists   bool      `json:"exists"`
	Sections []Section `json:"sections,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
	Valid    bool      `json:"valid"`
}

// Errors returns the number of error-severity findings.
func (r *ValidationResult) Errors() int { return r.count(SeverityError) }

// Warnings returns the number of warning-severity findings.
func (r *ValidationResult) Warnings() int { return r.count(SeverityWarning) }

func (r *ValidationResult) count(severity string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == severity {
			n++
		}
	}
	return n
}

func (r *ValidationResult) add(severity, field, message, hint string) {
	r.Findings = append(r.Findings, Finding{
		Severity: severity,
		Field:    field,
		Message:  message,
		Hint:     hint,
	})
}

// topLevelKeys mirrors the yaml tags on Config, in the order they are reported.
var topLevelKeys = []string{"servers", "proxmox", "wake", "alerts", "notify", "watch", "backup", "backup_dir"}

// Validate resolves the config the same way every other command does, then
// reports what it found without applying it. It never contacts a remote
// server and never writes anything.
//
// Validate deliberately does not go through Load: Load treats a missing file
// as "use defaults" so that running without a config works, which also means
// it cannot distinguish that from a path the user typed wrong.
func Validate(explicit string) *ValidationResult {
	path, source := ResolveWithSource(explicit)
	r := &ValidationResult{Path: path, Source: string(source)}

	if source == SourceNone {
		r.Valid = true
		r.add(SeverityWarning, "", "No config file found; built-in defaults are in use.",
			"Run `homebutler init` to create one, or pass --config <path>.")
		return r
	}

	info, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		// Load() swallows this and returns defaults, so commands run and
		// succeed against a config that was never read. Report it loudly.
		r.add(SeverityError, "", fmt.Sprintf("Config file %s does not exist.", path),
			fmt.Sprintf("It was selected by %s. Commands fall back to built-in defaults rather than failing, so this is easy to miss.", source.Describe()))
		return r
	case err != nil:
		r.add(SeverityError, "", fmt.Sprintf("Cannot read %s: %v", path, err), "")
		return r
	case info.IsDir():
		r.add(SeverityError, "", fmt.Sprintf("%s is a directory, not a config file.", path), "")
		return r
	}
	r.Exists = true

	data, err := os.ReadFile(path)
	if err != nil {
		r.add(SeverityError, "", fmt.Sprintf("Cannot read %s: %v", path, err), "")
		return r
	}

	// Lenient decode: exactly what Load() ends up with at runtime, so the
	// value checks below judge the config the commands will actually use.
	cfg := newDefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		r.add(SeverityError, "", fmt.Sprintf("Invalid YAML: %v", err),
			"Fix the syntax error above; nothing else can be checked until the file parses.")
		return r
	}
	cfg.Path = path

	// Shape errors were already reported by the decode above; anything that
	// fails here just yields an empty map and no presence information.
	var rawTop map[string]yaml.Node
	_ = yaml.Unmarshal(data, &rawTop)

	r.checkUnknownKeys(data)
	r.checkWatchKeys(rawTop)
	r.Sections = describeSections(rawTop, cfg)
	r.checkPermissions(path, cfg)
	r.checkServers(cfg)
	r.checkProxmox(cfg)
	r.checkWake(cfg)
	r.checkAlerts(cfg)
	r.checkNotify(cfg)
	r.checkWatch(cfg)
	r.checkBackupDir(cfg)

	r.Valid = r.Errors() == 0
	return r
}

// checkUnknownKeys re-decodes with KnownFields so that keys homebutler does
// not recognise are reported. yaml.Unmarshal drops them without a word, which
// is how a typo like `notifiy:` disables notifications with no visible error.
func (r *ValidationResult) checkUnknownKeys(data []byte) {
	strict := newDefaultConfig()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	err := dec.Decode(strict)
	if err == nil {
		return
	}
	typeErr, ok := err.(*yaml.TypeError)
	if !ok {
		return // syntax errors are already reported by the lenient decode
	}

	for _, msg := range typeErr.Errors {
		field, hint := unknownFieldHint(msg)
		r.add(SeverityWarning, field, cleanYAMLError(msg), hint)
	}
}

// unknownFieldHint pulls the offending key out of a yaml "field X not found"
// message and suggests the closest key homebutler does know, when the key is
// at the top level.
func unknownFieldHint(msg string) (field, hint string) {
	const marker = "field "
	i := strings.Index(msg, marker)
	if i < 0 || !strings.Contains(msg, "not found") {
		return "", ""
	}
	rest := msg[i+len(marker):]
	j := strings.Index(rest, " ")
	if j < 0 {
		return "", ""
	}
	field = rest[:j]

	hint = "Unrecognised keys are ignored silently, so this line currently has no effect."
	if !strings.Contains(msg, "type config.Config") {
		return field, hint
	}
	if best, ok := closestKey(field, topLevelKeys); ok {
		hint = fmt.Sprintf("Did you mean %q? Unrecognised keys are ignored silently, so this line currently has no effect.", best)
	}
	return field, hint
}

// cleanYAMLError rewrites yaml's Go type names into config paths, so the
// message reads as configuration rather than as internals.
func cleanYAMLError(msg string) string {
	replacements := []struct{ from, to string }{
		{"type config.Config", "the homebutler config"},
		{"type config.ServerConfig", "a servers[] entry"},
		{"type config.ProxmoxConfig", "a proxmox[] entry"},
		{"type config.WakeTarget", "a wake[] entry"},
		{"type config.AlertConfig", "alerts"},
		{"type config.WatchRuntimeConfig", "watch"},
		{"type notify.ProviderConfig", "notify"},
		{"type notify.TelegramConfig", "notify.telegram"},
		{"type notify.SlackConfig", "notify.slack"},
		{"type notify.DiscordConfig", "notify.discord"},
		{"type notify.WebhookConfig", "notify.webhook"},
		{"type watch.NotifySettings", "watch.notify"},
		{"type watch.FlappingConfig", "watch.flapping"},
	}
	for _, rep := range replacements {
		msg = strings.ReplaceAll(msg, rep.from, rep.to)
	}
	if msg == "" {
		return ""
	}
	return strings.ToUpper(msg[:1]) + msg[1:]
}

// closestKey returns the candidate within edit distance 2 of key, if any.
func closestKey(key string, candidates []string) (string, bool) {
	best, bestDist := "", 3
	for _, c := range candidates {
		if d := editDistance(strings.ToLower(key), c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best, best != ""
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// Keys accepted under watch:. The flat spellings are the compatibility path
// described on WatchRuntimeConfig.UnmarshalYAML.
var (
	watchKeys          = []string{"notify", "flapping", "retention", "enabled", "notify_on", "on_incident", "on_flapping", "cooldown"}
	watchNotifyKeys    = []string{"enabled", "notify_on", "on_incident", "on_flapping", "cooldown"}
	watchFlapKeys      = []string{"short_window", "short_threshold", "long_window", "long_threshold"}
	watchRetentionKeys = []string{"max_incidents"}
)

// checkWatchKeys inspects the watch subtree by hand.
//
// WatchRuntimeConfig has a custom unmarshaler, and a custom unmarshaler
// decodes its own node without inheriting the parent decoder's KnownFields
// setting. Unknown keys under watch: would therefore pass unreported, which
// is the failure this whole command exists to prevent.
func (r *ValidationResult) checkWatchKeys(rawTop map[string]yaml.Node) {
	node, ok := rawTop["watch"]
	if !ok || node.Kind != yaml.MappingNode {
		return
	}

	r.reportUnknownKeys(&node, "watch", watchKeys)

	var flat []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		switch key := node.Content[i].Value; key {
		case "notify":
			r.reportUnknownKeys(node.Content[i+1], "watch.notify", watchNotifyKeys)
		case "flapping":
			r.reportUnknownKeys(node.Content[i+1], "watch.flapping", watchFlapKeys)
		case "retention":
			r.reportUnknownKeys(node.Content[i+1], "watch.retention", watchRetentionKeys)
		default:
			if slices.Contains(watchKeys, key) {
				flat = append(flat, key)
			}
		}
	}

	if len(flat) > 0 && hasMappingKey(&node, "notify") {
		r.add(SeverityWarning, "watch",
			fmt.Sprintf("Both watch.notify and the flat form (%s) are set.", strings.Join(flat, ", ")),
			"The watch.notify block wins and the flat keys are ignored. Keep one form.")
	}
}

// reportUnknownKeys flags mapping keys that are not in the accepted set.
func (r *ValidationResult) reportUnknownKeys(node *yaml.Node, prefix string, known []string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if slices.Contains(known, key) {
			continue
		}
		hint := "Unrecognised keys are ignored silently, so this line currently has no effect."
		if best, ok := closestKey(key, known); ok {
			hint = fmt.Sprintf("Did you mean %q? %s", best, hint)
		}
		r.add(SeverityWarning, prefix+"."+key,
			fmt.Sprintf("Line %d: field %s not found in %s", node.Content[i].Line, key, prefix), hint)
	}
}

// describeSections reports, for each top-level key, whether the file set it
// and what the resulting configuration is.
func describeSections(rawTop map[string]yaml.Node, cfg *Config) []Section {
	present := map[string]bool{}
	for k := range rawTop {
		present[k] = true
	}

	sections := make([]Section, 0, len(topLevelKeys))
	for _, name := range topLevelKeys {
		sections = append(sections, Section{
			Name:    name,
			Present: present[name],
			Summary: sectionSummary(name, present[name], cfg),
		})
	}
	return sections
}

func sectionSummary(name string, present bool, cfg *Config) string {
	switch name {
	case "servers":
		if len(cfg.Servers) == 0 {
			return "not set"
		}
		names := make([]string, 0, len(cfg.Servers))
		for _, s := range cfg.Servers {
			names = append(names, s.Name)
		}
		return fmt.Sprintf("%s (%s)", plural(len(cfg.Servers), "server"), strings.Join(names, ", "))

	case "proxmox":
		if len(cfg.Proxmox) == 0 {
			return "not set"
		}
		names := make([]string, 0, len(cfg.Proxmox))
		for _, p := range cfg.Proxmox {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("%s (%s)", plural(len(cfg.Proxmox), "endpoint"), strings.Join(names, ", "))

	case "wake":
		if len(cfg.Wake) == 0 {
			return "not set"
		}
		return plural(len(cfg.Wake), "target")

	case "alerts":
		s := fmt.Sprintf("cpu %g%% · memory %g%% · disk %g%%", cfg.Alerts.CPU, cfg.Alerts.Memory, cfg.Alerts.Disk)
		if !present {
			return s + " (defaults)"
		}
		return s

	case "notify":
		channels := cfg.Notify.EnabledChannels()
		if len(channels) == 0 {
			if present {
				return "configured, but no channel is complete"
			}
			return "not set"
		}
		names := make([]string, 0, len(channels))
		for _, c := range channels {
			names = append(names, string(c))
		}
		sort.Strings(names)
		return strings.Join(names, ", ")

	case "watch":
		n := cfg.Watch.Notify
		notifyOn := n.NotifyOn
		if notifyOn == "" {
			notifyOn = "flapping"
		}
		s := fmt.Sprintf("enabled=%t · notify_on=%s · cooldown=%s", n.Enabled, notifyOn, n.Cooldown)
		if !present {
			return s + " (defaults)"
		}
		return s

	case "backup", "backup_dir":
		if cfg.Backup.Dir == "" && cfg.BackupDir == "" {
			return cfg.ResolveBackupDir() + " (default)"
		}
		return cfg.ResolveBackupDir()
	}
	return ""
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// checkPermissions mirrors the guard in Load so that validate reports the
// same refusal instead of the user meeting it later mid-command.
func (r *ValidationResult) checkPermissions(path string, cfg *Config) {
	if perm, tooOpen := PermissionProblem(path, cfg); tooOpen {
		r.add(SeverityError, "",
			fmt.Sprintf("Config contains plaintext secrets but permissions are too open (%04o).", perm),
			fmt.Sprintf("Run: chmod 600 %s", path))
	}
}

func (r *ValidationResult) checkServers(cfg *Config) {
	seen := map[string]int{}
	for i, s := range cfg.Servers {
		field := fmt.Sprintf("servers[%d]", i)

		if s.Name == "" {
			r.add(SeverityError, field+".name", "Server name is required.",
				"Names are how --server and the web dashboard refer to this entry.")
		} else if first, dup := seen[s.Name]; dup {
			r.add(SeverityError, field+".name",
				fmt.Sprintf("Duplicate server name %q (first defined at servers[%d]).", s.Name, first),
				"--server picks the first match, so the later entry is unreachable.")
		} else {
			seen[s.Name] = i
		}

		if s.Host == "" && !s.Local {
			r.add(SeverityError, field+".host", "Host is required for remote servers.",
				"Set host, or set local: true if this entry is the machine homebutler runs on.")
		}

		if s.Port < 0 || s.Port > 65535 {
			r.add(SeverityError, field+".port",
				fmt.Sprintf("Port %d is out of range.", s.Port), "Valid ports are 1-65535; omit the key to use 22.")
		}

		switch s.AuthMode {
		case "", "key", "password":
		default:
			r.add(SeverityError, field+".auth",
				fmt.Sprintf("Unknown auth mode %q.", s.AuthMode), `Valid values are "key" (default) or "password".`)
		}

		if s.AuthMode == "password" && s.Password == "" {
			r.add(SeverityError, field+".password", `Auth mode is "password" but no password is set.`, "")
		}
		if s.Password != "" && s.AuthMode != "password" {
			r.add(SeverityWarning, field+".password",
				"A password is set but this server uses key auth, so the password is ignored.",
				`Set auth: password to use it, or remove the key.`)
		}

		if s.KeyFile != "" && !s.Local {
			expanded := expandHome(s.KeyFile)
			if _, err := os.Stat(expanded); os.IsNotExist(err) {
				r.add(SeverityWarning, field+".key",
					fmt.Sprintf("Key file %s does not exist.", s.KeyFile),
					"Connections to this server will fail when they are attempted.")
			}
		}
	}
}

// sameProxmoxCredential reports whether the action credential's token source
// is textually identical to the read credential's — either the same inline
// value or the same file path. It does not read file contents: two different
// paths could still hold the same secret, but that is outside what config
// alone can tell.
func sameProxmoxCredential(p ProxmoxConfig) bool {
	if p.Token != "" && p.ActionToken != "" {
		return p.Token == p.ActionToken
	}
	if p.TokenFile != "" && p.ActionTokenFile != "" {
		return p.TokenFile == p.ActionTokenFile
	}
	return false
}

func (r *ValidationResult) checkProxmox(cfg *Config) {
	seen := map[string]int{}
	for i, p := range cfg.Proxmox {
		field := fmt.Sprintf("proxmox[%d]", i)

		if p.Name == "" {
			r.add(SeverityError, field+".name", "Proxmox endpoint name is required.",
				"Names are how the Proxmox CLI and MCP tools refer to this entry.")
		} else if first, dup := seen[p.Name]; dup {
			r.add(SeverityError, field+".name",
				fmt.Sprintf("Duplicate Proxmox endpoint name %q (first defined at proxmox[%d]).", p.Name, first),
				"Endpoint selection picks the first match, so the later entry is unreachable.")
		} else {
			seen[p.Name] = i
		}

		if p.Host == "" {
			r.add(SeverityError, field+".host", "Proxmox host is required.", "Set the IP address or hostname for this endpoint.")
		}
		if p.TokenID == "" {
			r.add(SeverityError, field+".token_id", "Proxmox token_id is required.", "Set the API token ID, such as monitoring@pve!readonly.")
		}
		if p.Port < 0 || p.Port > 65535 {
			r.add(SeverityError, field+".port", fmt.Sprintf("Port %d is out of range.", p.Port),
				"Valid ports are 1-65535; omit the key to use 8006.")
		}
		if p.Token == "" && p.TokenFile == "" {
			r.add(SeverityError, field, "A Proxmox token or token_file is required.",
				"token_file is preferred so the token is not stored in the config file.")
		}
		if p.Token != "" && p.TokenFile != "" {
			r.add(SeverityError, field, "Both Proxmox token and token_file are set.",
				"Keep token_file (preferred) or token, but not both.")
		}
		if p.TokenFile != "" {
			if _, err := os.Stat(p.TokenFilePath()); os.IsNotExist(err) {
				r.add(SeverityError, field+".token_file", fmt.Sprintf("Token file %s does not exist.", p.TokenFile), "Create the file or set token instead.")
			}
		}
		if p.Timeout != "" {
			timeout, err := time.ParseDuration(p.Timeout)
			if err != nil || timeout <= 0 {
				r.add(SeverityError, field+".timeout", fmt.Sprintf("Invalid duration %q.", p.Timeout), `Use a positive Go duration such as "10s".`)
			}
		}
		for j, guest := range p.Guests {
			if err := guest.Validate(); err != nil {
				r.add(SeverityError, fmt.Sprintf("%s.guests[%d]", field, j), err.Error(),
					"Set node, type (qemu or lxc), and a positive vmid.")
			}
		}

		if p.ActionTokenID != "" || p.ActionToken != "" || p.ActionTokenFile != "" {
			if p.ActionTokenID == "" {
				r.add(SeverityError, field+".action_token_id", "Proxmox action_token_id is required when an action credential is configured.",
					"Set the API token ID for a dedicated action user, such as power@pve!action — not the read-only user, which would widen its access.")
			}
			if p.ActionToken == "" && p.ActionTokenFile == "" {
				r.add(SeverityError, field, "An action_token or action_token_file is required when action_token_id is set.",
					"action_token_file is preferred so the token is not stored in the config file.")
			}
			if p.ActionToken != "" && p.ActionTokenFile != "" {
				r.add(SeverityError, field, "Both Proxmox action_token and action_token_file are set.",
					"Keep action_token_file (preferred) or action_token, but not both.")
			}
			if p.ActionTokenFile != "" {
				if info, err := os.Stat(p.ActionTokenFilePath()); os.IsNotExist(err) {
					r.add(SeverityError, field+".action_token_file", fmt.Sprintf("Action token file %s does not exist.", p.ActionTokenFile), "Create the file or set action_token instead.")
				} else if err == nil && runtime.GOOS != "windows" {
					if perm := info.Mode().Perm(); perm&0o077 != 0 {
						r.add(SeverityWarning, field+".action_token_file",
							fmt.Sprintf("Action token file %s is readable by group or others (%04o).", p.ActionTokenFile, perm),
							fmt.Sprintf("This credential grants VM.PowerMgmt. Run: chmod 600 %s", p.ActionTokenFilePath()))
					}
				}
			}
			if p.HasActionCredential() && p.ActionTokenID == p.TokenID {
				r.add(SeverityWarning, field+".action_token_id",
					"action_token_id is the same as token_id, so the action credential is not separate from the read credential.",
					"Use a dedicated user for the action token, such as power@pve!action; see docs/proxmox.md for the ACL split.")
			} else if p.HasActionCredential() && sameProxmoxCredential(p) {
				r.add(SeverityWarning, field+".action_token",
					"The action token value is the same as the read token, so the two credentials carry identical access.",
					"Issue a separate token for guest actions; see docs/proxmox.md for the ACL split.")
			}
		}
	}
}

func (r *ValidationResult) checkWake(cfg *Config) {
	for i, t := range cfg.Wake {
		field := fmt.Sprintf("wake[%d]", i)
		if t.Name == "" {
			r.add(SeverityError, field+".name", "Wake target name is required.", "")
		}
		if t.MAC == "" {
			r.add(SeverityError, field+".mac", "MAC address is required.", "")
		} else if _, err := net.ParseMAC(t.MAC); err != nil {
			r.add(SeverityError, field+".mac",
				fmt.Sprintf("Invalid MAC address %q.", t.MAC), "Expected format: AA:BB:CC:DD:EE:FF")
		}
	}
}

func (r *ValidationResult) checkAlerts(cfg *Config) {
	thresholds := []struct {
		name  string
		value float64
	}{
		{"cpu", cfg.Alerts.CPU},
		{"memory", cfg.Alerts.Memory},
		{"disk", cfg.Alerts.Disk},
	}
	for _, t := range thresholds {
		field := "alerts." + t.name
		switch {
		case t.value < 0 || t.value > 100:
			r.add(SeverityError, field,
				fmt.Sprintf("Threshold %g is out of range.", t.value), "Thresholds are percentages between 0 and 100.")
		case t.value == 0:
			r.add(SeverityWarning, field, "Threshold is 0, which alerts on every check.",
				"Omit the key to use the default instead.")
		}
	}
}

func (r *ValidationResult) checkNotify(cfg *Config) {
	if cfg.Notify.IsEmpty() {
		return
	}

	if t := cfg.Notify.Telegram; t != nil {
		switch {
		case t.BotToken == "" && t.ChatID == "":
			r.add(SeverityWarning, "notify.telegram", "Telegram block is empty, so Telegram stays disabled.", "")
		case t.BotToken == "":
			r.add(SeverityWarning, "notify.telegram.bot_token", "chat_id is set but bot_token is missing, so Telegram stays disabled.", "")
		case t.ChatID == "":
			r.add(SeverityWarning, "notify.telegram.chat_id", "bot_token is set but chat_id is missing, so Telegram stays disabled.", "")
		}
	}

	urls := []struct{ field, url string }{}
	if s := cfg.Notify.Slack; s != nil {
		urls = append(urls, struct{ field, url string }{"notify.slack.webhook_url", s.WebhookURL})
	}
	if d := cfg.Notify.Discord; d != nil {
		urls = append(urls, struct{ field, url string }{"notify.discord.webhook_url", d.WebhookURL})
	}
	if w := cfg.Notify.Webhook; w != nil {
		urls = append(urls, struct{ field, url string }{"notify.webhook.url", w.URL})
	}
	for _, u := range urls {
		provider := strings.Split(u.field, ".")[1]
		switch {
		case u.url == "":
			r.add(SeverityWarning, u.field,
				fmt.Sprintf("URL is missing, so %s stays disabled.", provider), "")
		case !strings.HasPrefix(u.url, "http://") && !strings.HasPrefix(u.url, "https://"):
			r.add(SeverityWarning, u.field,
				fmt.Sprintf("URL %q does not start with http:// or https://.", u.url), "")
		}
	}
}

func (r *ValidationResult) checkWatch(cfg *Config) {
	n := cfg.Watch.Notify

	switch n.NotifyOn {
	case "", "all", "incident", "flapping", "off":
	default:
		r.add(SeverityError, "watch.notify_on",
			fmt.Sprintf("Unknown value %q.", n.NotifyOn), `Valid values are "all", "incident", "flapping", or "off".`)
	}

	if n.Cooldown != "" {
		if _, err := time.ParseDuration(n.Cooldown); err != nil {
			r.add(SeverityError, "watch.cooldown",
				fmt.Sprintf("Invalid duration %q.", n.Cooldown), `Use a Go duration such as "5m" or "30s".`)
		}
	}

	// The #31 shape: watch is switched on, but nothing can deliver.
	if n.Enabled && n.NotifyOn != "off" && cfg.Notify.IsEmpty() {
		r.add(SeverityWarning, "watch.enabled",
			"Watch notifications are enabled but no notify provider is configured.",
			"Add a notify: block, or set watch.enabled: false to silence this.")
	}

	f := cfg.Watch.Flapping
	if f.ShortThreshold < 0 || f.LongThreshold < 0 {
		r.add(SeverityError, "watch.flapping", "Flapping thresholds cannot be negative.", "")
	}
	if f.ShortWindow < 0 || f.LongWindow < 0 {
		r.add(SeverityError, "watch.flapping", "Flapping windows cannot be negative.", "")
	}
}

func (r *ValidationResult) checkBackupDir(cfg *Config) {
	r.checkBackupRetention(cfg)

	configured := cfg.Backup.Dir
	if configured == "" {
		configured = cfg.BackupDir
	}
	if configured == "" {
		return
	}
	dir := expandHome(configured)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		parent := filepath.Dir(dir)
		if _, err := os.Stat(parent); os.IsNotExist(err) {
			r.add(SeverityWarning, "backup_dir",
				fmt.Sprintf("Neither %s nor its parent exists.", configured),
				"Backups will fail until the parent directory is created.")
		}
	}
}

// checkBackupRetention catches a size that will not parse, here rather than at
// the end of the first backup that tries to apply it.
func (r *ValidationResult) checkBackupRetention(cfg *Config) {
	ret := cfg.Backup.Retention
	if ret.MaxArchives < 0 {
		r.add(SeverityWarning, "backup",
			fmt.Sprintf("backup.retention.max_archives is %d.", ret.MaxArchives),
			"Use 0, or leave it out, to keep every backup. A negative value is not a way to say unlimited here.")
	}
	if _, err := backup.ParseByteSize(ret.MaxBytes); err != nil {
		r.add(SeverityError, "backup",
			fmt.Sprintf("backup.retention.max_bytes: %v.", err),
			"Write a number and a unit, like 20GB or 500MB. Retention will not run until this parses.")
	}
}

// expandHome resolves a leading ~ so that paths written the way users write
// them in YAML can actually be checked.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// FormatValidation renders a validation result for a terminal.
func FormatValidation(r *ValidationResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "🗂  Config validation\n")
	if r.Path == "" {
		fmt.Fprintf(&b, "   Source  %s\n\n", Source(r.Source).Describe())
	} else {
		fmt.Fprintf(&b, "   File    %s\n", r.Path)
		fmt.Fprintf(&b, "   Source  %s\n\n", Source(r.Source).Describe())
	}

	if len(r.Sections) > 0 {
		fmt.Fprintf(&b, "Sections\n")
		for _, s := range r.Sections {
			marker := "·"
			if s.Present {
				marker = "✓"
			}
			fmt.Fprintf(&b, "   %s %-11s %s\n", marker, s.Name, s.Summary)
		}
		fmt.Fprintln(&b)
	}

	if len(r.Findings) > 0 {
		fmt.Fprintf(&b, "Findings\n")
		for _, f := range r.Findings {
			icon := "⚠️"
			if f.Severity == SeverityError {
				icon = "❌"
			}
			// Unknown-key messages already name the field and carry a line
			// number, so prefixing them again just reads as a stutter.
			if f.Field != "" && !strings.Contains(f.Message, f.Field) {
				fmt.Fprintf(&b, "   %s %s: %s\n", icon, f.Field, f.Message)
			} else {
				fmt.Fprintf(&b, "   %s %s\n", icon, f.Message)
			}
			if f.Hint != "" {
				fmt.Fprintf(&b, "      → %s\n", f.Hint)
			}
		}
		fmt.Fprintln(&b)
	}

	errs, warns := r.Errors(), r.Warnings()
	switch {
	case errs > 0:
		fmt.Fprintf(&b, "❌ Config is invalid — %s, %s\n", plural(errs, "error"), plural(warns, "warning"))
	case warns > 0:
		fmt.Fprintf(&b, "⚠️  Config is valid — %s\n", plural(warns, "warning"))
	default:
		fmt.Fprintf(&b, "✅ Config is valid\n")
	}

	return b.String()
}
