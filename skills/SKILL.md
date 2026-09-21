---
name: homebutler
description: Tells an agent what changed on a server since it last looked - plus status, Docker, backups, Proxmox. 44 MCP tools, each classed read, write or destructive.
metadata:
  {
    "openclaw": {
      "emoji": "🏠",
      "requires": { "anyBins": ["homebutler"] },
      "configPaths": ["homebutler.yaml", "~/.config/homebutler/config.yaml"]
    }
  }
---

# Homebutler

[homebutler](https://github.com/Higangssh/homebutler) remembers what a server
looked like last time and reports only the changes worth mentioning. One Go
binary: no database, and no agent on the machines it watches — the binary is
deployed there once and runs only when asked, over SSH.

> This file is published to ClawHub as `@higangssh/homebutler`. The copy that
> matters lives in the repository at `skills/SKILL.md`, and a test in `cmd/`
> fails the build when a command or a tool named here stops existing.

## Use the MCP server, not the shell

Start `homebutler mcp` and call tools. Every tool is classed **read**, **write**
or **destructive**, and that classification is what lets an agent decide what it
may do unattended. Shell commands are for the handful of things no tool exposes,
and for anything the operator has to run themselves.

```bash
homebutler mcp
```

### The tools

**Read (27)** — nothing changes; see what a read still exposes, below.

- `system_status`, `processes`, `open_ports`, `alerts`, `alerts_history`
- `doctor` — health, exposure, backup age and readiness, as findings
- `inventory_scan`, `inventory_export`, `network_scan`, `config_validate`
- `docker_list`, `docker_logs`, `docker_stats`, `docker_top`, `docker_inspect`
- `backup_list`, `install_list`, `install_status`
- `watch_list`, `watch_history`
- `proxmox_status`, `proxmox_guests`, `proxmox_node`, `proxmox_tasks`,
  `proxmox_task_status`, `proxmox_script_list`, `proxmox_script_command`

**Write (13)** — something changes, or something leaves the machine.

- `report` — the comparison, and it saves a snapshot
- `docker_restart`, `wake`, `notify_test`
- `backup_create`, `backup_drill`
- `install_app`, `install_uninstall`
- `watch_add`, `watch_check`, `watch_remove`
- `proxmox_guest_start`, `proxmox_guest_reboot`

**Destructive (4)** — ask first.

- `backup_restore` — overwrites volumes with an archive
- `docker_stop`, `install_purge` — stops a service, deletes its data
- `proxmox_guest_shutdown`

### What the classes mean for you

- **read** — changes nothing on the machine, so running one needs no
  confirmation. What comes back is another matter: hostnames, internal
  addresses, what is listening, what is running, log contents. So read the
  machine the operator is asking about rather than every machine in the config;
  `--all` and `inventory_scan` are answers to a question somebody asked, not a
  way to begin. Summarise what matters instead of returning raw logs, port
  tables or JSON into a conversation other people can read.
- **write** — something changes on the machine, or a message leaves it. Do it when
  it follows from what was asked, and say afterwards what changed.
- **destructive** — **never on your own initiative.** Only when the operator asked
  for that specific action on that specific target, in the turn you are answering.
  Do not infer one from a goal: "free up space" is not permission to run
  `install_purge`, and "make it match production" is not permission to run
  `backup_restore`.

`backup_restore` and the Proxmox power tools take an explicit confirmation
argument, so a call without it fails rather than proceeding. That is a backstop,
not the rule — the rule is that the operator asked.

When an action is refused for lack of confirmation, say what would be destroyed
and let the operator decide. Do not re-send the same call with the confirmation
set.

Two things about reads that are easy to miss:

- **A remote read is not free.** It is an SSH round trip to somebody's server,
  every time. Polling in a loop is a cost they pay.
- **"What changed?" does not need a sweep.** `report` already answers it by
  comparing against the last snapshot, which is why it is the first thing to
  reach for rather than a tour of every tool.

## Start here: what changed?

`report` is the answer to "how is my server doing?" — it compares the machine
against the last snapshot rather than describing the present.

Each change carries a **kind**, and `--json` carries the same word, so branch on
it rather than reading the sentence:

| Kind | Means |
| --- | --- |
| `gone` | it was there last time and is not now |
| `new` | it was not there last time and is now |
| `replaced` | same name, different thing underneath — a recreated container |
| `image` | same container, different image |
| `state` | running where it was stopped, or the reverse |
| `port` | same port, a different process answering on it |
| `disk` | a mount moved by more than half a gigabyte |
| `skipped` | the comparison could not be made — not an all-clear |

```json
{"kind": "replaced", "target": "vaultwarden",
 "detail": "recreated, 4f2a1c → 9b7e03, vaultwarden:1.32 → vaultwarden:1.33",
 "text": "replaced: vaultwarden — recreated, …"}
```

`needs_attention` and `suggested_actions` have the same shape. An action may
carry a `command`, and when it does it also carries `runner` and `tool`:

- `runner: mcp` — call `tool` and carry it out
- `runner: cli` — homebutler can do it, no tool exposes it; the operator runs it
- `runner: shell` — not a homebutler command at all

An action with nothing to run — "address the items above" — has no `command`,
and then no `runner` either. All three are omitted rather than sent empty, so
absence means there is nothing to offer rather than something unclassified.

`doctor` findings carry the same three fields. **Check `runner` before offering
to fix something.**

## What needs a shell

```bash
homebutler init
homebutler trust <server>
homebutler watch install
homebutler watch tui
homebutler serve --token <token>
homebutler deploy --server <name>
homebutler upgrade
```

No tool exposes any of these, and a test checks that against the registry
rather than trusting this list.

### `restore` has a tool, and you should still run it yourself

`restore` writes over the data an app is running on, and the path it writes to
comes from the archive rather than from you — which is why the CLI refuses a
bind mount unless you name the path with `--allow-bind`. There is a
`backup_restore` tool, and over MCP it never restores bind mounts, for the same
reason: an agent has no way to name a host path it is allowed to write to, so
the archive's bind mounts are refused and reported in the result.

```bash
homebutler restore <archive>
```

### `trust`, and when it is required

```bash
homebutler trust <server>
homebutler trust <server> --reset
```

Since **0.34.0**, a server that signs in with a **password** must be trusted
before the first connection: homebutler will not send a password to a host it
has not been told to trust, because whatever answers at that address would
receive it. **Key authentication still trusts on first use** — the private key
never leaves the machine.

### `watch` supervises, `watch tui` displays

```bash
homebutler watch add <container>
homebutler watch install
homebutler watch tui
```

`watch` is a restart tracker: it records incidents, captures the logs from the
moment a container went down, and notifies. `watch install` hands that loop to
the machine's own supervisor — a systemd user unit or a launchd agent — so it
keeps running after logout. That is the one part of homebutler that stays
running, and it runs where the operator installed it, not on the machines it
watches. It is not a live dashboard — that is
`watch tui`, and it is for a person rather than an agent.

### `serve` edits, with a token

```bash
homebutler serve
homebutler serve --token <token>
homebutler serve --host 0.0.0.0 --token <token>
```

Since **0.33.0** the dashboard edits the config file: alert thresholds,
notification channels, Wake-on-LAN devices, servers and Proxmox endpoints.
Without `--token` it is read-only and the write routes do not exist at all. The
Report tab shows what `report` reports. There is a container image,
`ghcr.io/higangssh/homebutler`, which reaches the machines in `servers:` over
SSH — a container cannot see the host it runs on, and says so rather than
answering with its own numbers.

## Notifications

Channels: **telegram**, **slack**, **discord**, **webhook**, **ntfy**, **gotify**.
ntfy and Gotify are the two self-hosted push servers, and each takes its own
shape rather than a webhook payload. Tokens travel in a header, never in a URL,
so a failed request cannot put one in a log.

```bash
homebutler notify test
```

`notify_test` reports each channel separately, so a failure names the channel
that failed rather than all of them.

## Proxmox

Configured under `proxmox:` with an API token. The read credential and the one
that performs guest actions are separate: without an action token, start, reboot
and shutdown are unavailable rather than falling back to the read credential.

```bash
homebutler proxmox status
```

## Backups, and proving one comes back

```bash
homebutler backup
homebutler backup list
homebutler backup drill <app>
homebutler backup drill --all
homebutler restore <archive>
```

`backup drill` is the one to reach for when somebody asks whether backups are
trustworthy: it unpacks the archive into a container with a network and port of
its own, starts the app on that data, and requires an HTTP health check to
answer.

## Installing apps

```bash
homebutler install list
homebutler install <app>
homebutler install status <app>
homebutler install uninstall <app>
homebutler install purge <app>
```

## Output and config

Every command takes `--json`. Commands that can reach another machine take
`--server <name>` and `--all`.

Config is found in this order: `--config <path>`, `$HOMEBUTLER_CONFIG`,
`~/.config/homebutler/config.yaml`, `./homebutler.yaml`. Sections: `servers`,
`wake`, `alerts`, `notify`, `proxmox`, `watch`, `backup`.

```bash
homebutler config validate
```

## Prerequisites

Install a version, not whatever is newest at the moment the command runs. An
agent that installs an unpinned executable cannot say what it ran.

```bash
brew install Higangssh/homebutler/homebutler       # pinned formula, our own tap
go install github.com/Higangssh/homebutler@v0.38.0
```

Taking a release archive instead means checking it against the checksums the
release publishes:

```bash
V=0.38.0
BASE=https://github.com/Higangssh/homebutler/releases/download/v$V
curl -fsSLO $BASE/homebutler_${V}_linux_amd64.tar.gz
curl -fsSLO $BASE/checksums.txt
sha256sum --check --ignore-missing checksums.txt   # macOS: shasum -a 256 --check …
# must print: homebutler_0.38.0_linux_amd64.tar.gz: OK
tar xzf homebutler_${V}_linux_amd64.tar.gz
```

There is a container image, `ghcr.io/higangssh/homebutler:0.38.0`, pinned the
same way.
