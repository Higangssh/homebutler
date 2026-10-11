# Proxmox VE Helper-Scripts draft

A draft of `ct/homebutler.sh`, `install/homebutler-install.sh` and
`json/homebutler.json` in the shape [community-scripts/DevScripts](https://github.com/community-scripts/DevScripts)
asks for, so a submission there starts from something already checked against
their rules. New scripts go to DevScripts first; a maintainer promotes them to
ProxmoxVE once tested. Nothing here has been submitted.

The container gets the release binary in `/opt/homebutler`, a random token in
`/root/.config/homebutler/config.yaml` (mode 0600), and a system unit running
`homebutler serve --host 0.0.0.0 --port 8080` with that config.

## Decisions

- **`serve install` is not used.** It writes a systemd *user* unit, which is
  right on a shared machine and wrong in a single-app LXC: DevScripts runs
  everything as root under a system unit, and a user unit would depend on
  root's user manager and lingering.
- **`var_arm64` is `yes`.** DevScripts decides it from the release assets:
  `yes` when the project publishes arm64 assets, unset only when that cannot be
  told. The release has `linux_arm64` next to `linux_amd64`, and that binary has
  run for eleven days on an arm64 Raspberry Pi. `architectures` in the JSON
  lists both. (The first draft left it unset on the reading that `yes` needed
  the script itself run on arm64; AGENTS.md does not say that.)
- **The token is `openssl rand -hex 32`**, which their anti-pattern list names
  as the form for hex tokens; `random_password` is for passwords.
- **Category 9, Monitoring & Analytics.** 512 MB, 1 core, 2 GB disk: one Go
  binary and its snapshots.
- **The JSON says plainly that a homebutler in an LXC sees the LXC.** The node
  comes in through `proxmox:` with an API token and other machines through
  `servers:`; neither is automated, because both need credentials the user
  creates.

## Verified

- `bash -n` passes on both scripts. `shellcheck -S warning` reports only SC1090
  on the bootstrap `source` line, which is the same line verbatim in their
  template.
- `json/homebutler.json` parses, and its fields follow the AGENTS.md structure.
- The asset pattern `homebutler_*_linux_$(arch_resolve).tar.gz` matches both
  `homebutler_1.0.0_linux_amd64.tar.gz` and `..._linux_arm64.tar.gz` on the
  v1.0.0 release (both HTTP 200), and the archive has `homebutler` at its root,
  so `prebuild` puts the binary at `/opt/homebutler/homebutler`.
- `arch_resolve` with no arguments returns `amd64`/`arm64`, read from
  `community-scripts/core` `core/core.func`.
- The service's exact invocation: `serve --config <file with web.token> --host
  0.0.0.0` was run with the 1.0.0 binary. `/api/status` answered 401 without
  the token and 200 with it, and the page loaded.
- The config path and `web.token` key match `docs/configuration.md` and
  `docs/web-dashboard.md`.
- The logo URL serves a real WebP (selfh.st already has a homebutler icon).

## UNVERIFIED

- **Never run on Proxmox VE, and never run inside an LXC.** Everything the
  helpers do (`build_container`, `fetch_and_deploy_gh_release`,
  `check_for_gh_release`, `update_script`) is read from their source, not
  observed.
- The update path, including `CLEAN_INSTALL=1` replacing the binary while the
  config in `/root` stays untouched.
- What the dashboard shows in an unprivileged LXC with no Docker: expected to be
  the container's own status with `docker` listed as a failed collector, not
  checked.
- An arm64 container. The binary is checked on arm64; this script in one is not.

## Gaps against DevScripts rules

- **No checksum check.** `fetch_and_deploy_gh_release` downloads without
  verifying, and adding a `sha256sum` step of our own would be the custom
  download logic their rules forbid. homebutler publishes `checksums.txt`; the
  helper does not read it. Worth raising with them rather than working around.
- `var_testurl` needs a DevScripts issue to point at, so it is left out until a
  submission exists.
- The archive's `README.md`, `LICENSE` and `CHANGELOG.md` land in
  `/opt/homebutler` beside the binary. Harmless, noted because `prebuild`
  deploys the whole archive.
