# Web Dashboard

`homebutler serve` starts an embedded web dashboard — no Node.js, no Docker, no extra dependencies. The entire Svelte frontend is compiled into the Go binary at build time using `go:embed`.

```bash
homebutler serve                # http://localhost:8080
homebutler serve --port 3000    # custom port
```

## Keeping it running

`homebutler serve` runs for as long as the terminal it started in. To hand it to
the supervisor the host already has — a launchd agent on macOS, a systemd user
unit on Linux — install it:

```bash
homebutler serve install                 # supervised, on 127.0.0.1:8080
homebutler serve install --port 9090     # somewhere else
homebutler serve install --host 0.0.0.0  # needs a token, see below
homebutler serve installed               # what is installed, and the address it serves
homebutler serve uninstall               # stop it and remove the unit
```

The unit is user-level, for the same reasons `watch install`'s is: the config it
reads lives in the invoking user's home directory, and on macOS a LaunchDaemon
would outlive the logged-in session Docker Desktop needs. On Linux a user unit
stops at logout unless lingering is enabled, and `serve install` says so.

Reinstalling on a different port needs `--force`, which stops the running
service before writing the new unit. Overwriting the file alone is not enough:
launchd keeps the old address live until the agent is unloaded.

### The token is not in the unit

`serve install` writes the address and never the token. A unit file is
world-readable, and `--token` is visible in `ps` to every user on the machine,
so the installed dashboard reads its token from the config file instead:

```yaml
web:
  token: your-long-random-token
```

`doctor` already requires that file to be `0600` once it holds a secret.
`homebutler serve` reads the same key, so `--token` is now only needed for a
one-off run with a different one.

Installing on an address other machines can reach with no token configured is
refused. A foreground dashboard is exposed for as long as somebody is watching
the terminal; an installed one is exposed until it is uninstalled, and the
refusal names both ways out — set `web.token`, or bind `127.0.0.1`.

That refusal happens once, and the config file outlives it. `doctor` checks the
same thing on every run, because removing `web.token` afterwards leaves the
installed dashboard answering the network with nothing in front of it and
nothing fails to say so:

```
❌ [exposure] The installed dashboard answers the network with no token
   It is running on 0.0.0.0:8080 and ~/.config/homebutler/config.yaml has no
   web.token, so anyone who can reach this machine gets the dashboard.
   → Set web.token, or reinstall it on 127.0.0.1.
```

Access from another machine via SSH tunnel:

```bash
ssh -L 8080:localhost:8080 user@your-server
# Then open http://localhost:8080 in your browser
```

## Dashboard Cards

| Card | Description |
|---|---|
| **Server Overview** | All servers with live status (green/red dots), CPU, memory, uptime |
| **System Status** | CPU, memory, disk with color-coded progress bars |
| **Docker Containers** | Running/stopped with friendly status ("Running · 4d") |
| **Top Processes** | Top 10 by CPU usage with PID, CPU%, MEM% |
| **Alerts** | Threshold monitoring with OK / WARNING / CRITICAL |
| **Network Ports** | Open ports with process names |
| **Wake-on-LAN** | One-click wake buttons |
| **Proxmox VE** | Cluster quorum, nodes, QEMU/LXC guests, storage, and collector warnings |

## Tabs

| Tab | What it answers |
|---|---|
| **Dashboard** | What is true right now — servers, containers, ports, alerts |
| **Report** | What changed since the last snapshot, and what `doctor` thinks of the machine |
| **Watch** | What went down, why, and the logs captured at the time |
| **Config** | Every setting, editable with `--token`: thresholds, notification channels, servers, Proxmox endpoints, Wake-on-LAN devices |

The Report tab leads with what needs attention, then the changes with the kind
word each one carries, then the snapshot the comparison is against and how old
it is. Loading it does not save a snapshot — a page that saved one every time
it was opened would prune the baseline you wanted to compare against. Saving is
a button, and a write like any other.

## Reading the report from somewhere else

`GET /api/report` is the comparison as JSON, without saving a snapshot. It is
what a status page, a script on another machine, or a dashboard widget reads.

### In a Homepage dashboard

A [Homepage](https://gethomepage.dev) card that answers "is anything worth my
attention?" rather than adding a second CPU gauge. It uses Homepage's built-in
`customapi` widget, so there is nothing to install on the Homepage side. Add
this to `services.yaml`:

```yaml
- Homelab:
    - homebutler:
        href: http://your-server:8080
        description: what changed since the last snapshot
        widget:
          type: customapi
          url: http://your-server:8080/api/report
          refreshInterval: 60000
          headers:
            Authorization: Bearer YOUR_TOKEN
          mappings:
            - field: summary.needs_attention
              label: Needs attention
              format: number
            - field: summary.notable_changes
              label: Changes
              format: number
            - field: running_count
              label: Running
            - field: stopped_count
              label: Stopped
```

| Label | Field | What it counts |
|---|---|---|
| Needs attention | `summary.needs_attention` | Findings worth acting on: a stopped container, memory or a disk over 85%, a port newly open to every interface. `0` is the number you want |
| Changes | `summary.notable_changes` | Lines in the change list: what moved since the last saved snapshot, plus one line for each comparison that could not be made |
| Running | `running_count` | Docker containers running now |
| Stopped | `stopped_count` | Docker containers stopped now |

Run `homebutler report` once, or press **Save a snapshot** on the Report tab
(that needs a token), before trusting Changes. Until a snapshot exists there is
nothing to compare against, and Changes shows `1` — the line saying so. Loading
the widget never saves one, so after that Changes covers everything since the
last save; `homebutler report` on a schedule keeps the window short. While
Docker is unreachable, Changes includes a line saying containers were not
compared.

Running and Stopped show `-` when Docker did not answer. They are left without
`format: number` on purpose: homebutler sends `null` rather than a `0` it did
not see, and `format: number` would turn that into `NaN`.

`refreshInterval` is in milliseconds. Homepage's default is 10 seconds, and
every request collects the whole report again; once a minute is plenty for
counts that change when containers and ports do.

In the default block display Homepage shows the first four fields of a card;
`display: list` shows them all. `href` makes the card a link to the dashboard,
where the Report tab names what the numbers count. `summary` exists so each
value is one field; `format: size` on the `needs_attention` list counts it too.

Checked against Homepage 2.7.0: pasted as it is, with only the address and the
token filled in, the card shows all four values from `GET /api/report`,
and `-` for Running and Stopped while Docker was unreachable.

**The token.** If `serve` runs with a token (`web.token` or `--token`), every
`/api/*` route needs it, reads included; put it in `headers` as above. Without
a token, drop the `headers` block. Homepage makes the request from its own
server, not from the browser, so the token never reaches the page — nothing
Homepage served to the browser in that check contained it.

`your-server` is an address the Homepage container can reach. If Homepage runs
in Docker on the same machine as `serve`, `localhost` is the container itself;
use the machine's LAN address, or `host.docker.internal` on Docker Desktop.

`serve` has to be reachable from wherever the widget runs, which means binding
beyond `127.0.0.1`. The token then crosses the network, so put it behind a
reverse proxy with TLS or a tunnel; the settings screen says so when it sees a
public bind.
