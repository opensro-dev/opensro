# syntax=docker/dockerfile:1
#
# server.Dockerfile - the OpenSRO server binaries for the Docker deploy
#
# One image for every server role; server-entrypoint.sh picks the binary.
# Build context: the repository root (server.Dockerfile.dockerignore lets
# only apps/server through). The game data is a second context,
#   --build-context gamedata=<.../apps/server/.generated/game-data/1.150>
# because it lives in the main checkout's ignored tree, never in git.
#
# The compiler runs on the builder's own platform and cross-compiles, so
# an arm64 Mac builds amd64 and arm64 without emulation.

FROM --platform=$BUILDPLATFORM golang:1.27.4 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/apps/server
COPY apps/server/go.mod apps/server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY apps/server/cmd ./cmd
COPY apps/server/internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for command in services/sro-agent services/sro-gameworld tools/sro-authority-upgrade \
                   operations/sro-provision-identity operations/sro-init operations/hash-agent-password; do \
        CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
            -o /out/bin/$(basename $command) ./cmd/$command || exit 1; \
    done

FROM debian:stable-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 10001 sro && useradd -u 10001 -g sro -d /app -s /usr/sbin/nologin sro \
 && mkdir -p /cluster /data/agent /data/shard /cache/game-data \
 && chown sro:sro /cluster /data/agent /data/shard /cache/game-data
WORKDIR /app
COPY --from=build /out/bin/ /app/bin/
# The binaries read config/ relative to the working directory
# (mission-chat.json) and the catalogue files beside it.
COPY apps/server/config/compatibility.json apps/server/config/shard-settings.json apps/server/config/mission-chat.json /app/config/
COPY --from=gamedata server.srogz /app/game-data/1.150/server.srogz
COPY deploy/docker/server-entrypoint.sh /app/bin/server-entrypoint.sh
ENV PATH=/app/bin:$PATH \
    SRO_SERVER_GAME_DATA_ROOT=/app/game-data/1.150/server.srogz \
    SRO_SERVER_GAME_DATA_CACHE_ROOT=/cache/game-data
USER 10001:10001
ENTRYPOINT ["/app/bin/server-entrypoint.sh"]
