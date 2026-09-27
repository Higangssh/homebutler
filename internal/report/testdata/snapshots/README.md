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
| `v0.22.0-docker-down` | Hand-made; see below |

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

## `v0.22.0-docker-down` is hand-made

`failed_collectors` is frozen, and none of the snapshots above ever makes a
report decode it, so this one was written by hand from the `v0.22.0` file to
look the way v0.22.0 writes a run with the Docker daemon down, following
that release's `buildSnapshot` and `inventory.Collect`:

- `containers` is `null` and both counts are `0`, because a failed listing
  leaves the list unset
- `failed_collectors` is `["docker"]`, and `warnings` holds the
  `docker: ...` line that goes with it
- the two port entries for 18081 are gone, since nginx is not listening
  with the daemon down, and `public_port_count` is `2`

It was serialised by a script rather than by Go, so its whitespace is not
byte for byte what v0.22.0 would write. What it keeps is the keys, their
order and the type of every value.
