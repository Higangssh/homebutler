[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/Higangssh/homebutler)](https://goreportcard.com/report/github.com/Higangssh/homebutler)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/Higangssh/homebutler)](https://github.com/Higangssh/homebutler/releases)

# homebutler 🏠

**Manage your homelab from any AI — Claude, ChatGPT, Cursor, or terminal. One binary. Zero dependencies.**

A single-binary CLI + MCP server that lets you monitor servers, control Docker, wake machines, and scan your network — from chat, AI tools, or the command line.

## Features

- **TUI Dashboard** — Real-time monitoring with `homebutler watch` (btop-style)
- **System Status** — CPU, memory, disk, uptime at a glance
- **Docker Management** — List, restart, stop, logs for containers
- **Wake-on-LAN** — Power on machines remotely
- **Port Scanner** — See what's listening and which process owns it
- **Network Scan** — Discover devices on your LAN
- **Alerts** — Get notified when resources exceed thresholds
- **Multi-server** — Manage remote servers over SSH (key & password auth)
- **MCP Server** — Works with Claude Desktop, ChatGPT, Cursor, and any MCP client
- **JSON Output** — Pipe-friendly, perfect for AI assistants to parse

## Why homebutler?

> Other tools give you dashboards. homebutler gives you a **conversation**.

<details>
<summary>📊 Comparison with alternatives</summary>

| | homebutler | Glances/btop | Netdata | CasaOS |
|---|---|---|---|---|
| TUI dashboard | ✅ Built-in | ✅ | ❌ Web | ❌ Web |
| Single binary | ✅ | ❌ | ❌ | ❌ |
| No web server | ✅ | ❌ | ❌ | ❌ |
| Multi-server SSH | ✅ Parallel | ❌ | ❌ | ❌ |
| MCP support | ✅ Built-in | ❌ | ❌ | ❌ |
| Chat integration | ✅ Native | ❌ | ❌ | ❌ |
| AI-friendly JSON | ✅ | ❌ | ⚠️ API | ⚠️ API |
| Docker control | ✅ | ⚠️ Monitor | ❌ | ✅ |
| Wake-on-LAN | ✅ | ❌ | ❌ | ❌ |
| Network scan | ✅ | ❌ | ❌ | ❌ |
| Remote deploy | ✅ One command | ❌ | ❌ | ❌ |
| Air-gapped install | ✅ Copy binary | ⚠️ apt/brew | ❌ Docker | ❌ Docker |
| Resource usage | ~10MB, 0% idle | Medium | High | High |

</details>

## Demo

<p align="center">
  <img src="demo-tui.gif" alt="homebutler TUI dashboard" width="800">
</p>

## Quick Start

```bash
# One-line install (recommended, auto-detects OS/arch)
curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh

# Or via Homebrew
brew install Higangssh/homebutler/homebutler

# Run
homebutler status
homebutler watch             # TUI dashboard (all servers)
homebutler docker list
homebutler wake desktop
homebutler ports
homebutler status --all
```

## Usage

```
homebutler <command> [flags]

Commands:
  status              System status (CPU, memory, disk, uptime)
  watch               TUI dashboard (monitors all configured servers)
  docker list         List running containers
  docker restart <n>  Restart a container
  docker stop <n>     Stop a container
  docker logs <n>     Show container logs
  wake <name>         Send Wake-on-LAN packet
  ports               List open ports with process info
  network scan        Discover devices on LAN
  alerts              Show current alert status
  deploy              Install homebutler on remote servers
  mcp                 Start MCP server (JSON-RPC over stdio)
  version             Print version

Flags:
  --json              JSON output (default: human-readable)
  --server <name>     Run on a specific remote server
  --all               Run on all configured servers in parallel
  --local <path>      Use local binary for deploy (air-gapped)
  --config <path>     Config file (auto-detected, see Configuration)
```

## TUI Dashboard

`homebutler watch` launches an interactive terminal dashboard:

```bash
homebutler watch               # monitors all configured servers
```

**Layout:**
- **Top** — Server tabs (Tab / Shift+Tab to switch)
- **Left panel** — CPU, memory, disk usage bars (color-coded: green → yellow → red)
- **Right panel** — Docker containers with state and image info
- **Bottom** — Alert status + keybinding hints

Auto-refreshes every 2 seconds. Press `q` to quit.

## Configuration

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

See [homebutler.example.yaml](homebutler.example.yaml) for all options.

## Multi-server

Manage multiple servers from a single machine. homebutler connects via SSH and runs the remote homebutler binary to collect data.

### Setup

1. Install homebutler on remote servers:

```bash
# From a machine with internet access:
homebutler deploy --server rpi

# Air-gapped / offline environments:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o homebutler-linux-arm64
homebutler deploy --server rpi --local ./homebutler-linux-arm64
```

2. Configure servers in your config file:

```yaml
servers:
  - name: main-server
    host: 192.168.1.10
    local: true              # This machine

  - name: rpi
    host: 192.168.1.20
    user: pi
    auth: key                # Recommended (default)
    key: ~/.ssh/id_ed25519   # Optional, auto-detects id_ed25519 / id_rsa

  - name: vps
    host: my-vps.example.com
    user: deploy
    port: 2222
    auth: password           # Also supported
    password: "your-password"
```

### SSH Authentication

Both key-based and password-based authentication are supported:

- **Key-based (recommended)** — Set `auth: key` (or omit, it's the default). If `key` is not specified, homebutler tries `~/.ssh/id_ed25519` then `~/.ssh/id_rsa` automatically.
- **Password-based** — Set `auth: password` and provide `password`. Not recommended for production.

To set up key-based auth:

```bash
ssh-keygen -t ed25519 -C "homebutler"
ssh-copy-id user@remote-host
```

### Usage

```bash
# Query a specific server
homebutler status --server rpi
homebutler alerts --server rpi
homebutler docker list --server rpi

# Query all servers in parallel
homebutler status --all
homebutler alerts --all

# Deploy/update homebutler on remote servers
homebutler deploy --server rpi
homebutler deploy --all
```

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

- **No network listener** — homebutler never opens ports or accepts connections
- **Read-only by default** — Status commands don't modify anything
- **Explicit actions only** — Destructive commands require exact container/service names
- **SSH for remote** — Multi-server uses standard SSH (key-based auth recommended)
- **No telemetry** — Zero data collection, zero phone-home

## MCP Server

homebutler includes a built-in [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server, so any AI tool can manage your homelab.

### Supported Clients

- **Claude Desktop** — Anthropic's desktop app
- **ChatGPT Desktop** — OpenAI's desktop app
- **Cursor** — AI code editor
- **Windsurf** — AI code editor
- **Any MCP-compatible client**

### Setup

Add to your MCP client config:

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "homebutler": {
      "command": "homebutler",
      "args": ["mcp"]
    }
  }
}
```

**Cursor** (Settings → MCP → Add):
```json
{
  "command": "homebutler",
  "args": ["mcp"]
}
```

Restart your AI client — homebutler tools will appear automatically.

### Available MCP Tools

| Tool | Description |
|---|---|
| `system_status` | CPU, memory, disk, uptime |
| `docker_list` | List containers |
| `docker_restart` | Restart a container |
| `docker_stop` | Stop a container |
| `docker_logs` | Container log output |
| `wake` | Wake-on-LAN magic packet |
| `open_ports` | Open ports with process info |
| `network_scan` | Discover LAN devices |
| `alerts` | Resource threshold alerts |

All tools support an optional `server` parameter for multi-server management.

### How It Works

```
You: "How's my server doing?"
AI → calls system_status tool
homebutler → reads CPU/RAM/disk
AI: "CPU 12%, memory 48%, disk 3%. Everything looks healthy!"
```

No network ports opened. MCP uses stdio (stdin/stdout) — only the parent AI process can communicate with homebutler.

### OpenClaw Skill

An official [OpenClaw](https://github.com/openclaw/openclaw) skill is available on [ClawHub](https://clawhub.ai/skills/homeserver):

```bash
clawhub install homeserver
```

This lets you manage your homelab from Telegram, Discord, or any chat platform connected to OpenClaw — in any language.

## Installation

### Homebrew (Recommended)

```bash
brew install Higangssh/homebutler/homebutler
```

Automatically installs to PATH. Works on macOS and Linux.

### One-line Install

```bash
curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh
```

Auto-detects OS/architecture, downloads the latest release, and installs to PATH.

### Go Install

```bash
go install github.com/Higangssh/homebutler@latest
```

### Build from Source

```bash
git clone https://github.com/Higangssh/homebutler.git
cd homebutler
make build
```

## Uninstall

```bash
rm $(which homebutler)           # Remove binary
rm -rf ~/.config/homebutler      # Remove config (optional)
```

## Contributing

Contributions welcome! Please open an issue first to discuss what you'd like to change.

## License

[MIT](LICENSE)
