package server

import (
	"encoding/json"
	"net/http"
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
