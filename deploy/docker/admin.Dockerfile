# syntax=docker/dockerfile:1
#
# admin.Dockerfile - the admin panel and its account relay (deploy/admin)
#
# Build context: the repository root (admin.Dockerfile.dockerignore lets
# only deploy/admin through). The module uses the standard library alone.

FROM --platform=$BUILDPLATFORM golang:1.27.4 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/deploy/admin
COPY deploy/admin ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /rootfs/admin . \
 && mkdir -p /rootfs/secrets

# 10001 owns the stack's volumes; the panel reads admin-secrets, which the
# server image's provision job writes.
FROM gcr.io/distroless/static-debian12
COPY --from=build --chown=10001:10001 /rootfs/admin /admin-bin
COPY --from=build --chown=10001:10001 /rootfs/secrets /admin
USER 10001:10001
ENTRYPOINT ["/admin-bin"]
