# Contributing to homebutler

Thanks for your interest in contributing!

## What homebutler is

homebutler operates a homelab server through one structured surface that both
people and AI agents use. It answers "what changed?" rather than "what is the
state right now?"

That sentence settles most scope questions. If you are unsure whether an idea
fits, open an issue before writing code — we would rather talk it through early
than turn down finished work.

## What we accept

### App catalogue additions

Adding an app to `homebutler install` is the easiest place to start, and the bar
is mechanical:

- Official or first-party image
- Pinned tag, not `:latest`
- Default ports that do not collide with the existing catalogue
- Volume and data paths following the existing convention
- A smoke test that passes: install, start, health check
- README app list updated

We do not debate whether an app belongs in a homelab. If the checklist passes,
it goes in.

### Everything else

Four questions, in order. A "no" is not a verdict on the idea — it tells you what
the PR is still missing.

1. **Which target?** homebutler covers `docker`, `systemd`, `pm2`, `ports`,
   `network`, and `system`. A *new* target is a larger commitment: open an issue
   first, and expect to answer questions 2 through 4 for the whole target.
2. **Does it emit JSON?** Human-readable output is not enough. Anything a person
   can ask for, an agent should be able to parse — including on the `--json` path.
3. **Which question does it answer?** The README lists the ones homebutler exists
   for: what is running, which container owns this port, why did this restart at
   3 AM, is the backup restorable, what is reachable from outside. A new question
   needs a case for why operators ask it.
4. **If it detects a problem, can something act on it?** Either remediation
   arrives with the detection, or an issue is open committing to it. A read-only
   first phase is fine when a second phase is named. Reporting state — `docker
   top`, `docker inspect` — is not detection and this question does not apply.

## Priorities

Depth before breadth. Covering the existing targets thoroughly comes before
adding new ones.

Working toward 1.0, which freezes the MCP tool surface and the JSON schema:

- Bound the incident directory in bytes, not only in files (#81)
- Give `watch` a way to survive logout and reboot (#80)
- Compare identities rather than counts, so a swap is not read as no change
  (#58, #59)

The first two are commitments the code already makes and does not keep. The
third is the question the tool exists to answer, which is why it comes before
adding more things to ask it about.

New targets generally wait until after 1.0, so open an issue before starting one.
Work already discussed and agreed in an issue keeps the terms it was given —
Proxmox (#32) is on Phase 2 now that read-only visibility has landed, with the
dashboard tracked separately in #79 and the Phase 3 question still open in #62.

## Before submitting a PR

Please run these checks locally before pushing:

```bash
# Format code
gofmt -w .

# Run linter
golangci-lint run

# Run tests
go test ./...

# Build
go build ./...
```

All four must pass. CI will reject PRs that fail any of these.

## Code style

- Follow standard Go conventions
- Run `gofmt` on all `.go` files
- No unused variables or imports
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (e.g. `feat:`, `fix:`, `docs:`)

## PR guidelines

- One feature/fix per PR
- Include tests for new functionality
- Update README if adding user-facing features
- Keep PRs small and focused
