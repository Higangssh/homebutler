package docker

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

type Container struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	State  string `json:"state"`
	Ports  string `json:"ports"`
}

func List() ([]Container, error) {
	// Check if docker binary exists
	if _, lookErr := util.RunCmd("which", "docker"); lookErr != nil {
		return nil, fmt.Errorf("docker is not installed (binary not found in PATH)")
	}

	out, err := util.RunCmd("docker", "ps", "-a",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}\t{{.Ports}}")
	if err != nil {
		return nil, fmt.Errorf("docker daemon is not running: %w", err)
	}

	containers := make([]Container, 0)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		fields := splitTabs(line)
		if len(fields) < 5 {
			continue
		}
		c := Container{
			ID:     fields[0][:12],
			Name:   fields[1],
			Image:  fields[2],
			Status: friendlyStatus(fields[3], fields[4]),
			State:  fields[4],
		}
		if len(fields) > 5 {
			c.Ports = fields[5]
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// ActionResult holds the result of a docker action.
type ActionResult struct {
	Action    string `json:"action"`
	Container string `json:"container"`
	Status    string `json:"status"`
}

func Restart(name string) (*ActionResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.RunCmd("docker", "restart", name)
	if err != nil {
		return nil, fmt.Errorf("failed to restart %s: %s", name, out)
	}
	return &ActionResult{Action: "restart", Container: name, Status: "ok"}, nil
}

func Stop(name string) (*ActionResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.RunCmd("docker", "stop", name)
	if err != nil {
		return nil, fmt.Errorf("failed to stop %s: %s", name, out)
	}
	return &ActionResult{Action: "stop", Container: name, Status: "ok"}, nil
}

// LogsResult holds docker logs output.
type LogsResult struct {
	Container string `json:"container"`
	Lines     string `json:"lines"`
	Logs      string `json:"logs"`
}

func Logs(name string, lines string) (*LogsResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	// Validate lines is a positive integer
	for _, c := range lines {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("invalid line count: %s (must be a positive integer)", lines)
		}
	}
	out, err := util.RunCmd("docker", "logs", "--tail", lines, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for %s: %w", name, err)
	}
	return &LogsResult{Container: name, Lines: lines, Logs: out}, nil
}

var exitedRe = regexp.MustCompile(`(?i)exited\s*\(\d+\)\s*(.+)\s*ago`)

// friendlyStatus converts raw docker status to user-friendly format.
// "Exited (0) 6 hours ago" → "Stopped · 6h ago"
// "Up 4 days" → "Running · 4d"
func friendlyStatus(raw, state string) string {
	if state == "running" {
		s := strings.TrimPrefix(raw, "Up ")
		s = shortenDuration(s)
		return "Running · " + s
	}
	if m := exitedRe.FindStringSubmatch(raw); len(m) > 1 {
		return "Stopped · " + shortenDuration(strings.TrimSpace(m[1])) + " ago"
	}
	return raw
}

// shortenDuration shortens "4 days" → "4d", "6 hours" → "6h", "30 minutes" → "30m".
func shortenDuration(s string) string {
	s = strings.ReplaceAll(s, " seconds", "s")
	s = strings.ReplaceAll(s, " second", "s")
	s = strings.ReplaceAll(s, " minutes", "m")
	s = strings.ReplaceAll(s, " minute", "m")
	s = strings.ReplaceAll(s, " hours", "h")
	s = strings.ReplaceAll(s, " hour", "h")
	s = strings.ReplaceAll(s, " days", "d")
	s = strings.ReplaceAll(s, " day", "d")
	s = strings.ReplaceAll(s, " weeks", "w")
	s = strings.ReplaceAll(s, " week", "w")
	s = strings.ReplaceAll(s, " months", "mo")
	s = strings.ReplaceAll(s, " month", "mo")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// isValidName prevents command injection by allowing only safe characters.
func isValidName(name string) bool {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || //nolint:staticcheck // readability
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return len(name) > 0 && len(name) <= 128
}

func splitLines(s string) []string {
	return split(s, '\n')
}

func splitTabs(s string) []string {
	return split(s, '\t')
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
