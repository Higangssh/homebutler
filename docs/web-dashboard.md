# Web Dashboard

`homebutler serve` starts an embedded web dashboard — no Node.js, no Docker, no extra dependencies. The entire Svelte frontend is compiled into the Go binary at build time using `go:embed`.

```bash
homebutler serve                # http://localhost:8080
homebutler serve --port 3000    # custom port
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

A [Homepage](https://gethomepage.dev) `customapi` widget showing what moved,
rather than a second CPU gauge:

```yaml
- Homelab:
    - homebutler:
        description: what changed since the last snapshot
        widget:
          type: customapi
          url: http://your-server:8080/api/report
          method: GET
          headers:
            Authorization: Bearer YOUR_TOKEN
          mappings:
            - field: server_name
              label: Server
            - field: summary.needs_attention
              label: Needs attention
              format: number
            - field: summary.notable_changes
              label: Changes
              format: number
```

The counts come from `summary`, which the HTTP response adds for exactly this:
`needs_attention` and `notable_changes` are lists, and a widget that maps a
field to a value has no way to take the length of one — pointed at the lists
themselves it renders `NaN`.

`serve` has to be reachable from wherever the widget runs, which means binding
beyond `127.0.0.1`. The token then crosses the network, so put it behind a
reverse proxy with TLS or a tunnel; the settings screen says so when it sees a
public bind.
