# Changelog

All notable changes to this project will be documented in this file.

## [0.11.1](https://github.com/Higangssh/homebutler/compare/v0.11.0...v0.11.1) - 2026-04-01

**14 installable apps.** From monitoring to media streaming, DNS ad blocking to reverse proxies — all one command away.

```bash
homebutler install list              # see all 14 apps
homebutler install pi-hole           # DNS ad blocking
homebutler install jellyfin --media /movies  # media server
homebutler install portainer         # Docker GUI
```

### 🚀 Features

- add 8 new installable apps: homepage, stirling-pdf, speedtest-tracker, mealie, pi-hole, adguard-home, portainer, nginx-proxy-manager (total 14)
- add `--media` flag for jellyfin media directory mounting
- add safety checks: DNS port 53 conflict detection, mutual DNS app exclusion, port 80/443 check
- add Docker socket warning for portainer
- add post-install guidance: DNS setup, HTTPS access, default credential warnings
- auto-detect Docker socket path (Linux, colima, Docker Desktop) for portainer
- OS-specific DNS warnings (Linux: systemd-resolved, macOS: lsof)

### 🐛 Fixed

- install list `--json` now outputs proper JSON

### 📦 Other

- add `llms.txt` for AI search optimization
- update README with 14 apps table, options, and safety checks

## [0.11.0](https://github.com/Higangssh/homebutler/compare/v0.10.2...v0.11.0) - 2026-03-28

**Cobra CLI + docker stats.** The entire CLI is now powered by cobra — auto-generated help, shell completion, and cleaner flag handling. Plus a new `docker stats` command for real-time container resource monitoring.

```bash
homebutler docker stats          # per-container CPU, memory, network, I/O
homebutler completion zsh        # shell auto-completion
homebutler docker --help         # auto-generated sub-command help
```

### 🚀 Features

- add `docker stats` command for per-container resource usage (CPU, memory, network I/O, block I/O, PIDs)
- add `docker_stats` MCP tool (15th tool) with remote server support
- add `/api/docker/stats` web dashboard API endpoint
- add shell completion support for bash, zsh, and fish
- auto-generated help for all commands and sub-commands

### ♻️ Refactored

- migrate entire CLI from manual switch/case to cobra framework
- split monolithic root.go into per-command files (18 files)
- extract shared CLI helpers to cmd/helpers.go

### 🐛 Fixed

- wrap remote docker response to match local format (#21)

### 🧪 Tests

- boost test coverage: server 49→81%, ports 8→75%, docker 47→64%, remote 7→22%
- add docker stats parsing tests (7 cases)
- add docker stats API tests (7 cases)

### 📦 Other

- add Dockerfile for Glama MCP server inspection
- add glama.json for Glama author verification
- add Glama score badge to README

## [0.10.2](https://github.com/Higangssh/homebutler/compare/v0.10.1...v0.10.2) - 2026-03-21

**5 apps now installable with one command.** filebrowser, it-tools, and gitea join the registry.

```bash
homebutler install list          # see all 5 apps
homebutler install it-tools      # developer utilities in seconds
homebutler install gitea         # your own Git server
```

### 🚀 Features

- add filebrowser to install registry (web-based file manager)
- add it-tools to install registry (developer utility collection)
- add gitea to install registry (self-hosted Git service with SSH)
- show process/container name in port conflict messages
- check Docker container ports for colima/podman environments

## [0.10.1](https://github.com/Higangssh/homebutler/compare/v0.10.0...v0.10.1) - 2026-03-20

### 🧪 Tests

- add comprehensive install tests (registry, CRUD, template rendering, port check)
- add docker utility tests (socket detection, itoa)

## [0.10.0](https://github.com/Higangssh/homebutler/compare/v0.9.0...v0.10.0) - 2026-03-20

**One-command app deployment for your homelab.** Install, manage, and remove self-hosted apps with docker compose — no manual setup needed.

```bash
homebutler install uptime-kuma          # deploy in seconds
homebutler install vaultwarden --port 9090  # custom port
homebutler install status uptime-kuma   # check health
homebutler install uninstall uptime-kuma    # stop, keep data
homebutler install purge uptime-kuma    # remove everything
```

Each app gets its own `docker-compose.yml` at `~/.homebutler/apps/<app>/` with persistent data, pre-flight checks (docker, ports, duplicates), and cross-platform support (Linux, macOS, colima, podman).

### 🚀 Features

- add `install` command — deploy self-hosted apps with docker compose
- add `install list` — list available apps
- add `install status` — check installed app status
- add `install uninstall` — stop app, keep data
- add `install purge` — stop app, delete all data
- support `--port` flag for custom host port
- app registry: uptime-kuma, vaultwarden
- cross-platform docker socket detection (default, colima, podman)
- install registry (`installed.json`) to track app locations
- PUID/PGID support for compatible apps

### 🔒 Security

- harden SSH remote execution against shell injection (ShellQuote)
- add checksum verification for upgrade downloads


## [0.9.0](https://github.com/Higangssh/homebutler/compare/v0.8.2...v0.9.0) - 2026-03-11

### 🚀 Features

- add `backup` command — one-command Docker volume backup with compose files and env
- add `backup list` — list existing backups with size and timestamp
- add `restore` command — restore volumes from backup archive
- support `--service` flag for single-service backup/restore
- support `--to` flag for custom backup destination
- configurable `backup_dir` in homebutler.yml

### 🔒 Security

- warn when config file containing passwords has open permissions (recommend chmod 600)
- fix goroutine leak in network scan — context cancellation now stops ping sweep
- `ScanWithTimeout` properly cancels goroutines on timeout (no leak)

### 📖 Documentation

- split README into focused docs: `docs/backup.md`, `docs/configuration.md`, `docs/multi-server.md`, `docs/mcp-server.md`, `docs/web-dashboard.md`
- README slimmed from 719 to 386 lines with links to detailed docs
- add detailed backup documentation with how-it-works guide and security notes

### 🐛 Bug Fixes

- fix ineffective `break` in pingSweep `select` statement (staticcheck SA4011)
- handle empty config path gracefully (no panic on `Load("")`)
- log warning on backup temp directory cleanup failure

### 🧹 Chores

- rename `skill/` to `skills/` (convention)
- remove stale media files from git, update .gitignore
- add OpenClaw agent skill to repo

## [0.8.2](https://github.com/Higangssh/homebutler/compare/v0.8.1...v0.8.2) - 2026-03-02

### 🚀 Features

- add `alerts --watch` continuous monitoring mode
- configurable interval (`--interval`) and custom thresholds (`--config`)
- event deduplication (same alert won't repeat until recovered)
- aligned output formatting with fixed-width columns

## [0.8.1](https://github.com/Higangssh/homebutler/compare/v0.8.0...v0.8.1) - 2026-02-28

### ♻️ Refactor

- split cmd/root.go into deploy, upgrade, multiserver

### 🐛 Bug Fixes

- restore skills directory in git, only ignore skill symlink

### 🚀 Features

- add read-only config tab to web dashboard
- dynamic version in web dashboard + demo video
- implement graceful shutdown for http server (#12)
## [0.8.0](https://github.com/Higangssh/homebutler/compare/v0.7.1...v0.8.0) - 2026-02-27

### 🐛 Bug Fixes

- npm wrapper uses GitHub latest release, lazy install on first run

### 🔒 Security

- harden web server defaults

### 🚀 Features

- add upgrade command for self + remote server updates
- unify npm package name to homebutler
- add npm wrapper for zero-install MCP setup (npx homebutler-mcp)
- add MCP demo mode and Claude Code screenshots to README
- add Agent Skills support for Claude Code, Cursor, and more
## [0.7.1](https://github.com/Higangssh/homebutler/compare/v0.6.1...v0.7.1) - 2026-02-26

### 🐛 Bug Fixes

- use latest golangci-lint for Go 1.25+ compat
- use golangci-lint-action v7 for lint v2 support

### 🚀 Features

- add -v and --version aliases to version command
- wire server dropdown to switch all dashboard cards
## [0.6.1](https://github.com/Higangssh/homebutler/compare/v0.6.0...v0.6.1) - 2026-02-26

### 🐛 Bug Fixes

- remove goreleaser before hook (web built in CI step)
- build web frontend in CI before go build
## [0.6.0](https://github.com/Higangssh/homebutler/compare/v0.5.1...v0.6.0) - 2026-02-26

### 🐛 Bug Fixes

- update demo server count in test
- expand remote PATH for homebrew, snap, and go install
- hide empty wake array in generated config

### 🚀 Features

- add web dashboard with serve command
- add Dockerfile for MCP server (Glama registry)
## [0.5.1](https://github.com/Higangssh/homebutler/compare/v0.5.0...v0.5.1) - 2026-02-26

### ♻️ Refactor

- remove unused output config field

### 🐛 Bug Fixes

- improve SSH error messages with clear diagnostics and actions
- show 0% immediately on TUI start instead of waiting for data

### 🚀 Features

- redesign interactive init wizard
- add 'homebutler init' interactive setup wizard
- add project logo with rounded corners and update README header
- TOFU for SSH — auto-register unknown hosts, reject only on key change
- SSH known_hosts verification and instant CPU measurement
## [0.5.0](https://github.com/Higangssh/homebutler/compare/v0.4.0...v0.5.0) - 2026-02-26

### 🐛 Bug Fixes

- reorder demo GIF — TUI first, clear, then CLI commands
- reorder demo GIF (CLI first, TUI last) and reduce height
- widen demo GIF to prevent status output wrapping
- improve TUI layout and sparkline alignment

### 🚀 Features

- redesign TUI layout with History section and unified panels
- add sparkline history graphs and top processes panel
## [0.4.0](https://github.com/Higangssh/homebutler/compare/v0.3.0...v0.4.0) - 2026-02-25

### ♻️ Refactor

- simplify watch command, remove unused --all/--server flags

### 🐛 Bug Fixes

- reorder demo GIF to show TUI first, then CLI commands
- prevent goroutine leak in docker fetch
- preserve docker state when system data refreshes
- fetch docker data asynchronously in TUI
- improve tab bar label for clarity
- set DockerStatus for remote servers in TUI
- resolve TUI dashboard data loading issues

### 🚀 Features

- unified demo GIF with CLI commands + TUI dashboard
- add TUI demo GIF with dummy data renderer
- improve tab bar with numbered labels and server count
- improve footer keybinding hints in TUI
- show server name in system panel title
- show Docker status in TUI dashboard
- add TUI dashboard with 'watch' command
## [0.3.0](https://github.com/Higangssh/homebutler/compare/v0.2.1...v0.3.0) - 2026-02-24

### 🚀 Features

- add MCP server for AI tool integration
## [0.2.1](https://github.com/Higangssh/homebutler/compare/v0.2.0...v0.2.1) - 2026-02-24

### 🐛 Bug Fixes

- resolve go vet self-assignment in test
- validate docker logs line count and remove unused files

### 🚀 Features

- human-readable default output and GitHub Actions CI/CD
- add install script and improve PATH handling for deploy
## [0.2.0](https://github.com/Higangssh/homebutler/compare/v0.1.0...v0.2.0) - 2026-02-23

### 🚀 Features

- add deploy command for remote binary installation
- add multi-server support via SSH
- add config file auto-discovery with XDG support
## [0.1.0](https://github.com/Higangssh/homebutler/compare/...v0.1.0) - 2026-02-23

### 🐛 Bug Fixes

- filter incomplete ARP entries on Linux and return empty array for docker list

### 🚀 Features

- add OpenClaw skill wrapper for AI integration
- add demo GIF with sample data
- add build tooling, improve docker errors, and enhance README
- add alerts, config file loading, and WOL name support
- add network scan and filter multicast addresses
- initial project setup with core commands
