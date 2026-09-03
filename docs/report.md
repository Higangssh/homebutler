# report

`homebutler report` compares the server against the last time it looked and
says what moved. It writes a snapshot on every run, keeps the last few, and
diffs the current one against the previous.

```bash
homebutler report            # human-readable
homebutler report --json     # complete, ungrouped
homebutler report --no-save  # look without writing a snapshot
```

## What earns a line

Changes are compared by identity, not by count. Six containers before and six
after is not "no change" when one of them is a different container.

| Kind | Means |
| --- | --- |
| `gone` | a container or listener that was there and is not |
| `new` | one that is there and was not |
| `replaced` | the same name, a different container underneath |
| `image` | the same container, a different image |
| `state` | the same container, running where it was stopped or the reverse |
| `port` | the same port, a different process answering on it |
| `disk` | a mount whose usage moved by more than half a gigabyte |

Processes are compared the same way. A process is identified by its executable
name and a hash of its full invocation, so `python3 /opt/old.py` becoming
`python3 /opt/new.py` is a `replaced`, not silence.

`replaced` is reserved for the case where it is true: one invocation under a
name before, one after, and they differ. Names like `python3`, `bash` and `ssh`
routinely run several at once, and one of them exiting is reported as a
departure under a name that is still running — nothing was replaced.

`replaced` is the one worth knowing about. A container recreated under the same
name leaves every count identical, so a report that compares counts says
nothing at all — which is what this one did before 0.26.0.

## What is deliberately left out

A report nobody reads is worse than no report. These are excluded on purpose:

- **Uptime and status strings.** `Up 4 hours` becomes `Up 9 hours` between any
  two runs. Comparing it would put a line in every report forever.
- **CPU and memory.** They move on every sample. A single-sample delta is
  noise, not a change; a real signal needs a threshold and a duration behind
  it, which is a separate feature rather than a diff.
- **Restart counters that moved without a state change.** A container that
  restarted and came back is in the same state it was in.
- **Processes that have not been running for a minute.** Measured rather than
  guessed: two runs thirty seconds apart on an idle machine reported `new head`
  and `new sed` — the shell pipeline that was reading the report. Cron jobs and
  package managers arrive the same way. A process held back is not lost; it is
  reported on the next run, by which time it has earned the word "new".
- **The command line itself.** A snapshot keeps the executable name and a
  twelve-character hash of the invocation, never the arguments. Command lines
  carry secrets in flags, and `~/.homebutler/reports/snapshots/` has never had
  to be handled as a credential store.
- **Anything from a collector that did not answer.** If Docker was down when
  either snapshot was taken, container changes are not compared at all and the
  report says so. Reporting every container as gone because the daemon was
  restarting would be worse than reporting nothing.

## Grouping and truncation are human-only

More than five changes of one kind collapse into a single line that names three
and counts the rest:

```
state     7 containers  postgres, redis, grafana, +4
```

Changes that render identically are dropped, in both outputs. Docker publishes
a port on both address families, so one container starting produces two
listeners by identity and one sentence by display — one port opening is one
event. That is not the grouping rule: grouping collapses *different* changes
into a summary and is human-only, while an exact duplicate carries nothing for
anything reading the output.

`--json` never groups and never truncates. A person needs the section to stay
readable; anything reading the output needs all of it, and quietly handing a
shortened list to an agent is how an agent becomes confidently wrong.

## Snapshot size

Tracking processes made snapshots larger, and the number is worth knowing
before turning `--keep` up: on a desktop macOS with 639 distinct process
identities a snapshot is about 50 KB, against 2 KB before. A Linux server
running a handful of services is well under that. At the default `--keep 30`
that is roughly 1.5 MB of history in the worst case measured.

## Order

`Needs Attention`, then `Notable Changes`, then `Current Status`, then
`Suggested Actions`. Current status is context for the changes above it rather
than the headline — it is the reading every other tool already shows.
