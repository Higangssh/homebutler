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

We use Conventional Commits:

```
feat: add network latency monitoring
fix: correct CPU calculation on macOS
refactor: simplify SSH connection logic
docs: update MCP setup instructions
chore: update CI workflow
```

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
