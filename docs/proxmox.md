# Proxmox VE

HomeButler can inspect one or more Proxmox VE API endpoints without installing
an agent on a Proxmox node. Phase 1 is read-only: it reports cluster resources,
guests, nodes, and recent tasks.

## Setup

### Create a read-only API token

The built-in `PVEAuditor` role is the recommended minimum role. It is read-only
and includes the audit privileges needed for the cluster, node, guest, storage,
and task views.

```bash
# Create a token-only user in the PVE realm.
pveum user add monitoring@pve --comment "read-only API user for homebutler"

# Privilege separation is enabled by default. The token value is displayed once.
pveum user token add monitoring@pve readonly --privsep 1

# Grant PVEAuditor to both the user and token, propagated from /.
pveum acl modify / --users 'monitoring@pve' --roles PVEAuditor --propagate 1
pveum acl modify / --tokens 'monitoring@pve!readonly' --roles PVEAuditor --propagate 1

# Inspect the effective token permissions.
pveum user token permissions monitoring@pve readonly
```

With privilege separation, the token's effective permissions are the
intersection of the user ACLs and the token ACLs. Granting `PVEAuditor` only to
the user is not enough: the token can authenticate yet return empty resource
lists. The `/` ACL is recommended because `/cluster/status` needs `Sys.Audit`
on `/`.

Save the token value when it is printed. Proxmox does not reveal it again; if
it is lost, remove and create the token again. A newly granted token can briefly
return `403 Permission check failed (/, Sys.Audit)` while ACL changes propagate.

### Configure endpoints

Add endpoints under `proxmox:`, separately from SSH `servers:`. `token_file`
is preferred; its contents are the token value only. Keep the file readable
only by the account that runs HomeButler.

```yaml
proxmox:
  - name: pve
    host: 192.168.1.10
    port: 8006                 # optional; 8006 is the default
    token_id: monitoring@pve!readonly
    token_file: ~/.config/homebutler/pve.token
    fingerprint: "AB:CD:EF:..." # recommended for self-signed node certificates
    timeout: 10s               # optional; 10s is the default
```

`name`, `host`, and `token_id` are required. Configure exactly one credential
source: `token_file` or the inline `token`. Inline tokens are supported, but
HomeButler treats them as secrets and excludes them from JSON serialization.

## TLS

TLS verification is enabled by default and requires TLS 1.2 or later. Trust is
chosen in this order:

1. `fingerprint` pins the SHA-256 fingerprint of the leaf certificate. It takes
   precedence over the other settings and is a good fit for a self-signed
   Proxmox certificate.
2. `ca_file` trusts a PEM CA file and keeps normal certificate and hostname
   verification enabled.
3. `insecure: true` disables verification. Use it only as a temporary,
   explicitly unsafe fallback; it is never the default.

For example, obtain a SHA-256 fingerprint from a trusted connection:

```bash
openssl s_client -connect 192.168.1.10:8006 </dev/null 2>/dev/null \
  | openssl x509 -noout -fingerprint -sha256
```

## Usage

When exactly one endpoint is configured, `--endpoint` is optional. With more
than one endpoint, select one by name. Proxmox commands do not use SSH
`--server` or `--all` flags.

```bash
homebutler proxmox status
homebutler proxmox status --endpoint pve --json
homebutler proxmox guests --status running
homebutler proxmox guests --endpoint pve --node pve1 --status stopped
homebutler proxmox node pve1 --endpoint pve
homebutler proxmox tasks --endpoint pve --node pve1 --limit 50
```

Without `--node`, `proxmox tasks` queries recent tasks for each resource node.
`--status` accepts `running` or `stopped`; tasks default to 50 entries per node.

Human output is concise:

```text
Proxmox endpoint: pve
Version: 9.1.4
Resources: 2 nodes | 3 guests | 1 storage
```

Use `--json` for the full typed API view. CPU values remain Proxmox fractions
(`0..1`), byte counters remain integers, and QEMU and LXC guests are returned
in one `guests` list.

## MCP tools

The MCP server exposes read-only Proxmox tools. Each accepts `endpoint` when
needed to select among configured endpoints:

- `proxmox_status` — version, cluster status, and resources.
- `proxmox_guests` — unified guest list; optional `node`, `status`, and `type`
  (`qemu` or `lxc`) filters.
- `proxmox_node` — node detail; requires `node`.
- `proxmox_tasks` — recent tasks for a node; requires `node`.

## Not in Phase 1

Phase 1 does not start, stop, reboot, create, or otherwise change guests. The
web dashboard is a separate follow-up. Community Scripts provisioning is a
Phase 3 security decision and is not implemented here; HomeButler does not
fetch or execute external provisioning scripts.
