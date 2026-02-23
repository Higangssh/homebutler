# Reddit Post Drafts

---

## r/selfhosted (Primary Target)

**Title:** homebutler — single-binary CLI for homelab management, works with chat/AI or standalone

**Body:**

I run a couple of machines at home and wanted a quick way to check on them without opening SSH or a dashboard every time. So I wrote a small Go CLI that does the basics:

- System status (CPU, memory, disk, uptime)
- Docker list/restart/stop/logs
- Wake-on-LAN
- Open ports with process info
- LAN device discovery
- Resource alerts with configurable thresholds

Everything outputs JSON, so you can pipe it into jq, use it in scripts, or hook it up to an AI assistant. I use it with [OpenClaw](https://github.com/openclaw/openclaw) on Telegram — ask a question, get a plain-text answer — but it works fine on its own.

Single binary, ~3MB, no daemon, no web UI. Cross-compiled for linux/darwin (amd64/arm64). Works on a Raspberry Pi.

GitHub: https://github.com/Higangssh/homebutler

Still early (v0.1.0). Thinking about adding multi-server SSH support next. Open to suggestions.

---

## r/homelab

**Title:** homebutler — a small CLI for checking on your homelab

**Body:**

Small Go CLI I made for my homelab. Checks system status, manages Docker containers, does WOL, scans ports and network devices. JSON output so it plays nice with scripts or AI assistants.

Single binary, ~3MB, runs on a Pi. No web server, no daemon.

GitHub: https://github.com/Higangssh/homebutler

Still early — feedback welcome.

---

## r/golang

**Title:** homebutler — a zero-dependency Go CLI for homelab management (single binary, JSON output)

**Body:**

I just released v0.1.0 of homebutler, a CLI tool for managing homelab servers.

**Design decisions I'd love feedback on:**

- **No frameworks** — I skipped Cobra and wrote the CLI router manually to keep dependencies minimal. The entire binary is ~3MB with zero CGO. Good idea or should I just use Cobra?

- **JSON-first output** — Every command outputs JSON by default, designed to be parsed by scripts or AI assistants. Is this the right default, or should human-readable be default with `--json` flag?

- **`internal/` package structure** — system, docker, wake, network, ports, alerts, config each in their own package under internal/. Feels clean but maybe over-separated for a small project?

- **Security** — Container names are validated with allowlist (alphanumeric + hyphen/underscore/dot only) to prevent command injection, even though we use exec.Command directly. Belt and suspenders approach.

Cross-compiled for linux/darwin (amd64/arm64) using goreleaser. Tests cover all internal packages.

GitHub: https://github.com/Higangssh/homebutler

Feedback welcome — especially on the architecture choices.

---

## Hacker News

**Title:** Show HN: Homebutler – Manage your homelab from chat with a single Go binary

**URL:** https://github.com/Higangssh/homebutler

---

## Posting Schedule

1. **r/selfhosted** — Post first (biggest impact, most aligned audience)
2. **r/homelab** — Post 2-3 days later
3. **r/golang** — Post 1 week later (different angle: technical feedback)
4. **Hacker News** — Post when ready (timing: US East 8-10 AM = KST 10 PM-midnight)

## Tips
- Don't post to all subreddits on the same day (looks spammy)
- Reply to every comment (engagement boosts visibility)
- Never say "please star" — let the project speak for itself
- If a post flops, it's OK to try again in 2-3 weeks with a different angle
