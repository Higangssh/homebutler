package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/watch"
)

// demoServerName returns the server name from the ?server query param.
// Returns "" if not set or if it matches the local server (homelab-server).
func demoServerName(r *http.Request) string {
	name := r.URL.Query().Get("server")
	if name == "" || name == "homelab-server" {
		return ""
	}
	return name
}

// demoOfflineError writes a server offline error for unknown demo servers.
func demoOfflineError(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]string{"error": "server " + name + " is offline"})
}

// demoStatus returns realistic demo system status.
func (s *Server) demoStatus(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, map[string]any{
			"hostname": "homelab-server",
			"os":       "linux",
			"arch":     "amd64",
			"uptime":   "4d 12h",
			"time":     "2026-02-27T14:30:00Z",
			"cpu": map[string]any{
				"usage_percent": 23.4,
				"cores":         8,
			},
			"memory": map[string]any{
				"total_gb":      32.0,
				"used_gb":       12.4,
				"usage_percent": 38.8,
			},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 500.0, "used_gb": 187.5, "usage_percent": 37.5},
				{"mount": "/mnt/data", "total_gb": 2000.0, "used_gb": 1740.0, "usage_percent": 87.0},
			},
		})
	case "nas-box":
		writeJSON(w, map[string]any{
			"hostname": "nas-box",
			"os":       "linux",
			"arch":     "amd64",
			"uptime":   "12d 3h",
			"time":     "2026-02-27T14:30:00Z",
			"cpu": map[string]any{
				"usage_percent": 5.2,
				"cores":         4,
			},
			"memory": map[string]any{
				"total_gb":      16.0,
				"used_gb":       6.8,
				"usage_percent": 42.5,
			},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 120.0, "used_gb": 32.0, "usage_percent": 26.7},
				{"mount": "/mnt/storage", "total_gb": 8000.0, "used_gb": 4960.0, "usage_percent": 62.0},
			},
		})
	case "raspberry-pi":
		writeJSON(w, map[string]any{
			"hostname": "raspberry-pi",
			"os":       "linux",
			"arch":     "arm64",
			"uptime":   "28d 7h",
			"time":     "2026-02-27T14:30:00Z",
			"cpu": map[string]any{
				"usage_percent": 12.1,
				"cores":         4,
			},
			"memory": map[string]any{
				"total_gb":      4.0,
				"used_gb":       2.1,
				"usage_percent": 52.5,
			},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 64.0, "used_gb": 18.0, "usage_percent": 28.1},
			},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoDocker returns realistic demo container data.
func (s *Server) demoDocker(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "a1b2c3d4e5f6", "name": "nginx", "image": "nginx:1.25-alpine", "status": "Up 4 days", "state": "running", "ports": "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
				{"id": "b2c3d4e5f6a1", "name": "postgres", "image": "postgres:16", "status": "Up 4 days", "state": "running", "ports": "5432/tcp"},
				{"id": "c3d4e5f6a1b2", "name": "redis", "image": "redis:7-alpine", "status": "Up 4 days", "state": "running", "ports": "6379/tcp"},
				{"id": "d4e5f6a1b2c3", "name": "grafana", "image": "grafana/grafana:10.2", "status": "Up 3 days", "state": "running", "ports": "0.0.0.0:3000->3000/tcp"},
				{"id": "e5f6a1b2c3d4", "name": "prometheus", "image": "prom/prometheus:v2.48", "status": "Up 3 days", "state": "running", "ports": "0.0.0.0:9090->9090/tcp"},
				{"id": "f6a1b2c3d4e5", "name": "backup", "image": "restic/restic:0.16", "status": "Stopped · 6h ago", "state": "exited", "ports": ""},
			},
		})
	case "nas-box":
		writeJSON(w, map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "aa11bb22cc33", "name": "samba", "image": "dperson/samba:latest", "status": "Up 12 days", "state": "running", "ports": "445/tcp"},
				{"id": "dd44ee55ff66", "name": "plex", "image": "plexinc/pms-docker:latest", "status": "Up 12 days", "state": "running", "ports": "0.0.0.0:32400->32400/tcp"},
			},
		})
	case "raspberry-pi":
		writeJSON(w, map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "pi11pi22pi33", "name": "pihole", "image": "pihole/pihole:latest", "status": "Up 28 days", "state": "running", "ports": "0.0.0.0:53->53/tcp, 0.0.0.0:80->80/tcp"},
			},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoDockerStats returns realistic demo container stats data.
func (s *Server) demoDockerStats(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, map[string]any{
			"available": true,
			"stats": []map[string]any{
				{"id": "a1b2c3d4e5f6", "name": "nginx", "cpu_percent": "0.50%", "mem_usage": "10.5MiB / 1.94GiB", "mem_percent": "0.53%", "net_io": "1.2kB / 3.4kB", "block_io": "0B / 0B", "pids": "2"},
				{"id": "b2c3d4e5f6a1", "name": "postgres", "cpu_percent": "1.20%", "mem_usage": "256MiB / 1.94GiB", "mem_percent": "12.89%", "net_io": "5.6MB / 7.8MB", "block_io": "100MB / 50MB", "pids": "15"},
				{"id": "c3d4e5f6a1b2", "name": "redis", "cpu_percent": "0.10%", "mem_usage": "8.5MiB / 1.94GiB", "mem_percent": "0.43%", "net_io": "2.3kB / 1.1kB", "block_io": "0B / 4.1kB", "pids": "5"},
				{"id": "d4e5f6a1b2c3", "name": "grafana", "cpu_percent": "0.30%", "mem_usage": "45MiB / 1.94GiB", "mem_percent": "2.26%", "net_io": "12.3MB / 45.6MB", "block_io": "8.2MB / 0B", "pids": "12"},
				{"id": "e5f6a1b2c3d4", "name": "prometheus", "cpu_percent": "2.10%", "mem_usage": "512MiB / 1.94GiB", "mem_percent": "25.76%", "net_io": "34.5MB / 12.3MB", "block_io": "256MB / 128MB", "pids": "8"},
			},
		})
	case "nas-box":
		writeJSON(w, map[string]any{
			"available": true,
			"stats": []map[string]any{
				{"id": "aa11bb22cc33", "name": "samba", "cpu_percent": "0.80%", "mem_usage": "32MiB / 8GiB", "mem_percent": "0.39%", "net_io": "1.2GB / 3.4GB", "block_io": "500MB / 200MB", "pids": "4"},
				{"id": "dd44ee55ff66", "name": "plex", "cpu_percent": "15.30%", "mem_usage": "1.2GiB / 8GiB", "mem_percent": "15.00%", "net_io": "2.3GB / 8.9GB", "block_io": "1.5GB / 200MB", "pids": "25"},
			},
		})
	case "raspberry-pi":
		writeJSON(w, map[string]any{
			"available": true,
			"stats": []map[string]any{
				{"id": "pi11pi22pi33", "name": "pihole", "cpu_percent": "3.20%", "mem_usage": "128MiB / 4GiB", "mem_percent": "3.13%", "net_io": "45.6MB / 23.4MB", "block_io": "12MB / 8MB", "pids": "6"},
			},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoProcesses returns realistic demo process data.
func (s *Server) demoProcesses(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, []map[string]any{
			{"name": "nginx", "pid": 1234, "cpu": 2.1, "mem": 0.8},
			{"name": "postgres", "pid": 2345, "cpu": 8.5, "mem": 4.2},
			{"name": "node", "pid": 3456, "cpu": 5.3, "mem": 3.1},
			{"name": "go", "pid": 4567, "cpu": 3.7, "mem": 1.9},
			{"name": "dockerd", "pid": 890, "cpu": 1.8, "mem": 2.5},
			{"name": "redis-server", "pid": 5678, "cpu": 1.2, "mem": 0.6},
			{"name": "grafana", "pid": 6789, "cpu": 0.9, "mem": 1.4},
			{"name": "prometheus", "pid": 7890, "cpu": 0.7, "mem": 1.1},
			{"name": "containerd", "pid": 456, "cpu": 0.5, "mem": 0.9},
			{"name": "sshd", "pid": 123, "cpu": 0.1, "mem": 0.2},
		})
	case "nas-box":
		writeJSON(w, []map[string]any{
			{"name": "smbd", "pid": 1100, "cpu": 1.8, "mem": 1.2},
			{"name": "plex", "pid": 1200, "cpu": 3.1, "mem": 5.4},
			{"name": "dockerd", "pid": 800, "cpu": 0.5, "mem": 1.0},
			{"name": "mdadm", "pid": 500, "cpu": 0.3, "mem": 0.2},
			{"name": "sshd", "pid": 200, "cpu": 0.1, "mem": 0.1},
		})
	case "raspberry-pi":
		writeJSON(w, []map[string]any{
			{"name": "pihole-FTL", "pid": 800, "cpu": 5.2, "mem": 3.8},
			{"name": "lighttpd", "pid": 900, "cpu": 1.1, "mem": 1.5},
			{"name": "dockerd", "pid": 600, "cpu": 2.3, "mem": 4.1},
			{"name": "sshd", "pid": 300, "cpu": 0.1, "mem": 0.3},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoAlerts returns realistic demo alert data.
func (s *Server) demoAlerts(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 23.4, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 38.8, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 37.5, "threshold": 90.0},
				{"mount": "/mnt/data", "status": "warning", "current": 87.0, "threshold": 90.0},
			},
		})
	case "nas-box":
		writeJSON(w, map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 5.2, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 42.5, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 26.7, "threshold": 90.0},
				{"mount": "/mnt/storage", "status": "warning", "current": 62.0, "threshold": 70.0},
			},
		})
	case "raspberry-pi":
		writeJSON(w, map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 12.1, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 52.5, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 28.1, "threshold": 90.0},
			},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoPorts returns realistic demo ports data.
func (s *Server) demoPorts(w http.ResponseWriter, r *http.Request) {
	name := demoServerName(r)

	switch name {
	case "":
		writeJSON(w, []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "80", "pid": "1234", "process": "nginx"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "443", "pid": "1234", "process": "nginx"},
			{"protocol": "tcp", "address": "127.0.0.1", "port": "5432", "pid": "2345", "process": "postgres"},
			{"protocol": "tcp", "address": "127.0.0.1", "port": "6379", "pid": "5678", "process": "redis-server"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "3000", "pid": "6789", "process": "grafana"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "8080", "pid": "4567", "process": "homebutler"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "9090", "pid": "7890", "process": "prometheus"},
		})
	case "nas-box":
		writeJSON(w, []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "445", "pid": "1100", "process": "smbd"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "32400", "pid": "1200", "process": "plex"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "22", "pid": "200", "process": "sshd"},
		})
	case "raspberry-pi":
		writeJSON(w, []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "53", "pid": "800", "process": "pihole-FTL"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "80", "pid": "900", "process": "lighttpd"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "22", "pid": "300", "process": "sshd"},
		})
	default:
		demoOfflineError(w, name)
	}
}

// demoWake returns realistic demo WoL targets.
func (s *Server) demoWake(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, []map[string]any{
		{"name": "nas-server", "mac": "AA:BB:CC:11:22:33"},
		{"name": "gaming-pc", "mac": "DD:EE:FF:44:55:66"},
		{"name": "media-center", "mac": "11:22:33:AA:BB:CC"},
	})
}

// demoWakeSend simulates sending a WoL packet.
func (s *Server) demoWakeSend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	writeJSON(w, map[string]any{
		"action":    "wake",
		"target":    name,
		"broadcast": "255.255.255.255",
		"status":    "sent",
	})
}

// demoConfig returns realistic demo config data.
func (s *Server) demoConfig(w http.ResponseWriter, r *http.Request) {
	editable := s.token != ""
	body := map[string]any{
		"path": "~/.config/homebutler/config.yaml",
		"servers": []map[string]any{
			{"name": "mac-mini", "host": "192.168.1.10", "local": true, "user": "", "port": 0, "auth": "key", "key": "", "password_set": false},
			{"name": "nas-box", "host": "192.168.1.20", "local": false, "user": "admin", "port": 22, "auth": "key", "key": "id_rsa", "password_set": false},
			// Whether a password is set, never a stand-in for one.
			{"name": "raspberry-pi", "host": "192.168.1.30", "local": false, "user": "pi", "port": 22, "auth": "password", "key": "", "password_set": true},
		},
		"alerts": map[string]any{"cpu": 90, "memory": 85, "disk": 90},
		"wake": []map[string]string{
			{"name": "gaming-pc", "mac": "AA:BB:CC:DD:EE:FF", "broadcast": "192.168.1.255"},
		},
		"notify": s.notifySettings(demoNotifyConfig()),
		// Demo mode is started with a token by the end-to-end run, so the
		// editing surface is the one being exercised.
		"editable":          editable,
		"transport_warning": s.transportWarning(),
	}
	if editable {
		body["revision"] = demoRevision
	}
	writeJSON(w, body)
}

// demoNotifyConfig is what the demo dashboard shows on the settings screen.
//
// It is made up here rather than read from s.config(), which in demo mode is
// still the config of whoever started the process: a demo is supposed to show
// nothing real, and the channel list alone says which services someone uses.
// The three states a form has to handle are all present — a channel that is
// set up, one that is set up without its optional credential, and four that
// have never been touched.
func demoNotifyConfig() *config.Config {
	return &config.Config{Notify: notify.ProviderConfig{
		Ntfy:   &notify.NtfyConfig{URL: "https://ntfy.sh", Topic: "demo-topic", Token: "demo-token"},
		Gotify: &notify.GotifyConfig{URL: "https://gotify.example.com", Token: "demo-token"},
	}}
}

// demoNotifyTest reports what a test would have found without sending
// anything. One channel fails, because a settings screen that has only ever
// shown success is one whose failure path nobody has looked at.
func (s *Server) demoNotifyTest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, notifyTestResponse{Results: []notify.TestResult{
		{Channel: notify.ChannelNtfy, Sent: true},
		{Channel: notify.ChannelGotify, Sent: false, Error: "gotify: https://gotify.example.com returned 401 unauthorized"},
	}})
}

// demoRevision stands in for the hash of a file demo mode does not have. It is
// fixed so the end-to-end suite can send it back.
const demoRevision = "0000000000000000000000000000000000000000000000000000000000000000"

// demoSaveAlerts accepts a save and reports what a real one reports. Demo mode
// writes nothing, so the revision it answers with is the one it was given: the
// page stays consistent with itself across a save.
func (s *Server) demoSaveAlerts(w http.ResponseWriter, r *http.Request) {
	var req alertsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if req.Revision != demoRevision {
		writeError(w, http.StatusConflict, "the config file changed on disk since this page loaded it; reload before saving")
		return
	}
	writeJSON(w, saveResponse{Saved: true, Revision: demoRevision, RestartNeeded: []string{"watch", "alerts"}})
}

func (s *Server) demoSaveNotify(w http.ResponseWriter, r *http.Request) {
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if req.Revision != demoRevision {
		writeError(w, http.StatusConflict, "the config file changed on disk since this page loaded it; reload before saving")
		return
	}
	writeJSON(w, saveResponse{Saved: true, Revision: demoRevision, RestartNeeded: []string{"watch", "alerts"}})
}

// demoServers returns realistic demo server list.
func (s *Server) demoServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, []map[string]any{
		{"name": "homelab-server", "host": "192.168.1.10", "local": true, "status": "ok"},
		{"name": "nas-box", "host": "192.168.1.20", "local": false, "status": "ok"},
		{"name": "raspberry-pi", "host": "192.168.1.30", "local": false, "status": "ok"},
		{"name": "media-server", "host": "192.168.1.40", "local": false, "status": "ok"},
		{"name": "dev-vm", "host": "192.168.1.50", "local": false, "status": "ok"},
		{"name": "backup-nas", "host": "192.168.1.60", "local": false, "status": "error"},
		{"name": "docker-host", "host": "192.168.1.70", "local": false, "status": "ok"},
		{"name": "k3s-node-1", "host": "192.168.1.80", "local": false, "status": "ok"},
		{"name": "k3s-node-2", "host": "192.168.1.81", "local": false, "status": "ok"},
		{"name": "vpn-gateway", "host": "192.168.1.90", "local": false, "status": "error"},
	})
}

// demoServerStatus returns demo status for a named server.
func (s *Server) demoServerStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	data := map[string]map[string]any{
		"homelab-server": {
			"hostname": "homelab-server", "os": "linux", "arch": "amd64", "uptime": "4d 12h",
			"cpu":    map[string]any{"usage_percent": 23.4, "cores": 8},
			"memory": map[string]any{"total_gb": 32.0, "used_gb": 12.4, "usage_percent": 38.8},
			"disks":  []map[string]any{{"mount": "/", "total_gb": 500.0, "used_gb": 187.5, "usage_percent": 37.5}},
		},
		"nas-box": {
			"hostname": "nas-box", "os": "linux", "arch": "amd64", "uptime": "12d 3h",
			"cpu":    map[string]any{"usage_percent": 5.2, "cores": 4},
			"memory": map[string]any{"total_gb": 16.0, "used_gb": 6.8, "usage_percent": 42.5},
			"disks":  []map[string]any{{"mount": "/", "total_gb": 120.0, "used_gb": 32.0, "usage_percent": 26.7}},
		},
		"raspberry-pi": {
			"hostname": "raspberry-pi", "os": "linux", "arch": "arm64", "uptime": "28d 7h",
			"cpu":    map[string]any{"usage_percent": 12.1, "cores": 4},
			"memory": map[string]any{"total_gb": 8.0, "used_gb": 3.2, "usage_percent": 40.0},
			"disks":  []map[string]any{{"mount": "/", "total_gb": 64.0, "used_gb": 18.0, "usage_percent": 28.1}},
		},
	}

	if d, ok := data[name]; ok {
		writeJSON(w, d)
		return
	}

	// If the name doesn't match a demo server, try to return the first demo server's data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "server not found"})
}

// The demo watch list is the state doctor warns about: targets on the list and
// no service installed to poll them. Showing the healthy case would make the
// screen prettier and teach nobody what it is for.
func (s *Server) demoWatch(w http.ResponseWriter, r *http.Request) {
	added := time.Now().Add(-72 * time.Hour)
	writeJSON(w, watchOverview{
		Targets: []watch.Target{
			{Container: "plex", Kind: "docker", Unit: "plex", AddedAt: added},
			{Container: "gitea", Kind: "docker", Unit: "gitea", AddedAt: added.Add(time.Hour)},
			{Container: "node-exporter", Kind: "systemd", Unit: "node-exporter.service", AddedAt: added.Add(2 * time.Hour)},
		},
		Service:   watchService{Installed: false},
		Retention: watchRetention{Kept: 3, Max: 50},
	})
}

func demoIncidents() []watch.Incident {
	now := time.Now()
	exit137 := 137
	exit1 := 1
	return []watch.Incident{
		{
			ID:           "plex-20260912T031422Z",
			Container:    "plex",
			DetectedAt:   now.Add(-5 * time.Hour),
			RestartCount: 4,
			ExitCode:     &exit137,
			OOMKilled:    true,
			Flapping:     &watch.FlappingResult{IsFlapping: true, Level: "short", Count: 4, Window: "10m", Since: now.Add(-5*time.Hour - 10*time.Minute)},
			PreLogs:      "[transcode] session started for 1 client\n[transcode] buffer grew to 2.1 GB\nKilled",
			PostLogs:     "Starting Plex Media Server\n[transcode] ready",
		},
		{
			ID:           "gitea-20260911T221003Z",
			Container:    "gitea",
			DetectedAt:   now.Add(-29 * time.Hour),
			RestartCount: 1,
			ExitCode:     &exit1,
			PreLogs:      "level=fatal msg=\"unable to open database file: disk I/O error\"",
			PostLogs:     "level=info msg=\"gitea started\"",
		},
		{
			ID:           "node-exporter-20260910T084411Z",
			Container:    "node-exporter",
			DetectedAt:   now.Add(-3 * 24 * time.Hour),
			RestartCount: 1,
			PreLogs:      "caller=node_exporter.go msg=\"stopping\"",
			PostLogs:     "caller=node_exporter.go msg=\"listening on :9100\"",
		},
	}
}

func (s *Server) demoWatchIncidents(w http.ResponseWriter, r *http.Request) {
	// Same contract as the real handler: the list never carries logs.
	incidents := demoIncidents()
	stripped := make([]watch.Incident, len(incidents))
	copy(stripped, incidents)
	for i := range stripped {
		stripped[i].PreLogs = ""
		stripped[i].PostLogs = ""
	}
	writeJSON(w, stripped)
}

func (s *Server) demoWatchIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, incident := range demoIncidents() {
		if incident.ID == id {
			writeJSON(w, incident)
			return
		}
	}
	writeError(w, http.StatusNotFound, "incident not found")
}

// demoOverview answers the three states the real one can be in. A demo where
// every machine is current would make the screen prettier and leave the stale
// and unavailable branches — the ones that only appear when something is
// wrong — reachable by nobody.
func (s *Server) demoOverview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	lastGood := now.Add(-6 * time.Minute)

	reading := func(name, host string, local bool, cpu, memUsed, memTotal float64, uptime string) *system.StatusInfo {
		return &system.StatusInfo{
			Hostname: name, OS: "linux", Arch: "amd64", Uptime: uptime,
			CPU:    system.CPUInfo{UsagePercent: cpu, Cores: 8},
			Memory: system.MemInfo{TotalGB: memTotal, UsedGB: memUsed, Percent: memUsed / memTotal * 100},
			Disks:  []system.DiskInfo{{Mount: "/", TotalGB: 500, UsedGB: 187.5, Percent: 37.5}},
		}
	}

	writeJSON(w, overviewResponse{
		CollectedAt: now,
		Servers: []serverReading{
			{Name: "homelab-server", Host: "192.168.1.10", Local: true, Status: "current", UpdatedAt: &now,
				System: reading("homelab-server", "192.168.1.10", true, 23.4, 12.4, 32, "4d 12h")},
			{Name: "nas-box", Host: "192.168.1.20", Status: "current", UpdatedAt: &now,
				System: reading("nas-box", "192.168.1.20", false, 5.2, 6.8, 16, "12d 3h")},
			{Name: "raspberry-pi", Host: "192.168.1.30", Status: "current", UpdatedAt: &now,
				System: reading("raspberry-pi", "192.168.1.30", false, 12.1, 3.2, 8, "28d 7h")},
			// Answered six minutes ago and not since: the reading is still worth
			// showing, and saying how old it is, is the whole point of #146.
			{Name: "media-server", Host: "192.168.1.40", Status: "stale", UpdatedAt: &lastGood,
				System:       reading("media-server", "192.168.1.40", false, 61.0, 11.0, 16, "2d 1h"),
				FailureClass: remote.ClassUnreachable, Message: remote.Describe(remote.ClassUnreachable)},
			// Never read since this server started, so there is nothing to show.
			{Name: "backup-nas", Host: "192.168.1.60", Status: "unavailable",
				FailureClass: remote.ClassAuthentication, Message: remote.Describe(remote.ClassAuthentication)},
			// The one an operator has to look at from a terminal.
			{Name: "vpn-gateway", Host: "192.168.1.90", Status: "unavailable",
				FailureClass: remote.ClassHostKey, Message: remote.Describe(remote.ClassHostKey)},
		},
	})
}
