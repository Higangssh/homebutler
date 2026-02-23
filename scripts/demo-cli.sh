#!/bin/bash
# CLI demo script for GIF recording - no emoji, no ANSI colors

prompt() {
    printf "$ %s\n" "$1"
    sleep 0.5
}

prompt "homebutler status"
cat <<'EOF'
{
  "hostname": "homelab-server",
  "os": "linux",
  "arch": "arm64",
  "uptime": "42d 7h",
  "cpu": {"usage_percent": 23.5, "cores": 4},
  "memory": {"total_gb": 8, "used_gb": 3.2, "usage_percent": 40.0},
  "disks": [
    {"mount": "/", "total_gb": 128, "used_gb": 47, "usage_percent": 37}
  ]
}
EOF
sleep 2

prompt "homebutler docker list"
cat <<'EOF'
[
  {"id": "a1b2c3d4e5f6", "name": "nginx-proxy", "image": "nginx:latest", "status": "Up 12 days", "state": "running"},
  {"id": "f6e5d4c3b2a1", "name": "postgres-db", "image": "postgres:16", "status": "Up 12 days", "state": "running"},
  {"id": "1a2b3c4d5e6f", "name": "grafana", "image": "grafana/grafana:latest", "status": "Up 5 days", "state": "running"}
]
EOF
sleep 2

prompt "homebutler alerts"
cat <<'EOF'
{
  "cpu": {"status": "ok", "current": 23.5, "threshold": 90},
  "memory": {"status": "ok", "current": 40.0, "threshold": 85},
  "disks": [
    {"mount": "/", "status": "warning", "current": 82, "threshold": 80}
  ]
}
EOF
sleep 2

prompt "homebutler status --all"
cat <<'EOF'
[
  {"server": "homelab", "data": {"hostname": "homelab-server", "cpu": {"usage_percent": 23.5}, "memory": {"usage_percent": 40.0}}},
  {"server": "nas", "data": {"hostname": "nas-01", "cpu": {"usage_percent": 8.2}, "memory": {"usage_percent": 40.0}}}
]
EOF
sleep 3
