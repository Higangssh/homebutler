# The image is the hub, not the probe.
#
# A container cannot see its host: `status` would read the container's own
# /proc, `ports` its own sockets. So this runs `serve` and reaches the machines
# in servers: over SSH, which is the path multi-server already uses and needs
# nothing installed on the machines being watched. A `local: true` server
# configured in here is reported as not visible rather than answered with the
# container's numbers.
FROM alpine:3.21

RUN apk add --no-cache ca-certificates openssh-client tzdata

# The binary comes from the release build, which has the dashboard assets
# embedded. Building from source here would produce an image whose web
# dashboard is an empty directory.
COPY homebutler /usr/local/bin/homebutler

# Config is mounted read-write: the settings screen writes to it. State —
# snapshots and incidents — belongs on a volume, or a restart turns "what
# changed" into "everything is new".
ENV HOMEBUTLER_CONFIG=/config/config.yaml
VOLUME ["/config", "/root/.homebutler"]

EXPOSE 8080

ENTRYPOINT ["homebutler"]
# 0.0.0.0 because the point of the image is to be reached from another machine.
# That is also why serve refuses to run without --token when it is bound
# beyond localhost.
CMD ["serve", "--host", "0.0.0.0", "--port", "8080"]
