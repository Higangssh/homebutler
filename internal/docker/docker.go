package docker

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/util"
)

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Ports   string `json:"ports"`
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
			ID:    fields[0][:12],
			Name:  fields[1],
			Image: fields[2],
			Status: fields[3],
			State: fields[4],
		}
		if len(fields) > 5 {
			c.Ports = fields[5]
		}
		containers = append(containers, c)
	}
	return containers, nil
}

func Restart(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.RunCmd("docker", "restart", name)
	if err != nil {
		return fmt.Errorf("failed to restart %s: %s", name, out)
	}
	fmt.Fprintf(os.Stdout, `{"action":"restart","container":"%s","status":"ok"}`+"\n", name)
	return nil
}

func Stop(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.RunCmd("docker", "stop", name)
	if err != nil {
		return fmt.Errorf("failed to stop %s: %s", name, out)
	}
	fmt.Fprintf(os.Stdout, `{"action":"stop","container":"%s","status":"ok"}`+"\n", name)
	return nil
}

func Logs(name string, lines string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.RunCmd("docker", "logs", "--tail", lines, name)
	if err != nil {
		return fmt.Errorf("failed to get logs for %s: %w", name, err)
	}
	result := map[string]string{
		"container": name,
		"lines":     lines,
		"logs":      out,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// isValidName prevents command injection by allowing only safe characters.
func isValidName(name string) bool {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return len(name) > 0 && len(name) <= 128
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range split(s, '\n') {
		lines = append(lines, l)
	}
	return lines
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
