# Changelog

All notable changes to this project will be documented in this file.

## [v0.8.1] - 2026-02-28

### Added
- Read-only Config tab in web dashboard (servers, alert thresholds, WoL devices)
- `GET /api/config` endpoint with masked passwords and key paths
- Tab navigation in dashboard header (Dashboard | Config)
- Demo mode returns realistic sample config data
- Dynamic version display in dashboard footer via `/api/version` endpoint
- Demo video embedded in README (YouTube link with thumbnail)

### Changed
- Dashboard header layout updated to accommodate tab navigation
- Config values displayed in monospace font for readability

### Security
- Passwords masked as `••••••` in config API response
- SSH key paths reduced to basename only (no full path exposure)

## [v0.8.0] - 2026-02-28

### Added
- `upgrade` command — update local + all remote servers to latest in one command
- `--local` flag for upgrade to skip remote servers
- npm package unified to `homebutler` (previously `homebutler-mcp`, now deprecated)
- npm install path added to README and Quick Start

### Changed
- `install.js` rewritten: native HTTPS (no curl dependency), graceful postinstall failure with lazy install fallback
- MCP config example updated to `npx -y homebutler@latest`

## [v0.7.1] - 2026-02-27

### Added
- `-v` and `--version` flag aliases for version command
- `processes` CLI subcommand for remote process listing
- golangci-lint CI with govet, staticcheck, unused, ineffassign, gofmt, goimports
- Cross-compile build matrix in CI (linux/darwin × amd64/arm64)
- Race detector enabled in test CI
- Pull request template
- Branch protection: Lint + Test required for PRs

### Changed
- Updated CONTRIBUTING.md with squash merge policy and PR workflow rules
- GitHub repo configured for squash merge only

### Fixed
- Lint issues: unused functions, ineffectual assignments, staticcheck suggestions

## [v0.7.0] - 2026-02-27

### Added
- Server dropdown now switches all dashboard cards to show selected server's data
- `?server=name` query parameter support on all API endpoints
- Per-server demo data variations (nas-box, raspberry-pi, etc.)
- Remote server data forwarded via SSH

## [v0.6.1] - 2026-02-26

### Fixed
- Empty `web_dist/` in release binaries — frontend now built in CI before Go build

## [v0.6.0] - 2026-02-26

### Added
- Web dashboard via `homebutler serve` command
- 7 dashboard cards: ServerOverview, SystemStatus, Docker, Processes, Alerts, Ports, WakeOnLAN
- `--demo` flag for realistic dummy data (10 servers, 6 containers)
- Dark theme (GitHub dark: #0d1117 bg, #161b22 cards, #58a6ff accent)
- API endpoints: `/api/status`, `/api/docker`, `/api/processes`, `/api/alerts`, `/api/ports`, `/api/wake`, `/api/servers`
- Docker-friendly error handling (returns `available: false` instead of 500)
- Friendly Docker status labels ("Up 4 days" → "Running · 4d")

## [v0.5.1] - 2026-02-25

### Changed
- Removed unused `output` config field

## [v0.5.0] - 2026-02-25

### Added
- TUI sparkline history graphs for CPU/memory/disk
- Top processes panel in TUI
- Redesigned TUI layout with unified panels

### Fixed
- TUI footer alignment
- Demo GIF updated (TUI first, then CLI)

## [v0.4.0] - 2026-02-24

### Added
- TUI terminal dashboard with Bubble Tea + Lip Gloss
- Real-time multi-server monitoring
- CPU instant measurement (macOS iostat, Linux /proc/stat)

## [v0.3.0] - 2026-02-23

### Added
- MCP server with 9 tools (JSON-RPC 2.0, stdio transport)
- Docker support in MCP

## [v0.2.1] - 2026-02-22

### Changed
- Default output format changed to human-readable (previously JSON)

## [v0.2.0] - 2026-02-22

### Added
- Multi-server SSH remote execution
- `homebutler deploy` command
- `--server` and `--all` flags

## [v0.1.0] - 2026-02-21

### Added
- Initial release
- System status (CPU, memory, disk, uptime)
- Docker container management (list, start, stop, restart, logs)
- Open port detection
- LAN device scanning
- Wake-on-LAN
- Resource threshold alerts
- JSON output support (`--json`)
- Interactive setup wizard (`homebutler init`)

[v0.8.1]: https://github.com/Higangssh/homebutler/compare/v0.8.0...v0.8.1
[v0.8.0]: https://github.com/Higangssh/homebutler/compare/v0.7.1...v0.8.0
[v0.7.1]: https://github.com/Higangssh/homebutler/compare/v0.7.0...v0.7.1
[v0.7.0]: https://github.com/Higangssh/homebutler/compare/v0.6.1...v0.7.0
[v0.6.1]: https://github.com/Higangssh/homebutler/compare/v0.6.0...v0.6.1
[v0.6.0]: https://github.com/Higangssh/homebutler/compare/v0.5.1...v0.6.0
[v0.5.1]: https://github.com/Higangssh/homebutler/compare/v0.5.0...v0.5.1
[v0.5.0]: https://github.com/Higangssh/homebutler/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/Higangssh/homebutler/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/Higangssh/homebutler/compare/v0.2.1...v0.3.0
[v0.2.1]: https://github.com/Higangssh/homebutler/compare/v0.2.0...v0.2.1
[v0.2.0]: https://github.com/Higangssh/homebutler/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/Higangssh/homebutler/releases/tag/v0.1.0
