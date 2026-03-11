# Configuration

## Interactive Setup

The easiest way to get started:

```bash
homebutler init
```

The setup wizard will:
- Auto-detect your local machine (hostname, IP)
- Walk you through adding remote servers (SSH user, port, auth)
- Test SSH connectivity for each server
- Show a summary before saving

If you already have a config, `homebutler init` lets you **add servers** to your existing config or start fresh.

## Config File Location

homebutler searches for a config file in the following order:

1. `--config <path>` — Explicit flag (highest priority)
2. `$HOMEBUTLER_CONFIG` — Environment variable
3. `~/.config/homebutler/config.yaml` — XDG standard location
4. `./homebutler.yaml` — Current directory

If no config file is found, sensible defaults are used (CPU 90%, memory 85%, disk 90%).

```bash
# Recommended: use XDG location
mkdir -p ~/.config/homebutler
cp homebutler.example.yaml ~/.config/homebutler/config.yaml

# Or use environment variable
export HOMEBUTLER_CONFIG=/path/to/config.yaml

# Or just put it in the current directory
cp homebutler.example.yaml homebutler.yaml
```

See [homebutler.example.yaml](../homebutler.example.yaml) for all options.

## Alert Thresholds

**Default thresholds** (no config needed):
- **CPU** — 90%
- **Memory** — 85%
- **Disk** — 90%

**Custom thresholds** via YAML config:

```yaml
alerts:
  cpu: 80
  memory: 70
  disk: 85
```

## Backup Directory

```yaml
backup:
  dir: /mnt/nas/backups/homebutler
```

Default: `~/.homebutler/backups/`

## Output Format

Default output is human-readable:

```
$ homebutler status
🖥  homelab-server (linux/arm64)
   Uptime:  42d 7h
   CPU:     23.5% (4 cores)
   Memory:  3.2 / 8.0 GB (40.0%)
   Disk /:  47 / 128 GB (37%)

$ homebutler status --all
📡 homelab      CPU   24% | Mem   40% | Disk   37% | Up 42d 7h
📡 nas          CPU    8% | Mem   40% | Disk   62% | Up 128d 3h
```

Use `--json` for machine-readable output (ideal for AI agents and scripts):

```bash
homebutler status --json
homebutler alerts --json
```

## Security

- **No network listener by default** — CLI and MCP modes never open ports. `homebutler serve` starts a local-only dashboard (127.0.0.1) on demand
- **Read-only by default** — Status commands don't modify anything
- **Explicit actions only** — Destructive commands require exact container/service names
- **SSH for remote** — Multi-server uses standard SSH (key-based auth recommended)
- **No telemetry** — Zero data collection, zero phone-home
