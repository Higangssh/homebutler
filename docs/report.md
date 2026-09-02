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

`--json` never groups and never truncates. A person needs the section to stay
readable; anything reading the output needs all of it, and quietly handing a
shortened list to an agent is how an agent becomes confidently wrong.

## Order

`Needs Attention`, then `Notable Changes`, then `Current Status`, then
`Suggested Actions`. Current status is context for the changes above it rather
than the headline — it is the reading every other tool already shows.
