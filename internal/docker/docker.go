package docker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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

	out, err := util.DockerCmd("ps", "-a",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}\t{{.Ports}}")
	if err != nil {
		return nil, fmt.Errorf("docker daemon is not running: %w", err)
	}

	return parseDockerPS(out), nil
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
	out, err := util.DockerCmd("restart", name)
	if err != nil {
		return nil, fmt.Errorf("failed to restart %s: %s", name, out)
	}
	return &ActionResult{Action: "restart", Container: name, Status: "ok"}, nil
}

func Stop(name string) (*ActionResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.DockerCmd("stop", name)
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
	out, err := util.DockerCmd("logs", "--tail", lines, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for %s: %w", name, err)
	}
	return &LogsResult{Container: name, Lines: lines, Logs: out}, nil
}

// TopResult holds the processes running inside a container, read from the
// host via docker top. No exec, no TTY.
type TopResult struct {
	Container string       `json:"container"`
	Processes []TopProcess `json:"processes"`
}

// TopProcess is one row of docker top output.
type TopProcess struct {
	PID     string `json:"pid"`
	User    string `json:"user"`
	Command string `json:"command"`
}

func Top(name string) (*TopResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.DockerCmd("top", name)
	if err != nil {
		return nil, fmt.Errorf("failed to list processes for %s: %s", name, out)
	}
	return &TopResult{Container: name, Processes: parseDockerTop(out)}, nil
}

// parseDockerTop parses raw docker top output: a ps header line followed by
// one row per process. docker top passes its arguments to ps on the host, so
// the column layout differs between platforms — Linux lands on ps -ef with a
// UID first column, macOS on ps aux with USER and a COMMAND column instead of
// CMD. Both put the user first and the command last, which is what this reads;
// anything else is skipped rather than misindexed.
func parseDockerTop(out string) []TopProcess {
	processes := make([]TopProcess, 0)
	var cmdCol, pidCol int
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if cmdCol == 0 && pidCol == 0 {
			// First non-empty line is the ps header. The command column is
			// last on every layout seen so far; the PID column is found by
			// name, and if it is not there the output is not ps output.
			cmdCol = len(fields)
			pidCol = -1
			for i, f := range fields {
				if f == "PID" {
					pidCol = i
					break
				}
			}
			if pidCol < 0 || cmdCol-1 <= pidCol {
				return processes
			}
			continue
		}
		// A row shorter than the header has no reliable command tail; skip it.
		if len(fields) < cmdCol {
			continue
		}
		processes = append(processes, TopProcess{
			PID:     fields[pidCol],
			User:    fields[0],
			Command: strings.Join(fields[cmdCol-1:], " "),
		})
	}
	return processes
}

// InspectResult holds a readable summary of docker inspect for one container.
//
// Deliberately minimal: docker inspect exposes Config.Env, and container env
// routinely holds database passwords and API tokens. The decode target simply
// has no field for it, so values cannot leak into either output form.
type InspectResult struct {
	Name          string        `json:"name"`
	Image         string        `json:"image"`
	Status        string        `json:"status"`
	Uptime        string        `json:"uptime,omitempty"`
	RestartPolicy string        `json:"restart_policy"`
	RestartCount  int           `json:"restart_count"`
	Ports         []PortBinding `json:"ports"`
	Mounts        []Mount       `json:"mounts"`
	Networks      []Network     `json:"networks"`
	Health        string        `json:"health,omitempty"`
}

// PortBinding is one published or exposed port. Host is empty when the port
// is exposed but not published.
type PortBinding struct {
	Host      string `json:"host,omitempty"`
	Container string `json:"container"`
}

// Mount is one volume or bind mount.
type Mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode,omitempty"`
}

// Network is one attached network with the container's address in it.
type Network struct {
	Name string `json:"name"`
	IP   string `json:"ip,omitempty"`
}

func Inspect(name string) (*InspectResult, error) {
	if !isValidName(name) {
		return nil, fmt.Errorf("invalid container name: %s", name)
	}
	out, err := util.DockerCmd("inspect", name)
	if err != nil {
		// CombinedOutput carries the reason ("No such object", "cannot connect
		// to the Docker daemon"), so surface it rather than the bare exit code.
		return nil, fmt.Errorf("failed to inspect %s: %s", name, out)
	}
	return parseDockerInspect(out, time.Now())
}

// inspectDoc mirrors only the fields of the docker inspect document this
// reads. It is an array at the top level because docker accepts several names,
// though it errors before returning anything unless exactly one object matched.
type inspectDoc struct {
	Name         string `json:"Name"`
	Image        string `json:"Image"`
	RestartCount int    `json:"RestartCount"`
	Config       struct {
		Image string `json:"Image"`
	} `json:"Config"`
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// parseDockerInspect decodes docker inspect JSON into a summary. now is
// injected so uptime stays testable.
func parseDockerInspect(out string, now time.Time) (*InspectResult, error) {
	var docs []inspectDoc
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		return nil, fmt.Errorf("could not parse docker inspect output: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("docker inspect returned no data")
	}
	d := docs[0]

	res := &InspectResult{
		Name:          strings.TrimPrefix(d.Name, "/"),
		Status:        d.State.Status,
		RestartPolicy: d.HostConfig.RestartPolicy.Name,
		RestartCount:  d.RestartCount,
		Ports:         make([]PortBinding, 0),
		Mounts:        make([]Mount, 0),
		Networks:      make([]Network, 0),
	}
	if res.RestartPolicy == "" {
		res.RestartPolicy = "no"
	}
	res.Image = d.Config.Image
	if res.Image == "" {
		res.Image = d.Image
	}
	if d.State.Running && d.State.StartedAt != "" {
		if started, err := time.Parse(time.RFC3339Nano, d.State.StartedAt); err == nil && now.After(started) {
			res.Uptime = "up " + shortUptime(now.Sub(started))
		}
	}
	if d.State.Health != nil {
		res.Health = d.State.Health.Status
	}

	portKeys := make([]string, 0, len(d.NetworkSettings.Ports))
	for k := range d.NetworkSettings.Ports {
		portKeys = append(portKeys, k)
	}
	sort.Strings(portKeys)
	for _, k := range portKeys {
		bindings := d.NetworkSettings.Ports[k]
		if len(bindings) == 0 {
			res.Ports = append(res.Ports, PortBinding{Container: k})
			continue
		}
		for _, b := range bindings {
			host := b.HostPort
			if b.HostIP != "" && b.HostPort != "" {
				ip := b.HostIP
				// ps-style displays bracket bare IPv6 hosts: [::]:443, not :::443.
				if strings.Contains(ip, ":") {
					ip = "[" + ip + "]"
				}
				host = ip + ":" + b.HostPort
			}
			res.Ports = append(res.Ports, PortBinding{Host: host, Container: k})
		}
	}

	for _, m := range d.Mounts {
		mode := m.Mode
		if mode == "" {
			if m.RW {
				mode = "rw"
			} else {
				mode = "ro"
			}
		}
		res.Mounts = append(res.Mounts, Mount{
			Source:      m.Source,
			Destination: m.Destination,
			Mode:        mode,
		})
	}

	netNames := make([]string, 0, len(d.NetworkSettings.Networks))
	for n := range d.NetworkSettings.Networks {
		netNames = append(netNames, n)
	}
	sort.Strings(netNames)
	for _, n := range netNames {
		res.Networks = append(res.Networks, Network{
			Name: n,
			IP:   d.NetworkSettings.Networks[n].IPAddress,
		})
	}

	return res, nil
}

// shortUptime renders a duration compactly: 4d, 6h, 12m.
func shortUptime(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	case d >= time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
}

// parseDockerPS parses the output of docker ps -a --format with tab separators.
func parseDockerPS(out string) []Container {
	containers := make([]Container, 0)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		fields := splitTabs(line)
		if len(fields) < 5 {
			continue
		}
		id := fields[0]
		if len(id) > 12 {
			id = id[:12]
		}
		c := Container{
			ID:     id,
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
	return containers
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

// ContainerStats holds resource usage statistics for a running container.
type ContainerStats struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CPUPerc  string `json:"cpu_percent"`
	MemUsage string `json:"mem_usage"`
	MemPerc  string `json:"mem_percent"`
	NetIO    string `json:"net_io"`
	BlockIO  string `json:"block_io"`
	PIDs     string `json:"pids"`
}

// Stats returns resource usage statistics for all running containers.
func Stats() ([]ContainerStats, error) {
	// Check if docker binary exists
	if _, lookErr := util.RunCmd("which", "docker"); lookErr != nil {
		return nil, fmt.Errorf("docker is not installed (binary not found in PATH)")
	}

	out, err := util.DockerCmd("stats", "--no-stream",
		"--format", "{{.ID}}\t{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}\t{{.PIDs}}")
	if err != nil {
		return nil, fmt.Errorf("docker daemon is not running: %w", err)
	}

	return parseDockerStats(out), nil
}

// parseDockerStats parses the output of docker stats --no-stream --format with tab separators.
func parseDockerStats(out string) []ContainerStats {
	stats := make([]ContainerStats, 0)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		fields := splitTabs(line)
		if len(fields) < 8 {
			continue
		}
		id := fields[0]
		if len(id) > 12 {
			id = id[:12]
		}
		stats = append(stats, ContainerStats{
			ID:       id,
			Name:     fields[1],
			CPUPerc:  fields[2],
			MemUsage: fields[3],
			MemPerc:  fields[4],
			NetIO:    fields[5],
			BlockIO:  fields[6],
			PIDs:     fields[7],
		})
	}
	return stats
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
