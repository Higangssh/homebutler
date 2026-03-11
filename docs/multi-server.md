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
