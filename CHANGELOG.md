# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### ✨ Features

- diagnose each configured Proxmox endpoint in `doctor` with read-only requests (#105). `doctor` said nothing about Proxmox before this; now every endpoint gets one finding, using the same `DefaultView` call `proxmox status` makes, so the two never disagree about what a token can reach. TLS, authentication, authorization, and transport failures stay distinguishable, and the action `doctor` prints never suggests widening a token to Administrator to make a check pass — it names the read-only PVEAuditor role instead

### ⚠️ Behavior changes

- **`doctor` now makes outbound network calls, one per configured Proxmox endpoint.** A host that is slow to answer or unreachable makes `doctor` take noticeably longer, and `--strict` cron jobs now exit non-zero for a Proxmox host that is merely rebooting or firewalled, not only for problems on the local machine

## [0.25.0](https://github.com/Higangssh/homebutler/compare/v0.24.0...v0.25.0) - 2026-09-02

**Every permission failure homebutler can hit printed the operator's next command last.** `backup`, `restore`, `install` and `upgrade` all led with the raw syscall error and put the one line that mattered underneath it — and the detail could not simply be dropped, because there was no way to ask for it back. This release inverts the order and adds the flag that makes dropping it safe.

```bash
$ homebutler backup --to /mnt/nas --service jellyfin
error: cannot create backup dir /mnt/nas — rerun with: sudo homebutler backup --to /mnt/nas --service jellyfin

$ homebutler backup --to /mnt/nas --service jellyfin --verbose
error: cannot create backup dir /mnt/nas: mkdir /mnt/nas/backup_20260902_143012.812/volumes: permission denied

  ⚠️  Try: sudo homebutler backup --to /mnt/nas --service jellyfin
```

### ✨ Features

- lead a permission failure with the command that fixes it, and add the global `--verbose` / `-v` flag (#45). The raw Go error came first and the hint trailed it, so the only actionable line arrived behind a syscall message nobody can act on. `HintError` carries what failed, the cause and the hint in separate fields and the display layer chooses between them, which meant every hint had to become a command that works when it is typed: `restore` names the archive the operator passed rather than a path inside the temporary directory it had already deleted, `backup` keeps `--to` and `--service` so obeying the hint does not quietly back up every service to the default location, and a registry write that fails after `docker compose up` has already gone through points at ownership instead of proposing a reinstall of something that is running. Thanks to [@lenny-ts](https://github.com/lenny-ts)

### ♻️ Refactoring

- give Proxmox failures a class the caller can branch on (#111). API errors were flat strings, so anything wanting to tell a TLS failure from an expired token had to match on message text. Nothing user-facing moves — every message is byte-identical and the token masking is untouched — but #104, #105 and #106 each independently need that distinction and would each have invented their own. The class is applied where the failure is made rather than where it is caught, which is what keeps certificate pinning typed: Go does not wrap a `VerifyPeerCertificate` callback's error, so a fingerprint mismatch is invisible to a check made downstream. Thanks to [@gsaraiva2109](https://github.com/gsaraiva2109)

### ⚠️ Behavior changes

- **Permission errors no longer print the underlying cause by default.** They lead with what failed and the command to rerun, and `--verbose` brings back the cause and the hint together. Anything parsing homebutler's stderr for the old shape needs updating
- **`--verbose` is forwarded to remote servers rather than stripped**, on the grounds that detail asked for locally should come back from the far side too. A remote host still on 0.24.0 or older answers `unknown flag: --verbose`, which surfaces as a failed remote command — upgrade the fleet together, or leave the flag off when the target is older

## [0.24.0](https://github.com/Higangssh/homebutler/compare/v0.23.1...v0.24.0) - 2026-08-31

**The largest files homebutler writes were the only ones nothing ever deleted, and the safety of restoring them belonged to a program homebutler merely invoked.** `backup` wrote an archive on every run with no retention of any kind — fine while a person types the command, and the first thing to hurt whoever puts it in cron. Restore, meanwhile, held its destination only because GNU tar and bsdtar decline dangerous member names on their own; nothing in this repository asserted it and no test covered it. Separately, the MCP server had been answering every handshake with the first protocol revision ever published.

```yaml
backup:
  dir: /mnt/nas/backups/homebutler   # the spelling the docs have always shown, now read
  retention:
    max_archives: 7
    max_bytes: 20GB
```

### ✨ Features

- bound the backup directory with `backup.retention` (#102). `backup` wrote an archive on every run and nothing ever deleted one — the largest files homebutler writes, and the only ones with no limit at all, which is fine until somebody puts the command in cron. `max_archives` caps the count and `max_bytes` caps the total, because ten archives can be 200MB or 200GB and a disk guarantee is what the cron case actually wants. Both are off by default: a pruned incident costs some history, while a pruned backup can be the last copy of data that no longer exists, so this is the one store you opt into bounding. Pruning runs only after a backup is written, never after a failed one, and names each archive it removed
- warn in `doctor` when the backup directory is large and has no retention configured. Keeping everything is only a safe default while something says when the directory has outgrown what was expected, and `doctor` checked that a backup was *recent*, which says nothing about one that has been growing since the day it was created
- negotiate the MCP protocol version instead of declaring the oldest one that exists (#85). Every `initialize` was answered with `2024-11-05`, the first revision ever published, and the client's requested version was never read at all. homebutler now implements `2025-11-25`, `2025-06-18`, `2025-03-26` and `2024-11-05`, answers with the one the client asked for when it is among them, and with the newest otherwise. Nothing was broken by the old behaviour — a server may answer with any version it supports — but it was the oldest thing it could conformantly say, and clients cap their behaviour to it

### 🐛 Fixes

- answer `ping` (#85). "The receiver **MUST** respond promptly with an empty response" — initiating a ping is optional, answering one is not, and homebutler came back with `-32601 method not found`, which a client is entitled to read as a dead connection
- name a backup down to the millisecond (#115). The name stopped at the minute, so two backups started inside the same minute resolved to the same path and the second silently replaced the first, both reporting success — `backup --service a` followed by `backup --service b` left only `b`, with nothing to show `a` had been taken. Seconds were not enough either: a small project backs up fast enough that consecutive runs land in the same second. An archive that somehow already exists is now refused rather than overwritten, and the check runs before any work so a refusal leaves nothing behind
- read `backup.dir`, the setting `docs/backup.md` has documented all along. The schema only ever had the top-level `backup_dir`, so a config copied out of the documentation parsed without complaint and then wrote to the home directory — an operator pointing backups at a NAS got neither the destination they asked for nor any sign they had not. Both spellings are read, and `backup_dir` keeps working

### 🔐 Security

- check archive member names in homebutler rather than relying on `tar` to decline them (#87). `restore` and `backup drill` extracted with `tar xzf -C <dir>`, which held the destination only because GNU tar and bsdtar strip a leading `/` and skip members containing `..` of their own accord. Nothing here asserted it and no test covered it, so a host with a different tar would have lost the property with no signal. Extraction is now done in homebutler, and a member that is absolute, that climbs out of the root, that is a hard link to a file outside the tree, or that is written through a symlink an earlier member of the same archive planted, fails the restore instead of being skipped
- drop AppleDouble sidecars instead of restoring them as files. Archiving on macOS splits a file carrying extended attributes into the file plus a `._<file>` sibling, and `bsdtar` absorbs the sibling on the way back out — so taking extraction off `tar` meant those siblings started landing in restored volumes as files that were never in the source. A member is only dropped when it actually carries the AppleDouble magic, so a real file named `._notes` still restores

### ♻️ Refactoring

- consolidate the `ss` and `lsof` parser tests into `internal/ports` and un-export the parsers (#38). The fixtures lived in `internal/inventory` on the mistaken belief that parsing lived there, which forced `ParseLinuxOutput` and `ParseDarwinOutput` to be exported purely so another package's tests could reach them. The replacement tests also stopped indexing into `ports[0]` and `ports[2]`, which was asserting parser ordering as much as parser output. Thanks to [@lenny-ts](https://github.com/lenny-ts)
- derive a mount's archive path in one place (#92). `mountHasPayload`, `restoreMount` and `backup drill` each rebuilt `volDir/sanitizeName(name).tar.gz` independently, and the first two agreeing is load-bearing: `mountHasPayload` decides whether the containment check runs at all, and `restoreMount` decides what gets written. They agreed by identical code rather than by construction

### ⚠️ Behavior changes

- **Backup archives are named `backup_<date>_<hhmmss.mmm>.tar.gz`**, where they were `backup_<date>_<hhmm>.tar.gz`. Existing archives are untouched and still list and restore; anything matching on the old name shape will need updating
- **An archive member that would be written outside its destination now fails the restore**, where `tar` skipped it and reported success. A restore that was quietly dropping members will now stop and name the member it refused. Mounts already restored before the failure stay restored — a mount target that was never permitted is still reported under `refused` and skipped, because that is an operator configuration answer; a member that lies about where it belongs is the archive being untrustworthy, and continuing to read from it is not the safe response

### 🧪 Tests

- cover the four escapes the extractor now refuses, and the behaviour it has to preserve: file modes, modification times, symlinks, ownership when running as root, and a directory stored read-only whose contents must still be written into it
- cover retention against the cases that decide whether it is safe — an unconfigured policy that deletes nothing, count and size limits together, a limit smaller than a single archive, and files in the directory that are not backups

## [0.23.1](https://github.com/Higangssh/homebutler/compare/v0.23.0...v0.23.1) - 2026-08-30

**`--allow-bind` could be walked out of by the archive it was meant to contain.** The check that a bind target sits inside a permitted root compared paths as text, and text does not follow symlinks. One archive with two bind mounts defeats it without needing anything to exist on the host beforehand: the first restores normally inside the permitted root and its payload plants a symlink there, the second names that symlink as its source, and the extraction follows it wherever it points. Upgrade if you have ever run `homebutler restore --allow-bind` on an archive from anywhere but your own machine.

```bash
brew upgrade homebutler
curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh
```

### 🔐 Security

- resolve symlinks before deciding a bind target is inside a permitted root (CWE-59 / CWE-22). The containment check now runs per mount, immediately before that mount is written, because the symlink that breaks it does not exist until an earlier mount in the same archive has created it — a check made once up front cannot see it. The path that was checked is the path extracted into, and a target whose real location cannot be resolved is refused rather than assumed safe. A permitted root that is itself a symlink is resolved too, so naming one keeps working

Found by [@gsaraiva2109](https://github.com/gsaraiva2109) (#91). This defeats the containment added in v0.22.1 for GHSA-v8mc-vpp8-jr4p; versions v0.22.1 through v0.23.0 are affected. Named volumes and `backup drill` were never in scope.

### ⚠️ Behavior changes

- **A bind target that resolves outside every `--allow-bind` root is now refused**, where before only its spelling had to be inside one. A restore that used to write through a symlink out of the permitted root will now list that mount under `refused` and say where it resolved to. This is the intended containment, but it is a restore that will do less than it did before

### 🧪 Tests

- cover the two-mount escape end to end through `Restore`, a bind target that is an existing symlink out of the root, and the case that must keep working: a permitted root that is a symlink, with the target inside what it points to

## [0.23.0](https://github.com/Higangssh/homebutler/compare/v0.22.1...v0.23.0) - 2026-08-30

**Two things homebutler could see but not act on, and one it could not see at all.** A Proxmox cluster was readable and nothing else — no way to start a guest, no way to shut one down, and invisible in the dashboard entirely. And monitoring ran as two processes that did not know about each other: the loop that detected a container crash had no way to act on it, while the loop that could act never saw crashes.

```bash
homebutler proxmox guest shutdown --node pve1 --type lxc --vmid 105 --confirm
homebutler watch start
```

### ✨ Features

- start, shut down and reboot QEMU and LXC guests, and look up the status of a task already submitted. `shutdown` is Proxmox's graceful operation, not its `stop`, which cuts power and can leave a filesystem behind it — and `homebutler docker stop` is already graceful, so the same verb had to keep the same meaning across the CLI. Every action takes an explicit endpoint, node, type and VMID and refuses to run without `--confirm`, which is checked before the token is read. A successful action reports the task it submitted, never that the guest finished (#90)
- show a Proxmox cluster in `homebutler serve`: nodes, unified QEMU and LXC guests, storage, quorum. A section that could not be read renders as unavailable rather than as zero, so an ACL-limited token does not look like an empty rack (#79, #95)
- print the install command for a Proxmox Community Script, pinned to one commit, with a warning that it is not reviewed by homebutler and runs as root. homebutler never fetches or runs it. The catalogue lookup and the pin are the tedious part; the trigger stays with a person (#62, #94)
- run restart detection, resource thresholds and remediation rules in one process. `watch start` is now the monitoring process rather than half of one, with a single set of notification providers (#97, #98)

### 🐛 Fixes

- refuse a container name shaped like a flag before deciding whether to run locally or on a remote host. `docker_restart` with `name: "--help"` and a `server` set used to come back with homebutler's help text, exit zero, and read as a restart result — while the same call refused locally. Validation now runs once, ahead of the routing, and `docker_logs` gets the same treatment for `lines` (#83, #88)
- act on restart incidents with the rules engine. Remediation ran in the alerts process and restart detection ran in the watch process, so #49 fixed which target kinds `restart` understands without connecting the two halves. Same process now, and the rules engine is given the incident history so a target already flapping is not restarted further into it

### ⚠️ Behavior changes

- **`proxmox status --json` and the `proxmox_status` MCP tool report `failed_collectors: ["resources"]`** where a token is ACL-limited enough that `/cluster/resources` returns no nodes, guests or storage at all. A connected cluster always has at least one node, so an entirely empty response is a permission result rather than a real one, and three empty lists were the wrong way to say so. Anything parsing `failed_collectors` should account for the widened meaning
- **`watch start` now also checks CPU, memory and disk** against the thresholds in `alerts:`, which have had defaults of 90, 85 and 90 all along. An operator who ran `watch start` and `alerts --watch` side by side can stop running the second one; `alerts --watch` is unchanged and still does thresholds on their own

### 📝 Documentation

- add Proxmox to the README, which had never mentioned it across three merged pull requests. The command block had drifted past `report`, `inventory` and `notify` as well, and `restore` gained `--allow-bind` in v0.22.1 without the flag list following (#96)
- fill in the MCP tool table, which had been missing every Proxmox tool since #78

### 📦 Distribution

- allow a CI run to be started by hand. GitHub created no workflow run at all for one pull request, and the only way to get the checks `main` requires was to close and reopen it, which leaves a pair on a contributor's timeline that reads like a rejection (#93)

## [0.22.1](https://github.com/Higangssh/homebutler/compare/v0.22.0...v0.22.1) - 2026-08-26

**`restore` wrote to filesystem paths chosen by the backup archive.** `manifest.json` lives inside the archive being restored, so every path it declares belongs to whoever built that archive. homebutler trusted them. Restoring a backup you did not create yourself could create or overwrite any file the archive named — `~/.ssh/authorized_keys` was demonstrated — and through the volume path it could do so as root. Upgrade if you have ever run `homebutler restore` on an archive from anywhere but your own machine.

```bash
brew upgrade homebutler
curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh
```

### 🔐 Security

- refuse filesystem targets that only the archive asked for (GHSA-v8mc-vpp8-jr4p, CWE-22 / CWE-73). `restoreMount` extracted a bind mount to `m.Source` straight from the manifest, with no check that the path had anything to do with the operator. Bind targets are now restored only under a root named with `--allow-bind`, and refusals are reported rather than dropped
- reject volume names that are really host paths. `sanitizeName` guarded the archive filename lookup while the raw `m.Name` reached `docker run -v <name>:/target`, so a manifest naming `/etc` bind-mounted the host into a container the daemon runs as root — arbitrary write as root for anyone in the `docker` group, with no `sudo` prompt to notice. Volume names must now match Docker's own grammar
- refuse every bind mount over MCP. An agent has no way to name a host path it is permitted to write to, so `backup_restore` restores named volumes only

Reported by [@0xW41th](https://github.com/0xW41th). `backup drill` was never affected: it extracts into an isolation directory under a sanitized name and never reads `m.Source`.

### ⚠️ Behavior changes

- **Restoring a bind mount now requires `--allow-bind <path>`.** A backup you created yourself restores as before once you name the path it came from; the archive can no longer choose it for you. `restore --json` gains a `refused` array, and the human output lists what was declined and why, so a restore that did less than expected says so rather than reporting fewer volumes

### 🧪 Tests

- cover the reported path, the `..` escape out of an allowed root, and the volume names that reach `docker run -v` as host paths — alongside the cases that must keep working: real volume names, and a bind target the operator named

## [0.22.0](https://github.com/Higangssh/homebutler/compare/v0.21.2...v0.22.0) - 2026-08-24

**Two guards that were never running now run.** `watch` could detect a systemd or pm2 incident and had no way to act on one, so `action: restart` on a service failed with a docker error about a container that does not exist. And `config.Load` refuses a world-readable config holding plaintext secrets, but only ever looked at `servers[].password` — a config whose only credential was a Telegram bot token or a Slack webhook was never checked at all, which is the common case rather than a rare one.

```bash
homebutler inventory show --filter exposed
homebutler watch history --limit 10 --container nginx
homebutler watch list --json
```

### ✨ Features

- restart systemd units and pm2 apps from an alert rule. A rule carries `kind: docker` (default), `systemd` or `pm2`, written on the rule rather than resolved from the watch list so that restarting a host service is something asked for in the config rather than implied by a name appearing in another file (#49)
- warn at `alerts --watch` startup when a systemd restart rule is configured and homebutler is not running as root, rather than leaving it to be discovered when the rule first fires. `systemctl` needs root or a polkit rule, and a remediation that silently no-ops is worse than one that was never configured
- expose `watch_history`, `watch_list`, `processes` and `config_validate` over MCP, closing the gap where the README's own "why did this service restart at 3 AM?" was the question an agent could not ask (#54)
- add `--json` to `watch list`, which only ever printed a table, and `--limit`, `--container` and `--logs` to `watch history`
- accept `--filter` on `inventory show`, which advertised "same as scan" while rejecting the flag (#60, thanks @lenny-ts)
- record which collector failed in `Inventory.Failed` and the report snapshot, so an empty result can be told from a missing one. The exposed filter now withholds its `(none)` line only when the port scan itself failed, rather than whenever anything did (#69)

### 🐛 Fixes

- check permissions on a config whose only credential is a notification token. `hasSecrets` now walks the config for fields declaring themselves secret instead of naming the ones it knows, so the next credential-bearing section is covered by tagging its field rather than by remembering a list somewhere else (#61)
- tag credential fields `json:"-"` so they cannot be serialized at all. Nothing serializes a config today, but that was a property of the call sites rather than of the types
- collapse the two lists of supported inventory filters into one. Adding a filter to the slice and forgetting the renderer produced `unsupported filter "listening" (supported: exposed, listening)` — an error naming the value it had just rejected (#60, thanks @lenny-ts)

### ⚠️ Behavior changes

- **A config holding a notification credential with permissions looser than `0600` is now refused on load.** Telegram, Slack, Discord and generic webhook credentials all count. This check has always existed; it simply never ran for these fields. If homebutler starts refusing a config it used to accept, `chmod 600` it — the credential was readable by every user on the machine
- **A target that is flapping is no longer restarted automatically, including Docker targets.** Restarting something already in a restart loop feeds the loop, and most systemd units carry `Restart=always`, so homebutler restarting them fights systemd's own backoff. The thresholds are the existing `watch.flapping` ones, and the skip is reported rather than counted as either success or failure
- **`homebutler inventory scan --filter exposed` prints `(none)` in cases where it previously printed nothing.** It used to withhold the line whenever any collector had failed, so a host with Docker down and a healthy port scan finding nothing exposed was told nothing at all
- `watch list` says "No targets being watched" rather than "No containers", matching the rest of the command since systemd and pm2 support landed

### 📝 Documentation

- add `SECURITY.md` and enable private vulnerability reporting. Until now, anyone finding a vulnerability had a choice between a public issue and an email address the repository does not publish
- write down in `CONTRIBUTING.md` what homebutler accepts: the two-tier bar, what a new target has to prove, and what is waiting until 1.0
- document what is deliberately not exposed over MCP — `trust`, `upgrade`, the daemons, and `deploy` — so the omissions read as decisions
- document the `kind` field, the privilege requirement and the flapping interaction in the README

### ♻️ Internal

- make the MCP capability registry decide what a tool can be pointed at. `remoteSupport` read as the gate and was not one: nothing consulted it, and the real decision lived in the argv switch. They agreed by coincidence; they now agree structurally, before 1.0 freezes the surface (#56)
- fix demo mode advertising six tools it could not run (#56)
- give `report` and `doctor` the same answer about which ports are public (#57)

### 📦 Distribution

- validate the MCP Registry manifest before anything is published. v0.21.0 and v0.21.1 each spent a tag learning one rejection, because the only thing that read `server.json` was the registry itself, in the last step of the release, after the binaries and the npm package had already gone out

## [0.21.2](https://github.com/Higangssh/homebutler/compare/v0.21.1...v0.21.2) - 2026-08-23

**Second attempt at the MCP Registry listing.** v0.21.1 cleared the description limit and was then rejected with a 403: the registry grants publish rights to `io.github.<login>/*` from the GitHub OIDC token and keeps the login's casing, while `server.json` claimed `io.github.higangssh/homebutler` against a grant for `io.github.Higangssh/*`.

```bash
npx -y homebutler@latest
```

### 🐛 Fixes

- declare the server namespace with the repository owner's actual casing, in both `server.json` and the `mcpName` the npm package carries. The registry reads `mcpName` to confirm ownership of the package, so correcting one without the other would have traded the 403 for a different rejection at the same step. Nothing else about v0.21.1 changes

### 🧪 Tests

- derive the expected namespace from `repository.url` and assert `server.json` matches it, casing included
- assert the npm package's `mcpName` and the server name agree

## [0.21.1](https://github.com/Higangssh/homebutler/compare/v0.21.0...v0.21.1) - 2026-08-23

**Patch release to complete the MCP Registry listing.** v0.21.0 published its binaries, Homebrew tap and npm package, then failed on the last step of the release: the registry rejected a `server.json` description three characters over its 100-character limit. That step shipped in v0.21.0 itself, so the rejection means homebutler has never appeared in the registry at all.

```bash
npx -y homebutler@latest
```

### 🐛 Fixes

- shorten the `server.json` description to 94 characters so the MCP Registry accepts it. Nothing else about v0.21.0 changes; the binaries, tap and npm package are identical

### 🧪 Tests

- assert in CI that the `server.json` description fits the registry limit, and that the server version and its npm package version agree. Both constraints were previously only enforced by the registry, at the end of a release that had already published everything else

## [0.21.0](https://github.com/Higangssh/homebutler/compare/v0.20.0...v0.21.0) - 2026-08-23

**Three commands were confidently answering the wrong question.** `watch check` called systemd and pm2 targets clean without inspecting them, `report` counted fewer public ports than `doctor` did on the same scan, and a failed first connection blamed the host key rather than the reason it failed.

```bash
homebutler inventory scan --filter exposed
```

### ✨ Features

- add `inventory scan --filter exposed` to answer "what is reachable from outside" without reading the whole tree, reusing the container-owner and dedupe logic the default view already has (#51, thanks @lenny-ts)
- surface collection warnings in the filtered view, so a failed port scan never renders as an authoritative empty list
- expose `watch_check` over MCP, so an agent can ask what broke and not just what changed. Classified `riskWrite` rather than `riskRead`, since it records container state and saves any incident it finds (#55)
- cap incident history with `watch.retention.max_incidents`, default 200. `SaveIncident` wrote one file per incident and nothing ever deleted them, so the watch directory grew fastest exactly when something was wrong — a service restarting every 30s wrote roughly 2,880 files a day, each carrying 100 lines of captured logs (#50)
- align and colour the `report` and `doctor` output, with colour dropped automatically when piped, redirected, or run from cron (#42)

### 🐛 Fixes

- report what `watch check` actually inspected. It only examines docker targets — systemd and pm2 need a previous poll to compare against — but the skip was silent and the count included every registered target, so a watch list holding one systemd unit printed "Checked 1 container(s). No restarts detected." having looked at nothing (#46)
- count `[::]` and empty-address binds as public in `report`, which used its own inline comparison while `doctor` and the new exposure filter used `ports.IsPublicBind`. The same scan produced two different answers
- warn when `serve` binds to any wildcard address. The check matched only `0.0.0.0`, so `--host ::` exposed the dashboard with nothing printed
- don't blame the host key when the TOFU connection fails (#44)
- name the build matrix jobs after the platform they build. All four reported as "Build ()", so a cross-compile failure gave no way to tell which platform had broken (#53)

### ⚠️ Behavior changes

- **`watch check` now reports zero for a list it cannot inspect.** A watch list holding only systemd or pm2 targets previously read as a clean bill of health. It now prints what was checked, lists what was skipped, and points at `homebutler watch start`, which does monitor them
- **`PublicPortCount` in `report --json` rises on hosts with IPv6 wildcard listeners**, and now matches `doctor`. Anything comparing the two across versions will see the corrected value
- **`serve --host ::` now prints the exposure warning** it should always have printed
- **Incident files are pruned to the newest 200.** Existing directories are trimmed on the next write. Set `watch.retention.max_incidents` to change it; a negative value keeps everything. Files that cannot be parsed are never deleted

### 📝 Documentation

- write down which contributions homebutler accepts in `CONTRIBUTING.md`: the two-tier bar, what a new target has to prove, and what is waiting until 1.0
- replace the README `report` and `doctor` blocks with hand-written SVG terminal cards, since a fenced code block cannot show the colour that does most of the visual work (#43)
- add `doctor` to the tool table in `docs/mcp-server.md`, missing since it shipped in v0.19.0
- lead the README with what homebutler actually prints, and restore the logo and mascot placement

### 📦 Distribution

- publish releases to the MCP Registry after the npm step that proves ownership, now that `modelcontextprotocol/servers` has retired its third-party list (#52)
- drop the Go Report Card badge, which rendered as "go report | retired" after the service shut down and told every visitor the project was abandoned
- add the social preview card

### ♻️ Internal

- consolidate `isPublicBind` into `internal/ports` (#47)
- move the docker-only rule for `watch check` onto `Target.CheckSupported` so callers stop duplicating it

## [0.20.0](https://github.com/Higangssh/homebutler/compare/v0.19.2...v0.20.0) - 2026-08-18

**Config that fails silently now speaks up.** This release adds `homebutler config validate` and fixes three ways configuration could be ignored without producing any error at all — including a form documented in this project's own README that never worked.

```bash
homebutler config validate
homebutler config validate --strict
homebutler config validate --json
```

### ✨ Features

- add `homebutler config validate` to check the config file without starting a server, watcher, install, or remote connection
- report which file was used and which of the four resolution rules selected it
- report what homebutler made of each top-level section, so a section the file never set reads as `not set`
- flag keys that do not match the schema, with a did-you-mean suggestion at the top level, since `yaml.Unmarshal` drops them without a word
- treat a `--config` path that does not exist as an error rather than falling back to built-in defaults
- validate server names, hosts, ports, auth modes and duplicates, wake MAC addresses, alert thresholds, incomplete notify providers, `watch.notify_on` and `cooldown`, and the case where watch notifications are enabled with no provider configured to deliver them
- support `--strict` to exit non-zero on warnings as well as errors

### 🐛 Fixes

- load the config file in `watch start`, which never called `loadConfig()` and so could never reach the notify settings in `config.yaml` (#31)
- read the flat `watch.enabled` / `watch.notify_on` / `watch.cooldown` form the README has always documented; those keys were nested under `watch.notify` in the schema and were being dropped, leaving watch notifications off for anyone who configured them from the docs
- populate `PID` in the Linux port parser, which discarded the pid that `ss` already reports while the macOS parser kept it (#36)

### ⚠️ Behavior changes

- **watch notifications may start arriving after upgrading.** A config using the flat `watch.*` keys was silently running with notifications disabled; those settings now take effect as written
- **`homebutler ports` prints `process/PID` on Linux**, matching what macOS already printed. `nginx` now reads `nginx/5678`. The `pid` field in `--json` output is populated rather than empty

### 📝 Documentation

- document `config validate` in the README and `docs/configuration.md`, including the two silent-failure cases it exists to catch
- note that both the flat and nested `watch` forms are read, and that the nested block wins when a file contains both
- remove `output: json` from `homebutler.example.yaml`; it is not a config key, and JSON is a per-command flag
- correct the backup directory key in `docs/configuration.md` from `backup:` / `dir:` to `backup_dir:`

### 🧪 Tests

- add fixture-based tests for Linux `ss` and macOS `lsof` port parsing under `internal/inventory/testdata/` (#29, thanks @lenny-ts)
- cover multi-process `ss` entries, where a socket lists several pids and the first must win
- add 27 tests for config validation, covering resolution sources, missing files, unknown keys at any depth, every value check, and both `watch` config forms

### 📦 Distribution

- mark `cobra` and `go-isatty` as direct dependencies
- fail CI lint when `go.mod` is not tidy

## [0.19.2](https://github.com/Higangssh/homebutler/compare/v0.19.1...v0.19.2) - 2026-07-07

**Patch release to complete npm distribution.** This release carries the same remote deploy asset-name fix as v0.19.1 and republishes through the full release pipeline after refreshing npm credentials.

```bash
homebutler deploy --server pve1
homebutler deploy --all
```

### 🐛 Fixes

- keep GitHub-backed deploy downloads on the versioned release asset path used by GoReleaser

### 📦 Distribution

- publish the npm wrapper for the deploy download fix after refreshing the GitHub Actions npm token

## [0.19.1](https://github.com/Higangssh/homebutler/compare/v0.19.0...v0.19.1) - 2026-07-07

**Remote deploy downloads now match published release assets.** This patch fixes `homebutler deploy` so fresh remote installs download the versioned archives produced by GoReleaser.

```bash
homebutler deploy --server pve1
homebutler deploy --all
```

### 🐛 Fixes

- resolve the latest release version before GitHub-backed deploys so versioned assets such as `homebutler_0.19.1_linux_amd64.tar.gz` are downloaded instead of missing unversioned names
- require versioned release downloads and checksum lookups to prevent regressions to `releases/latest/download/homebutler_<os>_<arch>.tar.gz`

### ♻️ Changed

- split MCP capability metadata into a dedicated registry for cleaner server wiring

### 🧪 Tests

- add release download coverage for versioned asset paths and empty-version rejection
- verify with targeted remote/cmd tests, full test suite, build, and diff checks

## [0.19.0](https://github.com/Higangssh/homebutler/compare/v0.18.1...v0.19.0) - 2026-05-10

**Doctor check for the messy middle of self-hosting.** This release adds a read-only `doctor` command that turns common homelab risks into clear findings and next commands.

```bash
homebutler doctor
homebutler doctor --strict
homebutler doctor --json
```

### ✨ Features

- add `homebutler doctor` for resource pressure, stopped containers, public listeners, backup hygiene, notification readiness, and report baseline checks
- support `--strict` for CI/cron usage and `--backup-max-age` for backup freshness policy
- expose doctor through MCP, including remote server routing and demo responses
- support `doctor --all` summary output across configured servers

### 🧪 Tests

- add doctor unit coverage for healthy output, high memory/disk, stopped containers, public listeners, backup errors, missing backups, stale backups, invalid backup timestamps, and human output
- verify doctor on macOS and Linux arm64 Raspberry Pi
- run full test suite, build, JSON smoke test, and cross-compile check

### 📝 Documentation

- document doctor in README core workflows and command list

## [0.18.1](https://github.com/Higangssh/homebutler/compare/v0.18.0...v0.18.1) - 2026-05-03

**More useful MCP operations, cleaner ClawHub identity.** This patch expands the MCP server with operational tools and renames the published OpenClaw skill from `homeserver` to `homebutler` while keeping the old slug redirected.

```bash
homebutler mcp --demo
npx clawhub install homebutler
```

### ✨ Features

- expand MCP operations with additional homelab tools for reports, inventory, installs, backup drills, and related server workflows
- add demo responses for the expanded MCP tool surface

### 📝 Documentation

- polish the README header
- refresh MCP documentation for the expanded tool set
- rename the ClawHub skill identity from `homeserver` to `homebutler`
- ignore local `requirements.md` planning files so they are not committed accidentally

## [0.18.0](https://github.com/Higangssh/homebutler/compare/v0.17.0...v0.18.0) - 2026-04-29

**Your homelab gets a butler report.** This release adds a concise `report` command that snapshots server state, compares it with the previous run, and turns system health, containers, and public ports into a readable next-action summary.

```bash
homebutler report
homebutler report --keep 7
homebutler report --no-save
homebutler report --json
```

### ✨ Features

- add `homebutler report` for a butler-style health report with current status, needs attention, notable changes, and suggested actions
- create a baseline snapshot on first run, then compare future reports against the latest previous snapshot
- store report snapshots under `~/.homebutler/reports/snapshots/`
- add retention with `--keep` (default 30, minimum 1) so snapshots do not grow forever
- add `--no-save` for preview-only report runs
- support JSON output for automation and AI workflows

### 🎨 Branding

- add the HomeButler mascot to the README while keeping the existing logo as the official mark

### 🧪 Tests

- add report coverage for baseline creation, diff output, retention pruning, no-save behavior, and minimum keep handling
- verify the report command with real local runs, full test suite, race tests, build, lint, and CI

### 📝 Documentation

- document `report`, retention, preview mode, and JSON usage in README

## [0.17.0](https://github.com/Higangssh/homebutler/compare/v0.16.1...v0.17.0) - 2026-04-26

**Map your homelab before you fix it.** This release adds an inventory/topology view that turns system status, Docker containers, and open ports into a readable CLI map or Mermaid diagram.

```bash
homebutler inventory scan
homebutler inventory export --format mermaid
homebutler --json inventory scan
```

### ✨ Features

- add `homebutler inventory scan` and `homebutler inventory show` for a human-readable server inventory
- add `homebutler inventory export --format mermaid` for topology diagrams in GitHub, Obsidian, docs, and AI workflows
- connect Docker-published host ports back to the containers that expose them
- split app ports from system ports, show public vs local bind hints, and dedupe duplicate IPv4/IPv6 listeners
- keep Docker and port collection best-effort by surfacing warnings instead of failing the whole inventory scan

### 🧪 Tests

- add focused inventory collection, rendering, Mermaid, JSON, Docker port mapping, and dedupe coverage
- verify inventory on macOS and a Linux arm64 Raspberry Pi

### 📝 Documentation

- document inventory commands, tree output, and Mermaid export in README

## [0.16.1](https://github.com/Higangssh/homebutler/compare/v0.16.0...v0.16.1) - 2026-04-17

**Safer defaults and smoother watch notifications.** This patch tightens config and secret handling, adds a clearer `notify test` entry point, and keeps the watch-first config UX intact.

```bash
homebutler notify test               # send a test notification
homebutler watch start               # monitor processes with the watch-first flow
homebutler config path               # confirm the unified config location
```

### ✨ Features

- add `homebutler notify test` as the main notification test entry point while keeping `alerts test-notify` for backward compatibility

### 🔐 Security

- tighten config and secret handling, including stricter permissions for generated config files and secret-containing files
- reject insecure config file permissions when plaintext passwords are present
- use constant-time bearer token comparison in the web server
- write backup copies of compose and `.env` files with stricter permissions

### 🐛 Fixes

- guard watch notifier config lookup to avoid nil edge cases
- keep the unified config UX centered on `~/.config/homebutler/config.yaml` with legacy fallback support and deprecation warning for old paths
- remove the abandoned `monitor` wrapper experiment in favor of the `watch`-first UX
- clean up dead code and formatting around utility/config handling

### 🧪 Tests

- align config permission coverage with the tightened secret-handling behavior
- finish staticcheck nil-guard cleanup around alert/notification code paths

### 📝 Documentation

- refresh README watch guidance and notification config examples
- refresh `llms.txt` summary and remove stale internal planning documents

## [0.16.0](https://github.com/Higangssh/homebutler/compare/v0.15.0...v0.16.0) - 2026-04-10

**Understand why your processes keep dying.** Automatic crash analysis with exit code + log pattern matching, flapping detection for repeated restarts, and opt-in notification support.

```bash
homebutler watch start                  # crash analysis runs automatically
homebutler watch history                # [FLAPPING] tag + crash category
homebutler watch show <id>              # full crash analysis + flapping status
```

### ✨ Features

- add 2-tier flapping detection: acute (10m/3x) and chronic (24h/5x) windows
- add crash analysis with exit code mapping (OOM/SIGSEGV/SIGTERM) and log pattern matching (panic, connection refused, timeout, etc.)
- add watch notification system with per-container cooldown (default: disabled for air-gapped networks)
- add watch config file support (`~/.homebutler/watch/config.json`)
- show [FLAPPING] tag and crash category in `watch history`
- show full crash analysis and flapping status in `watch show`

### 🧪 Tests

- add 39 new tests covering flapping, crash analysis, notification, and config edge cases

## [0.15.0](https://github.com/Higangssh/homebutler/compare/v0.14.1...v0.15.0) - 2026-04-10

**Know why your processes died.** `homebutler watch` now monitors Docker containers, systemd services, and PM2 apps — capturing pre-death logs the moment a crash happens.

```bash
homebutler watch add nginx                          # interactive type selection
homebutler watch add --kind systemd nginx.service
homebutler watch start                              # real-time monitoring
homebutler watch show <id>                          # see why it crashed
```

### ✨ Features

- add multi-backend watch monitors: Docker (event streaming), systemd (state polling), PM2 (restart detection)
- capture pre-death logs on container/service crash via `docker events` real-time streaming
- add interactive process type selection to `watch add` (Docker / systemd / PM2)
- extend Target with `kind`/`unit` fields for multi-backend support (backward compatible)

### 🐛 Fixes

- eliminate duplicate `system.Status()` call in TUI alert rendering
- unify sparkline and progress bar color thresholds (70%/90%)
- preserve docker cache on timeout instead of discarding
- fix `truncate` to use rune count for proper unicode support
- apply De Morgan's law to satisfy staticcheck QF1001

### 🧪 Testing

- boost `internal/watch` coverage from 49% to 86%
- add edge case tests for all three monitors (malformed input, context cancel, error paths)
- add CheckTargets tests with mixed target kinds

### 📝 Documentation

- update README with multi-backend watch and pre-death log capture

### 🧹 Chores

- remove unused demo and metadata files

## [0.14.1](https://github.com/Higangssh/homebutler/compare/v0.14.0...v0.14.1) - 2026-04-06

**Harder to misuse, easier to contribute.** This release tightens security around install and alerts, adds local contribution guardrails, and improves test coverage across the core runtime.

### 🐛 Fixes

- prevent `install --dry-run` from creating files or triggering post-install verification
- fix staticcheck warning in `playbook_test.go`
- format `install_test.go` to keep CI green

### 🔐 Security

- block path traversal in install, uninstall, purge, and related app path handling
- add timeout support for `exec` alert actions to avoid stuck watcher runs
- bind web server to `127.0.0.1` by default and add optional token auth for API access
- validate ports before shell-based checks and drain HTTP response bodies properly
- clean up partial compose output on render failure

### 🧪 Testing

- raise coverage for core internal packages:
  - `internal/mcp` → 54.2%
  - `internal/config` → 87.7%
  - `internal/network` → 85.9%
  - `internal/backup` → 50.5%
  - `internal/remote` → 51.0%
  - `internal/wake` → 86.4%

### 📝 Documentation

- add `CONTRIBUTING.md` with required local checks before PR submission
- add PR template with `gofmt`, build, and test checklist

## [0.14.0](https://github.com/Higangssh/homebutler/compare/v0.13.0...v0.14.0) - 2026-04-05

**Self-Healing — your homelab fixes itself while you sleep.** Define rules in YAML, and homebutler watches your servers and takes action automatically. Plus multi-channel notifications and an interactive setup wizard.

### 🚀 Features

- add self-healing engine with YAML-defined rules and playbook execution
- support 4 metrics: `cpu`, `memory`, `disk`, `container`
- support 3 actions: `notify`, `restart` (docker restart), `exec` (run any command)
- cooldown support to prevent alert storms
- `alerts init` is now interactive — walks you through threshold, container, and webhook setup
- confirmation step after container selection ("Correct? [Y/n]")
- input hints for container selection format
- add multi-provider notification system: Telegram, Slack, Discord, generic webhook
- `alerts test-notify` command to verify notification channels
- `alerts history` to view past events and remediation results
- `alerts --watch` for continuous self-healing daemon mode

### 📝 Documentation

- add self-healing section to README with YAML examples
- add multi-channel notification docs to README
- add homebutler.dev website link to README

### 🐛 Fixes

- fix gofmt formatting in alerts rules

## [0.13.0](https://github.com/Higangssh/homebutler/compare/v0.12.2...v0.13.0) - 2026-04-04

**Backup Drill — prove your backups actually work.** Run a restore rehearsal in an isolated Docker environment. No risk to your running services.

```bash
homebutler backup drill uptime-kuma        # verify a single app
homebutler backup drill --all               # verify all apps in the backup
homebutler backup drill --json              # machine-readable output
homebutler backup drill --archive ./file    # use a specific backup
```

### 🚀 Features

- add `backup drill` command — automated restore verification in isolated containers
- 5-stage pipeline: locate → verify → isolate → boot → prove
- health checks for 10 apps (nginx-proxy-manager, vaultwarden, uptime-kuma, pi-hole, gitea, jellyfin, plex, portainer, homepage, adguard-home)
- `--all` flag to drill every supported app in one run
- `--archive` flag to target a specific backup file
- `--json` output for automation and MCP integration
- isolated Docker network + random port per drill (zero impact on running services)
- automatic cleanup of temporary containers, networks, and volumes
- friendly error messages with recovery hints

## [0.12.2](https://github.com/Higangssh/homebutler/compare/v0.12.1...v0.12.2) - 2026-04-04

**Better `ps`, plus Plex.** Process output is clearer when CPU is idle, memory now shows real RSS sizes, and Plex joins the installable app list.

```bash
homebutler ps                 # CPU + MEM% + RSS
homebutler ps --sort mem      # top processes by memory
homebutler install plex       # install Plex Media Server
homebutler install plex --media /path/to/media
```

### 🚀 Features

- add RSS column to `ps` output with human-readable memory sizes (`K`, `M`, `G`)
- add Plex Media Server to installable apps with `--media` mount support
- add Plex post-install guidance for initial web setup

### 🐛 Fixed

- show `sorted by memory instead` notice when all processes are at `0.0%` CPU
- widen PID column in `ps` output to handle 7+ digit PIDs cleanly

## [0.12.1](https://github.com/Higangssh/homebutler/compare/v0.12.0...v0.12.1) - 2026-04-03

### 🐛 Fixed

- filter kernel threads from `ps` output (kthreadd, kworker, rcu_*, etc.)
- add secondary sort: CPU tie → sort by memory, memory tie → sort by CPU

### 📦 Other

- add `ps` command to README CLI reference

## [0.12.0](https://github.com/Higangssh/homebutler/compare/v0.11.2...v0.12.0) - 2026-04-03

**Process monitoring + better error messages.** New `ps` command shows top processes with zombie detection. Permission errors now tell you exactly what to do.

```bash
homebutler ps                # top 10 by CPU
homebutler ps --sort mem     # top 10 by memory
homebutler ps --limit 20     # top 20
homebutler ps --all           # show everything
```

### 🚀 Features

- add `processes` command with `ps` alias for quick process monitoring
- `--sort cpu|mem` flag to sort by CPU or memory usage
- `--limit N` flag to control number of displayed processes (0 = all)
- zombie process detection with ⚠️ warning and PID listing
- process count summary (total + zombie count)

### 🐛 Fixed

- add sudo hints for permission errors across all commands:
  - `upgrade`: permission denied when replacing binary
  - `install`: directory creation and registry write failures
  - `uninstall`: registry update failures
  - `backup`: backup directory creation failures
  - `restore`: bind mount directory creation failures
- add `internal/util/permissions.go` helper for consistent permission error detection

## [0.11.2](https://github.com/Higangssh/homebutler/compare/v0.11.1...v0.11.2) - 2026-04-03

**Better UX for non-root users.** `homebutler ports` now tells you when process names are missing instead of showing blank columns.

```bash
homebutler ports    # shows ⚠️ hint if process info is missing
sudo homebutler ports   # full process info
```

### 🐛 Fixed

- show sudo hint when process names are missing in `ports` command
- address critical security and error handling issues

## [0.11.1](https://github.com/Higangssh/homebutler/compare/v0.11.0...v0.11.1) - 2026-04-01

**14 installable apps.** From monitoring to media streaming, DNS ad blocking to reverse proxies — all one command away.

```bash
homebutler install list              # see all 14 apps
homebutler install pi-hole           # DNS ad blocking
homebutler install jellyfin --media /movies  # media server
homebutler install portainer         # Docker GUI
```

### 🚀 Features

- add 8 new installable apps: homepage, stirling-pdf, speedtest-tracker, mealie, pi-hole, adguard-home, portainer, nginx-proxy-manager (total 14)
- add `--media` flag for jellyfin media directory mounting
- add safety checks: DNS port 53 conflict detection, mutual DNS app exclusion, port 80/443 check
- add Docker socket warning for portainer
- add post-install guidance: DNS setup, HTTPS access, default credential warnings
- auto-detect Docker socket path (Linux, colima, Docker Desktop) for portainer
- OS-specific DNS warnings (Linux: systemd-resolved, macOS: lsof)

### 🐛 Fixed

- install list `--json` now outputs proper JSON

### 📦 Other

- add `llms.txt` for AI search optimization
- update README with 14 apps table, options, and safety checks

## [0.11.0](https://github.com/Higangssh/homebutler/compare/v0.10.2...v0.11.0) - 2026-03-28

**Cobra CLI + docker stats.** The entire CLI is now powered by cobra — auto-generated help, shell completion, and cleaner flag handling. Plus a new `docker stats` command for real-time container resource monitoring.

```bash
homebutler docker stats          # per-container CPU, memory, network, I/O
homebutler completion zsh        # shell auto-completion
homebutler docker --help         # auto-generated sub-command help
```

### 🚀 Features

- add `docker stats` command for per-container resource usage (CPU, memory, network I/O, block I/O, PIDs)
- add `docker_stats` MCP tool (15th tool) with remote server support
- add `/api/docker/stats` web dashboard API endpoint
- add shell completion support for bash, zsh, and fish
- auto-generated help for all commands and sub-commands

### ♻️ Refactored

- migrate entire CLI from manual switch/case to cobra framework
- split monolithic root.go into per-command files (18 files)
- extract shared CLI helpers to cmd/helpers.go

### 🐛 Fixed

- wrap remote docker response to match local format (#21)

### 🧪 Tests

- boost test coverage: server 49→81%, ports 8→75%, docker 47→64%, remote 7→22%
- add docker stats parsing tests (7 cases)
- add docker stats API tests (7 cases)

### 📦 Other

- add Dockerfile for Glama MCP server inspection
- add glama.json for Glama author verification
- add Glama score badge to README

## [0.10.2](https://github.com/Higangssh/homebutler/compare/v0.10.1...v0.10.2) - 2026-03-21

**5 apps now installable with one command.** filebrowser, it-tools, and gitea join the registry.

```bash
homebutler install list          # see all 5 apps
homebutler install it-tools      # developer utilities in seconds
homebutler install gitea         # your own Git server
```

### 🚀 Features

- add filebrowser to install registry (web-based file manager)
- add it-tools to install registry (developer utility collection)
- add gitea to install registry (self-hosted Git service with SSH)
- show process/container name in port conflict messages
- check Docker container ports for colima/podman environments

## [0.10.1](https://github.com/Higangssh/homebutler/compare/v0.10.0...v0.10.1) - 2026-03-20

### 🧪 Tests

- add comprehensive install tests (registry, CRUD, template rendering, port check)
- add docker utility tests (socket detection, itoa)

## [0.10.0](https://github.com/Higangssh/homebutler/compare/v0.9.0...v0.10.0) - 2026-03-20

**One-command app deployment for your homelab.** Install, manage, and remove self-hosted apps with docker compose — no manual setup needed.

```bash
homebutler install uptime-kuma          # deploy in seconds
homebutler install vaultwarden --port 9090  # custom port
homebutler install status uptime-kuma   # check health
homebutler install uninstall uptime-kuma    # stop, keep data
homebutler install purge uptime-kuma    # remove everything
```

Each app gets its own `docker-compose.yml` at `~/.homebutler/apps/<app>/` with persistent data, pre-flight checks (docker, ports, duplicates), and cross-platform support (Linux, macOS, colima, podman).

### 🚀 Features

- add `install` command — deploy self-hosted apps with docker compose
- add `install list` — list available apps
- add `install status` — check installed app status
- add `install uninstall` — stop app, keep data
- add `install purge` — stop app, delete all data
- support `--port` flag for custom host port
- app registry: uptime-kuma, vaultwarden
- cross-platform docker socket detection (default, colima, podman)
- install registry (`installed.json`) to track app locations
- PUID/PGID support for compatible apps

### 🔒 Security

- harden SSH remote execution against shell injection (ShellQuote)
- add checksum verification for upgrade downloads


## [0.9.0](https://github.com/Higangssh/homebutler/compare/v0.8.2...v0.9.0) - 2026-03-11

### 🚀 Features

- add `backup` command — one-command Docker volume backup with compose files and env
- add `backup list` — list existing backups with size and timestamp
- add `restore` command — restore volumes from backup archive
- support `--service` flag for single-service backup/restore
- support `--to` flag for custom backup destination
- configurable `backup_dir` in homebutler.yml

### 🔒 Security

- warn when config file containing passwords has open permissions (recommend chmod 600)
- fix goroutine leak in network scan — context cancellation now stops ping sweep
- `ScanWithTimeout` properly cancels goroutines on timeout (no leak)

### 📖 Documentation

- split README into focused docs: `docs/backup.md`, `docs/configuration.md`, `docs/multi-server.md`, `docs/mcp-server.md`, `docs/web-dashboard.md`
- README slimmed from 719 to 386 lines with links to detailed docs
- add detailed backup documentation with how-it-works guide and security notes

### 🐛 Bug Fixes

- fix ineffective `break` in pingSweep `select` statement (staticcheck SA4011)
- handle empty config path gracefully (no panic on `Load("")`)
- log warning on backup temp directory cleanup failure

### 🧹 Chores

- rename `skill/` to `skills/` (convention)
- remove stale media files from git, update .gitignore
- add OpenClaw agent skill to repo

## [0.8.2](https://github.com/Higangssh/homebutler/compare/v0.8.1...v0.8.2) - 2026-03-02

### 🚀 Features

- add `alerts --watch` continuous monitoring mode
- configurable interval (`--interval`) and custom thresholds (`--config`)
- event deduplication (same alert won't repeat until recovered)
- aligned output formatting with fixed-width columns

## [0.8.1](https://github.com/Higangssh/homebutler/compare/v0.8.0...v0.8.1) - 2026-02-28

### ♻️ Refactor

- split cmd/root.go into deploy, upgrade, multiserver

### 🐛 Bug Fixes

- restore skills directory in git, only ignore skill symlink

### 🚀 Features

- add read-only config tab to web dashboard
- dynamic version in web dashboard + demo video
- implement graceful shutdown for http server (#12)
## [0.8.0](https://github.com/Higangssh/homebutler/compare/v0.7.1...v0.8.0) - 2026-02-27

### 🐛 Bug Fixes

- npm wrapper uses GitHub latest release, lazy install on first run

### 🔒 Security

- harden web server defaults

### 🚀 Features

- add upgrade command for self + remote server updates
- unify npm package name to homebutler
- add npm wrapper for zero-install MCP setup (npx homebutler-mcp)
- add MCP demo mode and Claude Code screenshots to README
- add Agent Skills support for Claude Code, Cursor, and more
## [0.7.1](https://github.com/Higangssh/homebutler/compare/v0.6.1...v0.7.1) - 2026-02-26

### 🐛 Bug Fixes

- use latest golangci-lint for Go 1.25+ compat
- use golangci-lint-action v7 for lint v2 support

### 🚀 Features

- add -v and --version aliases to version command
- wire server dropdown to switch all dashboard cards
## [0.6.1](https://github.com/Higangssh/homebutler/compare/v0.6.0...v0.6.1) - 2026-02-26

### 🐛 Bug Fixes

- remove goreleaser before hook (web built in CI step)
- build web frontend in CI before go build
## [0.6.0](https://github.com/Higangssh/homebutler/compare/v0.5.1...v0.6.0) - 2026-02-26

### 🐛 Bug Fixes

- update demo server count in test
- expand remote PATH for homebrew, snap, and go install
- hide empty wake array in generated config

### 🚀 Features

- add web dashboard with serve command
- add Dockerfile for MCP server (Glama registry)
## [0.5.1](https://github.com/Higangssh/homebutler/compare/v0.5.0...v0.5.1) - 2026-02-26

### ♻️ Refactor

- remove unused output config field

### 🐛 Bug Fixes

- improve SSH error messages with clear diagnostics and actions
- show 0% immediately on TUI start instead of waiting for data

### 🚀 Features

- redesign interactive init wizard
- add 'homebutler init' interactive setup wizard
- add project logo with rounded corners and update README header
- TOFU for SSH — auto-register unknown hosts, reject only on key change
- SSH known_hosts verification and instant CPU measurement
## [0.5.0](https://github.com/Higangssh/homebutler/compare/v0.4.0...v0.5.0) - 2026-02-26

### 🐛 Bug Fixes

- reorder demo GIF — TUI first, clear, then CLI commands
- reorder demo GIF (CLI first, TUI last) and reduce height
- widen demo GIF to prevent status output wrapping
- improve TUI layout and sparkline alignment

### 🚀 Features

- redesign TUI layout with History section and unified panels
- add sparkline history graphs and top processes panel
## [0.4.0](https://github.com/Higangssh/homebutler/compare/v0.3.0...v0.4.0) - 2026-02-25

### ♻️ Refactor

- simplify watch command, remove unused --all/--server flags

### 🐛 Bug Fixes

- reorder demo GIF to show TUI first, then CLI commands
- prevent goroutine leak in docker fetch
- preserve docker state when system data refreshes
- fetch docker data asynchronously in TUI
- improve tab bar label for clarity
- set DockerStatus for remote servers in TUI
- resolve TUI dashboard data loading issues

### 🚀 Features

- unified demo GIF with CLI commands + TUI dashboard
- add TUI demo GIF with dummy data renderer
- improve tab bar with numbered labels and server count
- improve footer keybinding hints in TUI
- show server name in system panel title
- show Docker status in TUI dashboard
- add TUI dashboard with 'watch' command
## [0.3.0](https://github.com/Higangssh/homebutler/compare/v0.2.1...v0.3.0) - 2026-02-24

### 🚀 Features

- add MCP server for AI tool integration
## [0.2.1](https://github.com/Higangssh/homebutler/compare/v0.2.0...v0.2.1) - 2026-02-24

### 🐛 Bug Fixes

- resolve go vet self-assignment in test
- validate docker logs line count and remove unused files

### 🚀 Features

- human-readable default output and GitHub Actions CI/CD
- add install script and improve PATH handling for deploy
## [0.2.0](https://github.com/Higangssh/homebutler/compare/v0.1.0...v0.2.0) - 2026-02-23

### 🚀 Features

- add deploy command for remote binary installation
- add multi-server support via SSH
- add config file auto-discovery with XDG support
## [0.1.0](https://github.com/Higangssh/homebutler/compare/...v0.1.0) - 2026-02-23

### 🐛 Bug Fixes

- filter incomplete ARP entries on Linux and return empty array for docker list

### 🚀 Features

- add OpenClaw skill wrapper for AI integration
- add demo GIF with sample data
- add build tooling, improve docker errors, and enhance README
- add alerts, config file loading, and WOL name support
- add network scan and filter multicast addresses
- initial project setup with core commands
