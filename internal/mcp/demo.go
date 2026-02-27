package mcp

import "fmt"

func (s *Server) executeDemoTool(name string, args map[string]any) (any, error) {
	server := stringArg(args, "server")

	switch name {
	case "system_status":
		return demoStatus(server), nil
	case "docker_list":
		return demoDocker(server), nil
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
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func demoStatus(server string) map[string]any {
	switch server {
	case "nas-box":
		return map[string]any{
			"hostname": "nas-box", "os": "linux", "arch": "amd64", "uptime": "12d 3h",
			"time": "2026-02-27T14:30:00Z",
			"cpu":  map[string]any{"usage_percent": 5.2, "cores": 4},
			"memory": map[string]any{"total_gb": 16.0, "used_gb": 6.8, "usage_percent": 42.5},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 120.0, "used_gb": 32.0, "usage_percent": 26.7},
				{"mount": "/mnt/storage", "total_gb": 8000.0, "used_gb": 4960.0, "usage_percent": 62.0},
			},
		}
	case "raspberry-pi":
		return map[string]any{
			"hostname": "raspberry-pi", "os": "linux", "arch": "arm64", "uptime": "28d 7h",
			"time": "2026-02-27T14:30:00Z",
			"cpu":  map[string]any{"usage_percent": 12.1, "cores": 4},
			"memory": map[string]any{"total_gb": 4.0, "used_gb": 2.1, "usage_percent": 52.5},
			"disks": []map[string]any{
				{"mount": "/", "total_gb": 64.0, "used_gb": 18.0, "usage_percent": 28.1},
			},
		}
	default:
		return map[string]any{
			"hostname": "homelab-server", "os": "linux", "arch": "amd64", "uptime": "4d 12h",
			"time": "2026-02-27T14:30:00Z",
			"cpu":  map[string]any{"usage_percent": 23.4, "cores": 8},
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
