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

## Validating Your Config

```bash
homebutler config validate
homebutler config validate --config ./homebutler.yaml
homebutler config validate --strict   # exit non-zero on warnings too
homebutler config validate --json
```

`config validate` is read-only: it starts no server, watcher, or install, and
never connects to a remote host. It reports three things.

**Which file was used, and which rule selected it.** With four resolution rules,
editing the wrong file is an easy mistake to make.

**What homebutler read from each section.** A section the file never set is
listed as `not set`, which is usually the answer to "why is my notify config
being ignored?".

**What is wrong or silently ignored.** Errors mean something will not work;
warnings mean the config parses but probably does not do what you intended:

```text
Findings
   ⚠️ Line 5: field notifiy not found in the homebutler config
      → Did you mean "notify"? Unrecognised keys are ignored silently, so this
        line currently has no effect.
   ❌ servers[0].host: Host is required for remote servers.
      → Set host, or set local: true if this entry is the machine homebutler runs on.
```

Two cases are worth calling out because nothing else surfaces them:

- **A key homebutler does not recognise is dropped without a word.** A typo
  such as `notifiy:` leaves notifications switched off with no error anywhere.
- **A `--config` path that does not exist falls back to built-in defaults**
  rather than failing, so commands run and succeed against a config that was
  never read. `config validate` reports this as an error.

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
backup_dir: /mnt/nas/backups/homebutler
```

Default: `~/.homebutler/backups/`

Run `homebutler config validate` after changing this — a mistyped key here is
dropped silently, and backups keep going to the default location.

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
