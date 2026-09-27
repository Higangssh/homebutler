# Snapshots older releases wrote

One file per release, each written by that release's own binary. `report`
compares against the newest file in `~/.homebutler/reports/snapshots/`, so
somebody upgrading from any of these versions has one of them on disk, and
the next report has to read it.

| Directory | Why this release |
| --- | --- |
| `v0.18.0` | The first release with `report` |
| `v0.22.0` | Added `failed_collectors` (#74) |
| `v0.26.0` | Added `processes` (#129) |
| `v0.39.0` | The last release before 1.0 |

## How they were made

On 2026-09-27 each tag was built for linux/arm64 and run on a Raspberry Pi
as `report --json`, with `HOME` pointed at an empty directory so that each
binary wrote a first snapshot of its own. Three containers were running for
it: `hb-fixture-a` (alpine, running), `hb-fixture-b` (nginx:alpine, running,
publishing 18081) and `hb-fixture-c` (alpine, stopped). The containers and
the pulled image were removed afterwards.

Three things were changed afterwards, by text substitution on the file so
the rest is byte for byte what the release wrote:

- the host's name is `homelab-server`
- `system.time` is in UTC, not in the host's time zone
- each process `hash` is replaced by a hash of the original, since the
  original is derived from a real command line

The shape of each file, including every key and the type of every value,
was checked to be the same before and after.

`failed_collectors` does not appear in any of them, because nothing failed
on that machine. The key is `omitempty`, so a release that has the field
writes it only when a collector fails.
