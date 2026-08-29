# Proxmox VE

HomeButler can inspect and perform a small set of confirmed guest power actions
against one or more Proxmox VE API endpoints without installing an agent on a
Proxmox node. Read operations report cluster resources, guests, nodes, recent
tasks, and the status of one asynchronous task.

## Setup

### Create a least-privilege API token

For read operations, the built-in `PVEAuditor` role is the recommended baseline.
It includes the audit privileges needed for the cluster, node, guest, storage,
and task views.

```bash
# Create a dedicated API user in the PVE realm.
pveum user add monitoring@pve --comment "least-privilege API user for homebutler"

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

Guest actions need `VM.PowerMgmt` on each selected `/vms/<vmid>` path. Keep
that permission separate from `PVEAuditor`; do not grant an administrator role
to HomeButler. Create a narrow role and grant it to both the user and the
privilege-separated token only for guests HomeButler may control:

```bash
pveum role add HomeButlerPower -privs VM.PowerMgmt
pveum acl modify /vms/100 --users 'monitoring@pve' --roles HomeButlerPower
pveum acl modify /vms/100 --tokens 'monitoring@pve!readonly' --roles HomeButlerPower
```

Repeat the two ACL commands only for other approved VMIDs. The task owner may
inspect its own task. Inspecting a task started by another user requires
`Sys.Audit` on the task's node; the read-only setup above already provides the
audit privileges used by HomeButler's read views.

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
homebutler proxmox guest start --endpoint pve --node pve1 --type qemu --vmid 100 --confirm
homebutler proxmox guest shutdown --endpoint pve --node pve1 --type lxc --vmid 101 --confirm
homebutler proxmox guest reboot --endpoint pve --node pve1 --type qemu --vmid 100 --confirm
homebutler proxmox task 'UPID:pve1:...' --endpoint pve --node pve1
homebutler proxmox task 'UPID:pve1:...' --endpoint pve --node pve1 --json
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

Guest actions always require explicit `--endpoint`, `--node`, `--type`,
`--vmid`, and `--confirm` values, even when only one endpoint is configured.
HomeButler does not discover or infer the node or guest type before an action.
Without `--confirm`, the command reports the full target and exact rerun flags;
it does not prompt interactively.

An action response means Proxmox accepted the asynchronous request. It does not
mean the guest transition completed. HomeButler returns the UPID exactly as
Proxmox supplied it and does not parse or poll it. Inspect it separately with
`proxmox task`. It reports `result` as one of three short tokens: `running`
for a task still in progress, `ok` for a stopped task with `exitstatus: OK`,
and `failed` for any other stopped exit status.

HomeButler never retries an action POST automatically. If a timeout or lost
connection happens after Proxmox received the request, retrying could submit the
same action twice. Treat that result as ambiguous and inspect Proxmox before
deciding whether to run the command again.

Phase 2 deliberately exposes graceful `shutdown`, not Proxmox hard `stop`.
QEMU hard stop is equivalent to pulling the power plug and can damage guest
data; LXC hard stop abruptly terminates container processes. A future hard-stop
operation, if needed, must use its own unmistakable name and safety contract.

## Community Scripts

```bash
homebutler proxmox script list
homebutler proxmox script show docker
```

`proxmox script show <slug>` prints an install command from the curated
[Proxmox VE Community Scripts](https://github.com/community-scripts/ProxmoxVE)
catalog. HomeButler only formats that command; it never fetches or runs it.
The command is pinned to one commit of the upstream repository rather than
`main`, so the bytes a script fetches today match a week from now — bumping
the pin is a deliberate, reviewable one-line change.

The output always carries a warning, in human text, `--json`, and over MCP
alike: the script is not reviewed by HomeButler, and it runs as root on the
Proxmox host. Read it, review the command yourself, and run it by hand.

## MCP tools

The MCP server exposes these read-only Proxmox tools. Each accepts `endpoint`
when needed to select among configured endpoints:

- `proxmox_status` - version, cluster status, and resources.
- `proxmox_guests` - unified guest list; optional `node`, `status`, and `type`
  (`qemu` or `lxc`) filters.
- `proxmox_node` - node detail; requires `node`.
- `proxmox_tasks` - recent tasks for a node; requires `node`.

Phase 2 adds four explicit tools:

- `proxmox_guest_start` - `riskWrite`; requires `endpoint`, `node`, `type`,
  `vmid`, and literal `confirm: true`.
- `proxmox_guest_reboot` - `riskWrite`; requires `endpoint`, `node`, `type`,
  `vmid`, and literal `confirm: true`.
- `proxmox_guest_shutdown` - `riskDestructive`; requires `endpoint`, `node`,
  `type`, `vmid`, and literal `confirm: true`.
- `proxmox_task_status` - `riskRead`; requires `endpoint`, `node`, and the
  opaque `upid`.

Risk metadata helps MCP clients make policy decisions, but it does not replace
runtime confirmation. Every action tool rejects a missing, false, or non-boolean
confirmation before HomeButler resolves the token or contacts Proxmox.

Two more tools cover Community Scripts and take no `endpoint`, since neither
contacts Proxmox:

- `proxmox_script_list` - `riskRead`; lists the curated catalog.
- `proxmox_script_command` - `riskRead`; requires `slug`; renders the pinned
  install command and a `warning` field with the same text the CLI prints.

## Excluded scope

HomeButler does not expose Proxmox hard stop, guest creation or deletion,
migration, reset, suspend or resume, snapshots, task polling, automatic POST
retries, arbitrary API actions, or root-only action overrides. The web
dashboard remains read-only and does not expose guest actions. HomeButler
prints Community Script install commands for a human to review and run; it
never fetches or executes them.
