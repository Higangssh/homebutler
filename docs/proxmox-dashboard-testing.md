# Testing the Proxmox dashboard

Use this guide to test the implementation of GitHub issue #79 before opening a
pull request. The dashboard is read-only: these checks must not start, stop, or
modify any Proxmox guest or cluster resource.

## Prerequisites

- A Proxmox endpoint already configured under `proxmox:`.
- A dedicated API token with the read-only `PVEAuditor` role.
- The token value stored in a `0600` file and referenced with `token_file`.
- TLS configured with a pinned `fingerprint` or `ca_file`. Use `insecure: true`
  only as a temporary test fallback.

See [proxmox.md](proxmox.md) for the complete least-privilege and TLS setup.

## Build and start the branch

```bash
git fetch fork
git switch feat/proxmox-dashboard
make build-all
./homebutler config validate
./homebutler serve --host 127.0.0.1 --port 8080
```

Open <http://127.0.0.1:8080>. Keep the terminal visible so connection or token
errors are easy to correlate with the page.

## Healthy endpoint

With one endpoint configured, verify that:

- The **Proxmox VE** card appears without an endpoint dropdown.
- The badge shows the configured endpoint name.
- The cluster name, quorum state, and online/total node count match Proxmox.
- Every node shows status, CPU percentage, memory usage, and uptime.
- QEMU and LXC guests share one table and show VMID, name, type, node, and status.
- Storage shows its name, node, status, and used/total bytes.
- Missing optional metrics show `—`; a measured zero shows `0.0%` or `0 B`.

Compare the card with the existing CLI response:

```bash
./homebutler proxmox status --json
```

The browser and CLI should describe the same cluster snapshot. Small metric
differences are expected if the two requests run at different times.

## Multiple endpoints

Configure a second Proxmox entry with a different `name`, then restart
`homebutler serve`.

- The card should show a labeled endpoint dropdown.
- Selecting either endpoint should replace all cluster, node, guest, and storage data.
- Switching quickly must not let a slower response overwrite the currently selected endpoint.
- The SSH server dropdown must not contain Proxmox endpoints.

Compare each selection explicitly:

```bash
./homebutler proxmox status --endpoint pve-a --json
./homebutler proxmox status --endpoint pve-b --json
```

## Empty versus unreadable

These are intentionally different states.

1. On a readable endpoint with no guests or no visible storage, verify the
   relevant section says `No guests found` or `No storage found`.
2. Create a separate test token with deliberately limited ACLs. Do not weaken
   the production token. Select that endpoint and verify:
   - A failed cluster collector says `Cluster status unavailable`.
   - A failed resources collector says node, guest, and storage inventory is
     unavailable, never empty.
   - Sections that remain readable still render normally.
   - The warning list explains which collector failed.
3. Use an unreachable test endpoint and verify the card reports unavailable
   collectors and warnings without showing an empty healthy cluster.

The machine-readable response can be checked directly:

```bash
curl -sS 'http://127.0.0.1:8080/api/proxmox/status?endpoint=pve-a'
```

A partial result should remain HTTP 200 and include both `warnings` and
`failed_collectors`. A successfully read empty collection must not list that
collector in `failed_collectors`.

## Security and accessibility

- Open `/api/proxmox/endpoints` and verify it contains endpoint names only. It
  must not expose token values, token IDs, token-file paths, CA paths, or fingerprints.
- Navigate to the endpoint selector with the keyboard, change it with arrow
  keys, and confirm the data updates.
- Verify loading, error, empty, and unavailable states use text rather than
  color alone.
- Test at 200% browser zoom and a narrow mobile viewport. Tables may scroll
  horizontally, but the page must not lose data or controls.
- If `serve --token` is part of your normal setup, repeat the API checks with
  the expected `Authorization: Bearer ...` header and confirm unauthenticated
  requests return HTTP 401.

## Pass criteria

The branch is ready for a pull request when all healthy data matches the CLI,
both endpoint selections work, empty and unreadable states are distinct, no
secret metadata appears in the API, and the card remains usable by keyboard
and at narrow widths.
