---
name: homeserver
description: Homelab server management via homebutler CLI. Check system status (CPU/RAM/disk), manage Docker containers, Wake-on-LAN, scan open ports, discover network devices, monitor resource alerts, and manage multiple servers over SSH. Use when asked about server status, docker containers, wake machines, open ports, network devices, system alerts, or multi-server management.
metadata:
  {
    "openclaw": {
      "emoji": "🏠",
      "requires": { "anyBins": ["homebutler"] },
      "configPaths": ["homebutler.yaml", "~/.config/homebutler/config.yaml"]
    }
  }
---

# Homeserver Management

Manage homelab servers using the `homebutler` CLI. Single binary, JSON output, AI-friendly.

## Prerequisites

`homebutler` must be installed and available in PATH.

```bash
# Check if installed
which homebutler

# Option 1: Install via Homebrew (macOS/Linux)
brew install Higangssh/homebutler/homebutler

# Option 2: Install via Go
go install github.com/Higangssh/homebutler@latest

# Option 3: Build from source
git clone https://github.com/Higangssh/homebutler.git
cd homebutler && make build && sudo mv homebutler /usr/local/bin/

# Option 4: Shell installer (review script before running)
curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh -o install.sh
less install.sh  # review first
sh install.sh
```

## Commands

### System Status
```bash
homebutler status                    # Local server
homebutler status --server rpi       # Specific remote server
homebutler status --all              # All servers in parallel
```
Returns: hostname, OS, arch, uptime, CPU (usage%, cores), memory (total/used/%), disks (mount/total/used/%)

### Docker Management
```bash
homebutler docker list               # List all containers
homebutler docker list --server rpi  # List on remote server
homebutler docker list --all         # List on all servers
homebutler docker restart <name>     # Restart a container
homebutler docker stop <name>        # Stop a container
homebutler docker logs <name>        # Last 50 lines of logs
homebutler docker logs <name> 200    # Last 200 lines
```

### Wake-on-LAN
```bash
homebutler wake <mac-address>           # Wake by MAC
homebutler wake <name>                   # Wake by config name
homebutler wake <mac> 192.168.1.255     # Custom broadcast
```
Config names are defined in config under `wake` targets.

### Open Ports
```bash
homebutler ports                     # Local
homebutler ports --server rpi        # Remote
homebutler ports --all               # All servers
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
homebutler alerts                    # Local
homebutler alerts --server rpi       # Remote
homebutler alerts --all              # All servers
```
Checks CPU/memory/disk against thresholds in config. Returns status (ok/warning/critical) per resource.

### Deploy (Remote Installation)
```bash
homebutler deploy --server rpi                          # Download from GitHub Releases
homebutler deploy --server rpi --local ./homebutler     # Air-gapped: copy local binary
homebutler deploy --all                                 # Deploy to all remote servers
```
Installs homebutler on remote servers via SSH. Auto-detects remote OS/architecture.
Install path priority: `/usr/local/bin` → `sudo /usr/local/bin` → `~/.local/bin` (with PATH auto-registration in .profile/.bashrc/.zshrc).

### Version
```bash
homebutler version
```

## Output Format

All commands output human-readable text by default. Use `--json` flag for machine-parseable JSON output (recommended for AI/script integration).

## Config File

Config file is auto-discovered in order:
1. `--config <path>` — Explicit flag
2. `$HOMEBUTLER_CONFIG` — Environment variable
3. `~/.config/homebutler/config.yaml` — XDG standard (recommended)
4. `./homebutler.yaml` — Current directory

If no config found, sensible defaults are used.

### Config Options
- `servers` — Server list with SSH connection details
- `wake` — Named WOL targets with MAC + broadcast
- `alerts.cpu/memory/disk` — Threshold percentages
- `output` — Default output format

### Multi-Server Config Example
```yaml
servers:
  - name: main-server
    host: 192.168.1.10
    local: true

  - name: rpi
    host: 192.168.1.20
    user: pi
    auth: key                # "key" (default, recommended) or "password"
    key: ~/.ssh/id_ed25519   # optional, auto-detects

  - name: vps
    host: example.com
    user: deploy
    port: 2222
    auth: key
    key: ~/.ssh/id_ed25519
```

## Usage Guidelines

1. **Always run commands, don't guess** — execute `homebutler status` to get real data
2. **Interpret results for the user** — don't dump raw JSON, summarize in natural language
3. **Warn on alerts** — if any resource shows "warning" or "critical", highlight it
4. **Use --all for overview** — when user asks about "all servers" or "everything", use `--all`
5. **Use --server for specific** — when user mentions a server by name, use `--server <name>`
6. **Docker errors** — if docker is not installed or daemon not running, explain clearly
7. **Network scan** — warn user it may take ~30 seconds
8. **Security** — never expose raw JSON with hostnames/IPs in group chats, summarize instead
9. **Deploy** — suggest `--local` for air-gapped environments

## Security Notes

- **SSH authentication**: Always prefer key-based auth over passwords. Never store plaintext passwords in config.
- **Network scans**: Only run on your own local network. Warn user before scanning.
- **Deploy**: Only deploy to servers you own. Confirm with user before remote installations.
- **Config file permissions**: Keep config files readable only by owner (`chmod 600`).
- **No telemetry**: homebutler sends zero data externally. All operations are local or to user-configured hosts only.

## Error Handling

- **SSH connection failed** → Check host/port/user in config, verify SSH key is registered on remote
- **homebutler not found on remote** → Run `homebutler deploy --server <name>` first
- **docker not installed** → Tell user docker is not available on that server
- **docker daemon not running** → Suggest `sudo systemctl start docker`
- **network scan timeout** → Normal on large subnets, suggest retrying
- **permission denied** → May need sudo for ports/docker commands on some systems

## Example Interactions

User: "How's the server doing?"
→ Run `homebutler status`, summarize: "CPU 23%, memory 40%, disk 37%. Uptime 42 days. All good 👍"

User: "Check all servers"
→ Run `homebutler status --all`, summarize each server's status

User: "How's the Raspberry Pi?"
→ Run `homebutler status --server rpi`, summarize

User: "What docker containers are running?"
→ Run `homebutler docker list`, list container names and states

User: "Wake up the NAS"
→ Run `homebutler wake nas` (if configured) or ask for MAC address

User: "Any alerts across all servers?"
→ Run `homebutler alerts --all`, report any warnings/critical

User: "Deploy homebutler to the new server"
→ Run `homebutler deploy --server <name>`, report result
