package mcp

import (
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/install"
)

func (s *Server) executeDemoTool(name string, args map[string]any) (any, error) {
	server := stringArg(args, "server")

	switch name {
	case "system_status":
		return demoStatus(server), nil
	case "docker_list":
		return demoDocker(server), nil
	case "docker_stats":
		return demoDockerStats(server), nil
	case "docker_restart":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return map[string]any{"action": "restart", "container": cname, "status": "restarted"}, nil
	case "docker_stop":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return map[string]any{"action": "stop", "container": cname, "status": "stopped"}, nil
	case "docker_logs":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return demoLogs(cname), nil
	case "wake":
		target, ok := requireString(args, "target")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: target")
		}
		return map[string]any{"action": "wake", "target": target, "broadcast": "255.255.255.255", "status": "sent"}, nil
	case "open_ports":
		return demoPorts(server), nil
	case "network_scan":
		return demoNetworkScan(), nil
	case "alerts":
		return demoAlerts(server), nil
	case "inventory_scan":
		return map[string]any{
			"server_name": serverOrDefault(server),
			"system":      demoStatus(server),
			"containers":  demoDocker(server),
			"ports":       demoPorts(server),
		}, nil
	case "inventory_export":
		format := stringArg(args, "format")
		if format == "" {
			format = "mermaid"
		}
		if format == "json" {
			return map[string]any{"server_name": serverOrDefault(server), "ports": demoPorts(server)}, nil
		}
		return map[string]any{"format": "mermaid", "content": "graph TD\n  home[\"🏠 Home Network\"] --> server[\"🖥 " + serverOrDefault(server) + "\"]\n"}, nil
	case "report":
		return map[string]any{
			"server_name":       serverOrDefault(server),
			"status":            []string{"CPU 23%, memory 39%, disk 38%"},
			"needs_attention":   []string{},
			"notable_changes":   []string{"Demo baseline created"},
			"suggested_actions": []string{"Run inventory_scan for topology details"},
			"snapshot_saved":    !boolArg(args, "no_save"),
		}, nil
	case "doctor":
		return map[string]any{
			"server_name": serverOrDefault(server),
			"status":      "warn",
			"summary":     map[string]any{"pass": 0, "warn": 2, "fail": 0},
			"findings": []map[string]any{
				{"severity": "warn", "category": "exposure", "title": "4 port(s) are listening on all interfaces", "action": "Make sure each one is intentional and protected.", "command": "homebutler inventory scan"},
				{"severity": "warn", "category": "backup", "title": "Latest backup is older than expected", "action": "Run a fresh backup. If this app matters, follow up with a backup drill.", "command": "homebutler backup"},
			},
		}, nil
	case "watch_check":
		// Carries a skipped target on purpose: a demo that only ever returned
		// incidents would teach a caller that an empty incident list means the
		// whole watch list is healthy, which is the misreading the real
		// command's Skipped field exists to prevent.
		return map[string]any{
			"checked": 2,
			"incidents": []map[string]any{
				{
					"id":              "demo-nextcloud-20260430T120000Z",
					"container":       "nextcloud",
					"detected_at":     "2026-04-30T12:00:00Z",
					"restart_count":   3,
					"prev_started_at": "2026-04-30T09:14:02Z",
					"curr_started_at": "2026-04-30T11:58:41Z",
					"post_logs":       "MySQL server has gone away\nRetrying connection (1/5)",
				},
			},
			"skipped": []map[string]any{
				{"name": "caddy.service", "kind": "systemd"},
			},
		}, nil

	case "watch_history":
		return demoWatchHistory(args), nil

	case "watch_list":
		// Mirrors demoWatchHistory's targets, and carries a systemd entry with
		// no last_checked: watch check cannot inspect those, so a caller that
		// only ever saw polled docker targets would not learn that some of the
		// list is only covered while watch start is running.
		return []map[string]any{
			{"container": "nextcloud", "kind": "docker", "added_at": "2026-04-12 09:31:00", "restart_count": 3, "last_checked": "2026-04-30 12:00:00"},
			{"container": "postgres", "kind": "docker", "added_at": "2026-04-12 09:31:12", "restart_count": 0, "last_checked": "2026-04-30 12:00:00"},
			{"container": "caddy.service", "kind": "systemd", "added_at": "2026-04-18 22:04:00", "restart_count": 0},
		}, nil

	case "backup_create":
		service := stringArg(args, "service")
		if service == "" {
			service = "all"
		}
		return map[string]any{"archive": "~/.homebutler/backups/demo.tar.gz", "services": []string{service}, "volumes": 2, "size": "12.3 MB"}, nil
	case "backup_list":
		return []map[string]any{{"name": "demo.tar.gz", "path": "~/.homebutler/backups/demo.tar.gz", "size": "12.3 MB", "created_at": "2026-04-30T12:00:00Z"}}, nil
	case "backup_drill":
		if boolArg(args, "all") {
			return map[string]any{"total": 1, "passed": 1, "failed": 0}, nil
		}
		app, ok := requireString(args, "app")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: app (or set all=true)")
		}
		return map[string]any{"app": app, "passed": true, "integrity": true, "booted": true, "health_status": 200}, nil
	case "backup_restore":
		archive, ok := requireString(args, "archive")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: archive")
		}
		return map[string]any{"archive": archive, "services": []string{"demo"}, "volumes": 1}, nil
	case "install_list":
		// The catalogue is a static map compiled into the binary, so demo mode
		// can answer this truthfully instead of inventing apps that would drift
		// away from the real list.
		return install.List(), nil

	case "install_status":
		app := stringArg(args, "app")
		if _, ok := install.Registry[app]; !ok {
			return nil, fmt.Errorf("unknown app %q, use install_list to see available apps", app)
		}
		return map[string]any{"app": app, "state": "running"}, nil

	case "install_app":
		app := stringArg(args, "app")
		a, ok := install.Registry[app]
		if !ok {
			return nil, fmt.Errorf("unknown app %q, use install_list to see available apps", app)
		}
		port := a.DefaultPort
		if p := stringArg(args, "port"); p != "" {
			port = p
		}
		return map[string]any{
			"status": "installed",
			"app":    a.Name,
			"port":   port,
			"path":   "/home/demo/.homebutler/apps/" + a.Name,
			"state":  "running",
		}, nil

	case "install_uninstall":
		app := stringArg(args, "app")
		if _, ok := install.Registry[app]; !ok {
			return nil, fmt.Errorf("unknown app %q, use install_list to see available apps", app)
		}
		return map[string]any{"status": "uninstalled", "app": app, "data_preserved": true}, nil

	case "install_purge":
		app := stringArg(args, "app")
		if _, ok := install.Registry[app]; !ok {
			return nil, fmt.Errorf("unknown app %q, use install_list to see available apps", app)
		}
		return map[string]any{"status": "purged", "app": app}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// demoDockerStats mirrors demoDocker's containers so the two tools do not
// describe different machines.
func demoDockerStats(server string) []map[string]any {
	switch server {
	case "nas-box":
		return []map[string]any{
			{"id": "aa11bb22cc33", "name": "samba", "cpu_percent": "0.42%", "mem_usage": "88MiB / 8GiB", "mem_percent": "1.07%", "net_io": "1.2GB / 340MB", "block_io": "820MB / 4.1GB", "pids": "12"},
			{"id": "dd44ee55ff66", "name": "plex", "cpu_percent": "18.30%", "mem_usage": "1.4GiB / 8GiB", "mem_percent": "17.50%", "net_io": "44GB / 2.1GB", "block_io": "12GB / 900MB", "pids": "48"},
		}
	case "raspberry-pi":
		return []map[string]any{
			{"id": "pi11pi22pi33", "name": "pihole", "cpu_percent": "1.10%", "mem_usage": "76MiB / 1GiB", "mem_percent": "7.42%", "net_io": "3.4GB / 2.9GB", "block_io": "120MB / 640MB", "pids": "9"},
		}
	default:
		return []map[string]any{
			{"id": "a1b2c3d4e5f6", "name": "nginx", "cpu_percent": "0.15%", "mem_usage": "24MiB / 16GiB", "mem_percent": "0.15%", "net_io": "890MB / 1.1GB", "block_io": "12MB / 8MB", "pids": "5"},
			{"id": "b2c3d4e5f6a1", "name": "postgres", "cpu_percent": "2.80%", "mem_usage": "412MiB / 16GiB", "mem_percent": "2.51%", "net_io": "220MB / 180MB", "block_io": "3.4GB / 9.8GB", "pids": "21"},
			{"id": "c3d4e5f6a1b2", "name": "redis", "cpu_percent": "0.60%", "mem_usage": "38MiB / 16GiB", "mem_percent": "0.23%", "net_io": "140MB / 96MB", "block_io": "44MB / 12MB", "pids": "6"},
			{"id": "d4e5f6a1b2c3", "name": "grafana", "cpu_percent": "1.90%", "mem_usage": "186MiB / 16GiB", "mem_percent": "1.13%", "net_io": "310MB / 420MB", "block_io": "88MB / 210MB", "pids": "14"},
			{"id": "e5f6a1b2c3d4", "name": "prometheus", "cpu_percent": "4.20%", "mem_usage": "740MiB / 16GiB", "mem_percent": "4.51%", "net_io": "1.8GB / 640MB", "block_io": "5.2GB / 14GB", "pids": "17"},
		}
	}
}

func serverOrDefault(server string) string {
	if server != "" {
		return server
	}
	return "homelab-server"
}

func demoStatus(server string) map[string]any {
	switch server {
	case "nas-box":
		return map[string]any{
			"hostname": "nas-box", "os": "linux", "arch": "amd64", "uptime": "12d 3h",
			"time":   "2026-02-27T14:30:00Z",
			"cpu":    map[string]any{"usage_percent": 5.2, "cores": 4},
			"memory": map[string]any{"total_gb": 16.0, "used_gb": 6.8, "usage_percent": 42.5},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 120.0, "used_gb": 32.0, "usage_percent": 26.7},
				{"mount": "/mnt/storage", "total_gb": 8000.0, "used_gb": 4960.0, "usage_percent": 62.0},
			},
		}
	case "raspberry-pi":
		return map[string]any{
			"hostname": "raspberry-pi", "os": "linux", "arch": "arm64", "uptime": "28d 7h",
			"time":   "2026-02-27T14:30:00Z",
			"cpu":    map[string]any{"usage_percent": 12.1, "cores": 4},
			"memory": map[string]any{"total_gb": 4.0, "used_gb": 2.1, "usage_percent": 52.5},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 64.0, "used_gb": 18.0, "usage_percent": 28.1},
			},
		}
	default:
		return map[string]any{
			"hostname": "homelab-server", "os": "linux", "arch": "amd64", "uptime": "4d 12h",
			"time":   "2026-02-27T14:30:00Z",
			"cpu":    map[string]any{"usage_percent": 23.4, "cores": 8},
			"memory": map[string]any{"total_gb": 32.0, "used_gb": 12.4, "usage_percent": 38.8},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 500.0, "used_gb": 187.5, "usage_percent": 37.5},
				{"mount": "/mnt/data", "total_gb": 2000.0, "used_gb": 1740.0, "usage_percent": 87.0},
			},
		}
	}
}

func demoDocker(server string) map[string]any {
	switch server {
	case "nas-box":
		return map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "aa11bb22cc33", "name": "samba", "image": "dperson/samba:latest", "status": "Up 12 days", "state": "running", "ports": "445/tcp"},
				{"id": "dd44ee55ff66", "name": "plex", "image": "plexinc/pms-docker:latest", "status": "Up 12 days", "state": "running", "ports": "0.0.0.0:32400->32400/tcp"},
			},
		}
	case "raspberry-pi":
		return map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "pi11pi22pi33", "name": "pihole", "image": "pihole/pihole:latest", "status": "Up 28 days", "state": "running", "ports": "0.0.0.0:53->53/tcp, 0.0.0.0:80->80/tcp"},
			},
		}
	default:
		return map[string]any{
			"available": true,
			"containers": []map[string]any{
				{"id": "a1b2c3d4e5f6", "name": "nginx", "image": "nginx:1.25-alpine", "status": "Up 4 days", "state": "running", "ports": "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
				{"id": "b2c3d4e5f6a1", "name": "postgres", "image": "postgres:16", "status": "Up 4 days", "state": "running", "ports": "5432/tcp"},
				{"id": "c3d4e5f6a1b2", "name": "redis", "image": "redis:7-alpine", "status": "Up 4 days", "state": "running", "ports": "6379/tcp"},
				{"id": "d4e5f6a1b2c3", "name": "grafana", "image": "grafana/grafana:10.2", "status": "Up 3 days", "state": "running", "ports": "0.0.0.0:3000->3000/tcp"},
				{"id": "e5f6a1b2c3d4", "name": "prometheus", "image": "prom/prometheus:v2.48", "status": "Up 3 days", "state": "running", "ports": "0.0.0.0:9090->9090/tcp"},
				{"id": "f6a1b2c3d4e5", "name": "backup", "image": "restic/restic:0.16", "status": "Exited (0) 6h ago", "state": "exited", "ports": ""},
			},
		}
	}
}

func demoLogs(container string) map[string]any {
	logs := map[string]string{
		"nginx":    "2026/02/27 14:25:01 [notice] 1#1: start worker process 29\n2026/02/27 14:28:33 192.168.1.5 - - \"GET /api/health HTTP/1.1\" 200 2\n2026/02/27 14:29:01 192.168.1.10 - - \"GET / HTTP/1.1\" 200 612\n2026/02/27 14:30:15 192.168.1.20 - - \"GET /dashboard HTTP/1.1\" 304 0",
		"postgres": "2026-02-27 14:25:00 UTC [1] LOG:  database system is ready to accept connections\n2026-02-27 14:28:00 UTC [45] LOG:  checkpoint starting: time\n2026-02-27 14:28:05 UTC [45] LOG:  checkpoint complete",
		"backup":   "2026-02-27 08:00:01 Starting backup...\n2026-02-27 08:12:33 Files: 2847 new, 156 changed, 98432 unmodified\n2026-02-27 08:12:33 Added: 1.284 GiB\n2026-02-27 08:12:34 Backup completed successfully",
	}
	text, ok := logs[container]
	if !ok {
		text = fmt.Sprintf("No recent logs for container %q", container)
	}
	return map[string]any{"container": container, "logs": text}
}

func demoPorts(server string) []map[string]any {
	switch server {
	case "nas-box":
		return []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "445", "pid": "1100", "process": "smbd"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "32400", "pid": "1200", "process": "plex"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "22", "pid": "200", "process": "sshd"},
		}
	case "raspberry-pi":
		return []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "53", "pid": "800", "process": "pihole-FTL"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "80", "pid": "900", "process": "lighttpd"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "22", "pid": "300", "process": "sshd"},
		}
	default:
		return []map[string]any{
			{"protocol": "tcp", "address": "0.0.0.0", "port": "80", "pid": "1234", "process": "nginx"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "443", "pid": "1234", "process": "nginx"},
			{"protocol": "tcp", "address": "127.0.0.1", "port": "5432", "pid": "2345", "process": "postgres"},
			{"protocol": "tcp", "address": "127.0.0.1", "port": "6379", "pid": "5678", "process": "redis-server"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "3000", "pid": "6789", "process": "grafana"},
			{"protocol": "tcp", "address": "0.0.0.0", "port": "9090", "pid": "7890", "process": "prometheus"},
		}
	}
}

func demoNetworkScan() []map[string]any {
	return []map[string]any{
		{"ip": "192.168.1.1", "mac": "00:11:22:33:44:55", "hostname": "router.local"},
		{"ip": "192.168.1.10", "mac": "AA:BB:CC:11:22:33", "hostname": "homelab-server"},
		{"ip": "192.168.1.20", "mac": "DD:EE:FF:44:55:66", "hostname": "nas-box"},
		{"ip": "192.168.1.30", "mac": "11:22:33:AA:BB:CC", "hostname": "raspberry-pi"},
		{"ip": "192.168.1.50", "mac": "44:55:66:DD:EE:FF", "hostname": "gaming-pc"},
	}
}

func demoAlerts(server string) map[string]any {
	switch server {
	case "nas-box":
		return map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 5.2, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 42.5, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 26.7, "threshold": 90.0},
				{"mount": "/mnt/storage", "status": "warning", "current": 62.0, "threshold": 70.0},
			},
		}
	case "raspberry-pi":
		return map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 12.1, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 52.5, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 28.1, "threshold": 90.0},
			},
		}
	default:
		return map[string]any{
			"cpu":    map[string]any{"status": "ok", "current": 23.4, "threshold": 90.0},
			"memory": map[string]any{"status": "ok", "current": 38.8, "threshold": 85.0},
			"disks": []map[string]any{
				{"mount": "/", "status": "ok", "current": 37.5, "threshold": 90.0},
				{"mount": "/mnt/data", "status": "warning", "current": 87.0, "threshold": 90.0},
			},
		}
	}
}

// demoWatchHistory honours limit, container and include_logs so a caller can
// see what those arguments actually do. A demo that ignored them would teach
// the wrong thing about the tool it is demonstrating — particularly
// include_logs, whose whole purpose is that the expensive shape is opt-in.
func demoWatchHistory(args map[string]any) []map[string]any {
	incidents := []map[string]any{
		{
			"id": "demo-nextcloud-20260430T120000Z", "container": "nextcloud",
			"detected_at": "2026-04-30T12:00:00Z", "restart_count": 3,
			"prev_started_at": "2026-04-30T09:14:02Z", "curr_started_at": "2026-04-30T11:58:41Z",
			"pre_logs":  "PHP Fatal error: Allowed memory size exhausted",
			"post_logs": "MySQL server has gone away\nRetrying connection (1/5)",
		},
		{
			"id": "demo-nextcloud-20260429T031500Z", "container": "nextcloud",
			"detected_at": "2026-04-29T03:15:00Z", "restart_count": 2,
			"prev_started_at": "2026-04-28T20:02:11Z", "curr_started_at": "2026-04-29T03:14:47Z",
			"pre_logs":  "Redis connection refused",
			"post_logs": "Starting apache2",
		},
		{
			"id": "demo-postgres-20260427T181200Z", "container": "postgres",
			"detected_at": "2026-04-27T18:12:00Z", "restart_count": 1,
			"prev_started_at": "2026-04-20T11:00:00Z", "curr_started_at": "2026-04-27T18:11:30Z",
			"pre_logs":  "received fast shutdown request",
			"post_logs": "database system is ready to accept connections",
		},
	}

	if c := stringArg(args, "container"); c != "" {
		filtered := incidents[:0:0]
		for _, inc := range incidents {
			if strings.EqualFold(inc["container"].(string), c) {
				filtered = append(filtered, inc)
			}
		}
		incidents = filtered
	}

	if n := intArg(args, "limit", 10); n > 0 && len(incidents) > n {
		incidents = incidents[:n]
	}

	if !boolArg(args, "include_logs") {
		stripped := make([]map[string]any, 0, len(incidents))
		for _, inc := range incidents {
			c := make(map[string]any, len(inc))
			for k, v := range inc {
				if k == "pre_logs" || k == "post_logs" {
					continue
				}
				c[k] = v
			}
			stripped = append(stripped, c)
		}
		incidents = stripped
	}

	return incidents
}
