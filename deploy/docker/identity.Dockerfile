# syntax=docker/dockerfile:1
#
# identity.Dockerfile - the workload identity service (deploy/identity)
#
# Build context: the repository root. The module imports the upstream
# server's internal packages through its replace directive, so apps/server
# comes along (identity.Dockerfile.dockerignore).

FROM --platform=$BUILDPLATFORM golang:1.27.4 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY apps/server/go.mod apps/server/go.sum apps/server/
COPY deploy/identity/go.mod deploy/identity/go.sum deploy/identity/
RUN --mount=type=cache,target=/go/pkg/mod cd deploy/identity && go mod download
COPY apps/server/internal apps/server/internal
COPY deploy/identity deploy/identity
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    cd deploy/identity && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -o /rootfs/identity . \
 && mkdir -p /rootfs/identity-state /rootfs/out

# Every stack image runs as 10001, the owner of the shared volumes
# (cluster is written by the server image and read here). The mount
# points exist in the image so new named volumes inherit that owner.
FROM gcr.io/distroless/static-debian12
COPY --from=build --chown=10001:10001 /rootfs/identity /identity-bin
COPY --from=build --chown=10001:10001 /rootfs/identity-state /identity
COPY --from=build --chown=10001:10001 /rootfs/out /out
COPY --from=build --chown=10001:10001 /rootfs/out /cluster
USER 10001:10001
ENTRYPOINT ["/identity-bin"]
