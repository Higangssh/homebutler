# Backup & Restore

Back up all your Docker service volumes, compose files, and environment
variables in one command — and then prove the result comes back.

## Quick Start

```bash
homebutler backup                          # backup everything
homebutler backup --service jellyfin       # backup a specific service
homebutler backup --to /mnt/nas/backups/   # custom destination
homebutler backup list                     # list existing backups
homebutler backup drill uptime-kuma        # boot the backup and check it answers
```

**Restore from a backup:**

```bash
homebutler restore ./backup_2026-03-11_1830.tar.gz                    # restore all
homebutler restore ./backup_2026-03-11_1830.tar.gz --service postgres  # restore one service
```

## How It Works

When you run `homebutler backup`, here's what happens step by step:

### Step 1: Discover Docker services

```
docker compose ls
```

homebutler finds all running Docker Compose projects and their compose file locations.

### Step 2: Inspect container mounts

```
docker inspect <container> --format '{{json .Mounts}}'
```

For each container, homebutler identifies all attached volumes — both **named volumes** (managed by Docker) and **bind mounts** (host directories).

### Step 3: Back up volumes

**Named volumes** can't be accessed directly from the host. homebutler uses the [official Docker pattern](https://docs.docker.com/engine/storage/volumes/#back-up-restore-or-migrate-data-volumes) — spinning up a temporary Alpine container that mounts the volume read-only and creates a tar archive:

```
docker run --rm \
  -v my_volume:/source:ro \
  -v /backup/path:/backup \
  alpine tar czf /backup/my_volume.tar.gz -C /source .
```

The temporary container is removed immediately after (`--rm`). The volume is mounted read-only (`:ro`), so **your data is never modified**.

**Bind mounts** are backed up directly from the host filesystem using `tar`.

### Step 4: Copy compose files

The `docker-compose.yml` and `.env` files are copied into the archive. These are essential for restoring your services.

### Step 5: Generate manifest

A `manifest.json` is created with full metadata:

```json
{
  "version": "1",
  "created_at": "2026-03-11T18:30:00+09:00",
  "services": [
    {
      "name": "postgres",
      "container": "a1b2c3d4...",
      "image": "postgres:16",
      "mounts": [
        {
          "type": "volume",
          "name": "postgres_data",
          "source": "/var/lib/docker/volumes/postgres_data/_data",
          "destination": "/var/lib/postgresql/data"
        }
      ]
    }
  ]
}
```

### Step 6: Create archive

Everything is bundled into a single `.tar.gz`:

```
backup_2026-03-11_1830.tar.gz
├── manifest.json
├── compose/
│   ├── docker-compose.yml
│   └── .env
└── volumes/
    ├── postgres_data.tar.gz
    └── jellyfin_config.tar.gz
```

## Drill: proving the backup comes back

A backup you have never restored is a folder. `backup drill` is the command
that turns it into a backup: it takes an archive and boots the app from it, on
a network and a port of its own, and requires the app to answer over HTTP
before it will say the archive is good.

```bash
homebutler backup drill uptime-kuma        # the latest archive
homebutler backup drill --all              # every app the archive holds
homebutler backup drill --archive ./old.tar.gz uptime-kuma
homebutler backup drill uptime-kuma --json
```

```
🔍 Backup Drill — uptime-kuma

  📦 Backup: /Users/you/.homebutler/backups/backup_2026-04-04_1711.tar.gz
  📏 Size: 18.6 MB
  🔐 Integrity: ✅ tar valid (8 files)

  🚀 Boot: ✅ container started in 0s
  🌐 Health: ✅ HTTP 200 on port 60405
  ⏱️  Total: 2s

  ✅ DRILL PASSED
```

### What it actually does

1. Picks the latest archive in the backup directory, or the one `--archive` names
2. Checks the archive is readable as a `tar` and counts what is inside
3. Creates a Docker network and picks a free port, both used by this drill only
4. Unpacks the app's volumes into fresh volumes and starts the app on them
5. Waits for an HTTP health check to answer
6. Removes the container, the network and the volumes it made

Step 6 runs whether the drill passed or failed. Nothing it creates outlives it,
and it never touches the running copy of the app: the drill boots a second copy
beside it, on its own port, from the archive's data.

### A failed drill exits non-zero

The verdict leaves through the exit code as well as the screen, so the command
is usable from cron or CI without parsing anything:

```
🚀 Boot: ❌ container failed to start
⏱️  Total: 0s

❌ DRILL FAILED
💡 Run: homebutler backup --service vaultwarden
error: vaultwarden did not come back from the backup
```

```bash
homebutler backup drill --all || notify-send "a backup did not come back"
```

With `--all`, any single app failing makes the run fail — `--all` is not a
vote. `--json` still prints the full result to stdout, so the exit code and the
report are both available:

```json
{
  "app": "uptime-kuma",
  "archive": "/Users/you/.homebutler/backups/backup_2026-04-04_1711.tar.gz",
  "size": "18.6 MB",
  "file_count": 8,
  "integrity": true,
  "booted": true,
  "boot_seconds": 0,
  "health_status": 200,
  "health_port": "60405",
  "passed": true,
  "total_seconds": 2
}
```

### What a drill does not prove

It proves the archive unpacks and the app starts on that data and answers a
health check. It does not read your data back: an app that boots with an empty
database answers its health check too. Treat a passing drill as "this archive
is not corrupt and this app runs on it", which is the part that silently stops
being true, and not as "every row is there".

### The drill leaves a record, and `doctor` reads it

Each drill writes a small record — the app, the archive, the verdict, and when
— into `.drills/` beside the backups, capped at the most recent 50 the way
`watch` caps incidents. `backup drill --json` carries the same timestamp as
`drilled_at`.

`doctor` asks two questions of it, both as warnings rather than failures,
because never having drilled is where every install starts:

- **No backup has ever been drilled.** Archives exist and none has been
  restored to see whether it comes back.
- **The newest backup has never been drilled.** There is a drill, and it is
  older than the newest archive — so what passed is not what you would restore
  from. This is the sharper of the two.

A failed drill is recorded too, and `doctor` says so: the archive you would
have restored from is the one that did not come back.

## Restore

When you run `homebutler restore ./backup.tar.gz`:

1. Extracts the archive to a temp directory
2. Reads `manifest.json` to understand what was backed up
3. Restores **named volumes** using the reverse pattern (alpine container + `tar xzf`)
4. Restores **bind mounts** to host paths you named with `--allow-bind`
5. Restores compose files to their original location

Use `--service <name>` to restore only a specific service.

### Bind mounts are refused unless you name the path

`manifest.json` lives inside the archive, so every path it declares is chosen
by whoever built that archive. For a backup you created yourself that is the
path you expect; for a backup someone sent you it is whatever they decided.

Restore therefore treats manifest paths as a request, not an instruction. Named
volumes must look like volume names, and a bind mount is only restored to a
path you passed on the command line:

```bash
homebutler restore ./backup.tar.gz                       # bind mounts refused, and listed
homebutler restore ./backup.tar.gz --allow-bind /srv/app # restores under /srv/app only
```

Member names inside the archive get the same treatment. A member that is an
absolute path, that climbs out with `..`, or that is written through a symlink
an earlier member of the same archive planted, fails the restore rather than
being skipped. `tar` declines most of those on its own; homebutler no longer
depends on it doing so.

The bind target is checked after symlinks are resolved, and it is checked again
for each mount just before that mount is written. Both matter: an archive can
declare two bind mounts, restore the first one normally inside the permitted
root so its payload plants a symlink there, and then name that symlink as the
second one's source. It reads as inside the root, and it is not.

Refusals are printed, and appear in `--json` under `refused`, so a restore that
did less than you expected says why rather than reporting fewer volumes.

Over MCP there is no way for an agent to name an allowed path, so
`backup_restore` never restores bind mounts.

## Configuration

Set a custom backup directory in your `homebutler.yml`:

```yaml
backup:
  dir: /mnt/nas/backups/homebutler
  retention:
    max_archives: 7      # keep the 7 newest
    max_bytes: 20GB      # and no more than 20GB in total
```

Default location: `~/.homebutler/backups/`

`backup_dir:` at the top level is the older spelling and still works. A file
carrying both gets `backup.dir`.

### Nothing is deleted unless you ask

Both retention limits are off by default, which is the opposite of every other
store homebutler keeps. Incidents and snapshots are bounded out of the box
because a pruned incident costs some history. A pruned backup can be the only
remaining copy of data that no longer exists anywhere else, and that is not a
default anyone chose.

So retention is opt-in, and two things follow from that:

- Pruning runs after a backup is written, never before and never after a failed
  one, and it names each archive it removed. Deleting a backup is not a detail.
- The newest archive is never removed, whatever the limits say — including when
  it exceeds `max_bytes` on its own. A limit that empties the directory has
  misunderstood what it was asked to bound.

Because the default never deletes, `homebutler doctor` says when the directory
has grown large and has no limit on it. That warning is how the safe default
stays safe.

## Scheduled Backups

Combine with cron for automated backups:

```bash
# Every day at 3 AM
0 3 * * * homebutler backup --to /mnt/nas/backups/

# Weekly on Sunday at 2 AM, keep only the latest
0 2 * * 0 homebutler backup --to /mnt/nas/weekly/
```

A backup on a schedule and nothing checking it is the arrangement that fails
quietly, so drill on a schedule too. A failed drill exits non-zero, which is
what makes the `||` below fire:

```bash
# Back up at 3, then prove it comes back
0 3 * * * homebutler backup --to /mnt/nas/backups/
30 3 * * * homebutler backup drill --all || homebutler notify test
```

## JSON Output

```bash
homebutler backup --json
```

```json
{
  "archive": "/home/user/.homebutler/backups/backup_2026-03-11_1830.tar.gz",
  "services": ["postgres", "jellyfin", "pihole"],
  "volumes": 5,
  "size": "2.5 GB"
}
```

## ⚠️ Important Notes

### Database consistency

homebutler copies volume files as-is. It does **not** automatically stop or pause containers during backup. For most services (config files, media libraries, etc.) this is perfectly fine.

However, **database services** (PostgreSQL, MySQL, MongoDB, etc.) may be writing data during the backup, which can result in an inconsistent snapshot.

**Recommended approach for databases:**

```bash
# Option A: Pause the container (brief freeze, no downtime)
docker pause postgres
homebutler backup --service postgres
docker unpause postgres

# Option B: Use native database dump (most reliable)
docker exec postgres pg_dump -U myuser mydb > dump.sql
homebutler backup  # backs up everything else
```

### Security

- Backup archives are **not encrypted**. They may contain sensitive data (database contents, environment variables with passwords, API keys).
- Store backups in a secure location with appropriate file permissions.
- Consider encrypting with `gpg` for offsite storage:

```bash
homebutler backup --to /tmp/
gpg --symmetric /tmp/backup_2026-03-11_1830.tar.gz
```

### What is NOT backed up

- Docker images (they can be re-pulled with `docker compose pull`)
- Container logs
- Docker networks (they are recreated by `docker compose up`)
- System-level configs outside of Docker
