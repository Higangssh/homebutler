# Running homebutler in a container

The image is the hub, not the probe.

A container cannot see the machine it runs on: `status` would read the
container's own `/proc`, `ports` its own sockets. So the image runs `serve` and
reaches the machines in `servers:` over SSH — the path multi-server already
uses. A server marked `local: true` is reported as not visible from a container
rather than answered with the container's numbers.

**homebutler has to be on the machines it reaches.** A remote command runs the
remote binary: `report --server pi` runs `homebutler report` on the Pi. Nothing
runs there between commands — no daemon, no open port — but the binary has to
be installed, and it has to be new enough for the command being asked for. Put
it there with `homebutler deploy` from a machine that has your key:

```bash
homebutler deploy --server pi     # or --all
```

An older binary on the far side fails with `unknown command`, which is what an
upgrade fixes:

```bash
homebutler upgrade                # local, then every server in servers:
```

```bash
docker run -d --name homebutler -p 8080:8080 \
  -v ~/.config/homebutler:/config \
  -v homebutler-state:/root/.homebutler \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/higangssh/homebutler:latest \
  serve --host 0.0.0.0 --port 8080 --token "$(openssl rand -hex 16)"
```

Or as a compose block to paste beside the rest:

```yaml
services:
  homebutler:
    image: ghcr.io/higangssh/homebutler:latest
    container_name: homebutler
    command: serve --host 0.0.0.0 --port 8080 --token ${HOMEBUTLER_TOKEN}
    ports:
      - "8080:8080"
    volumes:
      - ./config:/config                 # config.yaml, written by the settings screen
      - homebutler-state:/root/.homebutler  # snapshots and incidents
      - ~/.ssh:/root/.ssh:ro             # key and known_hosts, read-only
    restart: unless-stopped

volumes:
  homebutler-state:
```

## What each mount is for

| Mount | Why |
| --- | --- |
| `/config` | `config.yaml`. Read-write, because the settings screen writes to it |
| `/root/.homebutler` | Snapshots and watch incidents. On a volume, or a restart turns "what changed" into "everything is new" |
| `/root/.ssh` | The key that reaches your servers, and `known_hosts`. Read-only: nothing in here should be adding to your trusted hosts |

## The token is not optional here

`serve` refuses to bind beyond `127.0.0.1` from a container without `--token`.
The image exists to be reached from another machine, which is exactly when an
unauthenticated dashboard is wrong. The token still crosses the network in the
clear, so put a reverse proxy with TLS or a tunnel in front of it.

## Trusting a server the first time

`/root/.ssh` is mounted read-only, so a first connection to a host that is not
in `known_hosts` fails rather than trusting it silently. That is deliberate:

- **Key authentication** would otherwise record the host key on first sight, and
  a read-only mount cannot.
- **Password authentication** refuses regardless, since 0.34.0 — homebutler does
  not send a password to a host it has not been told to trust.

Trust it where the key lives, then restart the container:

```bash
homebutler trust pi          # on the host, with ~/.ssh writable
docker restart homebutler
```

## What the container cannot do

- **Watch.** `watch` supervises containers on the machine it runs on, which in
  here is the container itself.
- **Report on the host.** The host is a machine like any other: add it to
  `servers:` and reach it over SSH, or run the binary on it directly.
- **See the host's Docker.** Mounting the socket would hand the container
  control of every container on the machine, which is not a default anyone
  should copy from a README.
