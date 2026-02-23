---
name: homeserver
description: Homelab server management via homebutler CLI. Check system status (CPU/RAM/disk), manage Docker containers, Wake-on-LAN, scan open ports, discover network devices, and monitor resource alerts. Use when asked about server status, docker containers, wake machines, open ports, network devices, or system alerts.
---

# Homeserver Management

Manage homelab servers using the `homebutler` CLI. Single binary, JSON output, AI-friendly.

## Prerequisites

`homebutler` must be installed and available in PATH.

```bash
# Check if installed
which homebutler

# If not, build from source
cd workspace/projects/homebutler && make build && sudo cp homebutler /usr/local/bin/
```

## Commands

### System Status
```bash
homebutler status
```
Returns: hostname, OS, arch, uptime, CPU (usage%, cores), memory (total/used/%), disks (mount/total/used/%)

### Docker Management
```bash
homebutler docker list          # List all containers
homebutler docker restart <name> # Restart a container
homebutler docker stop <name>    # Stop a container
homebutler docker logs <name>    # Last 50 lines of logs
homebutler docker logs <name> 200 # Last 200 lines
```

### Wake-on-LAN
```bash
homebutler wake <mac-address>           # Wake by MAC
homebutler wake <name>                   # Wake by config name
homebutler wake <mac> 192.168.1.255     # Custom broadcast
```
Config names are defined in `homebutler.yaml` under `wake.targets`.

### Open Ports
```bash
homebutler ports
```
Returns: protocol, address, port, PID, process name

### Network Scan
```bash
homebutler network scan
```
Discovers devices on the local LAN via ping sweep + ARP table. Returns: IP, MAC, hostname, status.
Note: May take up to 30 seconds. Some devices may not appear if they don't respond to ping.

### Resource Alerts
```bash
homebutler alerts
```
Checks CPU/memory/disk against thresholds in config. Returns status (ok/warning/critical) per resource.

### Version
```bash
homebutler version
```

## Output Format

All commands output JSON by default. Use `--json` flag to force JSON even if future versions add other formats.

## Config File

`homebutler.yaml` (or specify with `--config <path>`):
- `wake.targets` — named WOL targets with MAC + broadcast
- `alerts.cpu/memory/disk` — threshold percentages
- `output.format` — default output format

## Multi-Server (Future)

When `--server <name>` is supported, homebutler will SSH to remote servers and run commands there. Server configs will be in `homebutler.yaml` under `servers`.

## Usage Guidelines

1. **Always run commands, don't guess** — execute `homebutler status` to get real data
2. **Interpret results for the user** — don't dump raw JSON, summarize in natural language
3. **Warn on alerts** — if any resource shows "warning" or "critical", highlight it
4. **Docker errors** — if docker is not installed or daemon not running, explain clearly
5. **Network scan** — warn user it may take ~30 seconds
6. **Security** — never expose raw JSON with hostnames/IPs in group chats, summarize instead

## Example Interactions

User: "How's the server doing?"
→ Run `homebutler status`, summarize: "CPU 23%, memory 40%, disk 37%. Uptime 42 days. All good 👍"

User: "What docker containers are running?"
→ Run `homebutler docker list`, list container names and states

User: "Wake up the NAS"
→ Run `homebutler wake nas` (if configured) or ask for MAC address

User: "What ports are open?"
→ Run `homebutler ports`, summarize which services are listening
