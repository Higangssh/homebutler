# Changelog

All notable changes to this project will be documented in this file.

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

