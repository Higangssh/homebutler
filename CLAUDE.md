# homebutler

A single Go binary for running a small home server: CLI, TUI, web dashboard, and
an MCP server, with no daemon and no database.

## Verify before claiming anything works

```bash
gofmt -w .
go vet ./...
golangci-lint run
go test ./...
go build ./...
```

All five must pass. `go test ./...` takes several minutes — `internal/network`
and `internal/remote` wait on real timeouts — so start it early rather than
running it last.

## Writing commits, PRs, and comments

Everything written here is read by users, contributors, and future maintainers.
Match the voice already in `git log` and `CHANGELOG.md`.

### No tool attribution

Never add `Co-Authored-By: Claude`, `Generated with Claude Code`, 🤖 badges,
"AI-assisted" notes, or any other tool signature to a commit message, PR
description, issue comment, review comment, or code comment. The author is the
person who decided the change should exist.

### Commit subjects

Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`),
lowercase after the type, no trailing period, no emoji.

Describe the behaviour that changed, not the mechanism. A `fix:` subject should
read as the symptom a user would have hit:

```
fix: watch check called systemd and pm2 targets clean without checking them
fix: don't blame the host key when the TOFU connection fails
feat: cap incident history so the watch directory stops growing forever
docs: lead the README with what homebutler actually prints
```

Not `fix: update watch.go`, and not `fix: add nil check in checkTarget`.

### Commit bodies

Only when the subject cannot carry it. Explain why the change is correct or what
breaks without it — the diff already shows what changed. A one-line commit with
a good subject is finished, not lazy.

Note user-visible behaviour changes explicitly. Someone upgrading needs to know
that settings which were silently inert will now take effect.

### PR descriptions and comments

Lead with what changed and why it matters. Keep the implementation notes to the
decisions a reviewer could not infer from the diff — what you reused, what you
deliberately left alone, what fails fast and where.

Do not restate the diff as a bullet list, do not add a summary table, and do not
open with "This PR". Skip "comprehensive", "robust", "successfully", and
"seamlessly". If a section would be true of every PR in the repository, cut it.

### CHANGELOG entries

Each release opens with one bolded sentence naming the problem the release
solves, then the commands it introduces, then the grouped sections. Keep
`⚠️ Behavior changes` honest and specific — that section is the reason someone
reads a changelog before upgrading.
