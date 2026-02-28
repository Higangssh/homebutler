# Contributing to HomeButler

Thanks for your interest in contributing! HomeButler is a single-binary homelab management tool, and we welcome contributions of all kinds.

## Getting Started

### Prerequisites

- Go 1.25+
- Git

### Setup

```bash
git clone https://github.com/Higangssh/homebutler.git
cd homebutler
go build -o homebutler .
./homebutler version
```

### Run Tests

```bash
go test ./...
go test -race ./...
go vet ./...
```

## How to Contribute

### Bug Reports

Open an issue with:
- What you expected
- What actually happened
- Steps to reproduce
- OS/architecture (`homebutler version`)

### Feature Requests

Open an issue describing:
- The problem you're trying to solve
- Your proposed solution
- Any alternatives you considered

### Pull Requests

1. **Comment on the issue first** — Let others know you're working on it to avoid duplicate PRs
2. Fork the repo
3. Create a branch (`git checkout -b feat/my-feature`)
4. Make your changes
5. Run `go fmt ./...` and `go vet ./...`
6. Run tests (`go test ./...`)
7. Commit with [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `refactor:`, etc.)
8. Push and open a PR — **1 PR per issue**

> **Note:** All PRs are squash-merged into a single commit on main.

### Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/). Commits are automatically parsed to generate the [CHANGELOG](CHANGELOG.md) via `git-cliff`.

**Format:** `<type>: <description>`

| Type | Purpose | Appears in CHANGELOG |
|------|---------|---------------------|
| `feat` | New feature or capability | 🚀 Features |
| `fix` | Bug fix | 🐛 Bug Fixes |
| `security` | Security fix or hardening | 🔒 Security |
| `refactor` | Code restructuring (no behavior change) | ♻️ Refactor |
| `perf` | Performance improvement | ⚡ Performance |
| `docs` | Documentation only | Hidden |
| `test` | Adding or fixing tests | Hidden |
| `chore` | Build, CI, dependencies, tooling | Hidden |
| `style` | Formatting, whitespace (no logic change) | Hidden |
| `ci` | CI/CD workflow changes | Hidden |

**Rules:**
- Lowercase type, no capitalized description: `feat: add config tab` not `Feat: Add Config Tab`
- No period at the end
- Keep the first line under 72 characters
- Use imperative mood: "add" not "added", "fix" not "fixed"
- Scope is optional: `feat(web): add config tab` is fine but not required
- Breaking changes: add `!` after type: `feat!: change config format`

**Examples:**
```
feat: add network latency monitoring
fix: correct CPU calculation on macOS
security: bind web server to localhost by default
refactor: split cmd/root.go into domain files
perf: parallelize multi-server SSH connections
docs: update MCP setup instructions
test: add coverage for alert thresholds
chore: update CI workflow
```

> **Why it matters:** `feat` and `fix` commits become release notes. `docs`, `test`, `chore` are hidden from the CHANGELOG. Choose your type carefully — it determines what users see.

## Project Structure

```
homebutler/
├── main.go                 # Entry point
├── cmd/
│   ├── root.go             # CLI routing
│   └── init.go             # Interactive setup wizard
├── internal/
│   ├── system/             # CPU, memory, disk, processes
│   ├── docker/             # Container management
│   ├── remote/             # SSH multi-server
│   ├── tui/                # Terminal dashboard (Bubble Tea)
│   ├── mcp/                # MCP server (JSON-RPC)
│   ├── config/             # Config loading
│   ├── alerts/             # Resource threshold alerts
│   ├── network/            # LAN device scanning
│   ├── ports/              # Open port detection
│   ├── wake/               # Wake-on-LAN
│   ├── format/             # Human-readable output
│   └── util/               # Shared utilities
├── demo/                   # Demo GIF assets
├── skill/                  # OpenClaw skill definition
└── docs/                   # Internal specs
```

## Guidelines

- **Keep it simple** — HomeButler is a single binary with zero dependencies. Avoid adding external libraries unless absolutely necessary.
- **Cross-platform** — All features should work on macOS and Linux (arm64 + amd64).
- **Test what matters** — Write tests for logic, not boilerplate. Table-driven tests preferred.
- **JSON output** — All commands should support `--json` for machine-readable output.

## Need Help?

- Open an issue with the `question` label
- Check existing issues for similar questions

Thank you for helping make HomeButler better!
