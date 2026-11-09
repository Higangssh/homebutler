# Multi-server Management

Manage multiple servers from a single machine. homebutler connects via SSH and runs the remote homebutler binary to collect data.

## Setup

### 1. Install homebutler on remote servers

```bash
# From a machine with internet access:
homebutler deploy --server rpi

# Air-gapped / offline environments:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o homebutler-linux-arm64
homebutler deploy --server rpi --local ./homebutler-linux-arm64
```

### 2. Configure servers

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

## SSH Authentication

Both key-based and password-based authentication are supported:

- **Key-based (recommended)** — Set `auth: key` (or omit, it's the default). If `key` is not specified, homebutler tries `~/.ssh/id_ed25519` then `~/.ssh/id_rsa` automatically.
- **Password-based** — Set `auth: password` and provide `password`. Not recommended for production.

To set up key-based auth:

```bash
ssh-keygen -t ed25519 -C "homebutler"
ssh-copy-id user@remote-host
```

## Usage

```bash
# Query a specific server
homebutler status --server rpi
homebutler alerts --server rpi
homebutler docker list --server rpi

# Query all servers in parallel
homebutler status --all
homebutler alerts --all

# Deploy homebutler to remote servers (first install)
homebutler deploy --server rpi
homebutler deploy --all

# Upgrade local + all remote servers to latest
homebutler upgrade

# Upgrade only the local binary
homebutler upgrade --local
```

## What `--all` runs

`--all` asks every server in `servers:` at once and prints one line each:

```
$ homebutler alerts --all
📡 mac-mini     OK       | CPU    6% | Mem   36% | Disk    6% /
📡 rpi5         OK       | CPU    0% | Mem    5% | Disk   18% /
```

It runs `status`, `alerts`, `doctor`, `report`, `processes`, `ports`,
`docker list`, `docker stats`, `backup list` and `network scan`. Each server is
asked for JSON, whether or not you passed `--json`, and the line is read from
that answer. A reading the answer did not carry is shown as `-`, not `0%`, and
a server that answered with something this version cannot read says so. With
`--json`, the output is a list of `{"server", "data"}` or `{"server", "error"}`,
one per server, with each server's own `--json` answer as `data`.

Everything else is refused with `--all`. `docker restart`, `docker stop`,
`backup`, `restore` and `wake` change the machine, and `docker logs`, `top` and
`inspect` read one container on one server; name the server with `--server`.
`report --all` saves a snapshot on every server, as `report` does on one; add
`--no-save` to compare without saving. `backup drill --all` is not the
multi-server flag: it drills every app in one archive.

## Upgrade

Upgrade checks GitHub Releases for the latest version, compares with each target, and updates only what's outdated:

```
$ homebutler upgrade
checking latest version... v0.8.0

upgrading local... ✓ v0.7.1 → v0.8.0
upgrading rpi5...  ✓ v0.7.1 → v0.8.0 (linux/arm64)
upgrading nas...   ─ already v0.8.0

2 upgraded, 1 already up-to-date
```
