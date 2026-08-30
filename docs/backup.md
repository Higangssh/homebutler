# Backup & Restore

Back up all your Docker service volumes, compose files, and environment variables in one command.

## Quick Start

```bash
homebutler backup                          # backup everything
homebutler backup --service jellyfin       # backup a specific service
homebutler backup --to /mnt/nas/backups/   # custom destination
homebutler backup list                     # list existing backups
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

The path is checked after symlinks are resolved, and it is checked again for
each mount just before that mount is written. Both matter: an archive can
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
```

Default location: `~/.homebutler/backups/`

## Scheduled Backups

Combine with cron for automated backups:

```bash
# Every day at 3 AM
0 3 * * * homebutler backup --to /mnt/nas/backups/

# Weekly on Sunday at 2 AM, keep only the latest
0 2 * * 0 homebutler backup --to /mnt/nas/weekly/
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
