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

- Compare identities rather than counts, so a container swap is not read as no
  change (#58)
- Track processes and network rather than only observing them (#59)

Those two are the question homebutler exists to answer, which is why they come
before adding more things to ask it about. They also have to land before 1.0
rather than after: they change the shape of what `report` returns, and 1.0
freezes that shape.

New targets generally wait until after 1.0, so open an issue before starting one.
Work already discussed and agreed in an issue keeps the terms it was given.
Proxmox is past its original scope — #104, #105 and #107 are in review, and #106
is open and unclaimed.

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

If you touched anything under `web/`, there are two more. The component tests
are quick:

```bash
npm --prefix web test
```

The end-to-end suite drives a browser against a real `homebutler serve --demo`,
so it needs Chromium once:

```bash
npx --prefix web playwright install chromium
npm --prefix web run e2e
```

It starts both servers itself and covers the states that are otherwise only
reached by accident — a dashboard asking for its token, an endpoint returning
500, an incident opened to read the logs captured before the container died. CI
runs it in a job of its own, so a frontend change that builds and still breaks
in a browser fails there rather than on someone's dashboard.

If the change is visible to someone using homebutler — new output, a new flag, a
different default, a message that reads differently — add an entry to
`CHANGELOG.md` under `## [Unreleased]` in the same PR. Something that used to
work and now does not goes under `⚠️ Behavior changes`, which is the section
people read before upgrading. An internal change with no user-visible effect
does not need one. If you are unsure, write the line and let the review decide.

### Linting with what CI lints with

CI pins golangci-lint rather than taking the newest, because a linter that
upgrades itself breaks the build on a day nobody touched the repository. Match
it locally or you will pass here and fail there:

```bash
golangci-lint --version                                  # compare with .github/workflows/ci.yml
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
```

Dependabot raises the actions and the dependencies, not those pinned tool
versions — they are inputs to an action rather than a version of one. Raising
them is a pull request of its own, which is the point: CI runs the new linter
against the whole tree before it can fail a release.

### Changing something other people build on

Tool names, JSON fields, risk classes, `doctor`'s exit codes, documented config
keys and the HTTP routes are frozen at 1.0, and
[docs/compatibility.md](docs/compatibility.md) says exactly which of them and
what is still allowed to change. `go test ./internal/contract` fails when any
of it moves, with the diff and the command to regenerate the golden file. If
the change is deliberate, regenerate, commit the golden file alongside it, and
write the ⚠️ Behavior changes line.

### Cutting a release

The version in `skills/SKILL.md` is pinned, because a skill that tells an agent
to install whatever is newest gives it something it cannot account for
afterwards. So the release PR that moves `## [Unreleased]` to a version number
also updates that pin — a test compares the two and fails the build if they
drift apart, which is the reminder.

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
