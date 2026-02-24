#!/bin/bash
# CLI demo script - human-readable output

prompt() {
    printf "$ %s\n" "$1"
    sleep 0.5
}

prompt "homebutler status"
cat <<'EOF'
🖥  homelab-server (linux/arm64)
   Uptime:  42d 7h
   CPU:     23.5% (4 cores)
   Memory:  3.2 / 8.0 GB (40.0%)
   Disk /:  47 / 128 GB (37%)
EOF
sleep 2

prompt "homebutler docker list"
cat <<'EOF'
CONTAINER            IMAGE                          STATE      STATUS
nginx-proxy          nginx:latest                   running    Up 12 days
postgres-db          postgres:16                    running    Up 12 days
grafana              grafana/grafana:latest          running    Up 5 days
EOF
sleep 2

prompt "homebutler alerts"
cat <<'EOF'
   CPU:     23.5% (threshold: 90%) ✅
   Memory:  40.0% (threshold: 85%) ✅
   Disk /:  82% (threshold: 80%) ⚠️
EOF
sleep 2

prompt "homebutler status --all"
cat <<'EOF'
📡 homelab      CPU   24% | Mem   40% | Disk   37% | Up 42d 7h
📡 nas          CPU    8% | Mem   40% | Disk   62% | Up 128d 3h
EOF
sleep 3
