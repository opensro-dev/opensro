#!/usr/bin/env bash
# build-images.sh - build the stack's three images: server, identity, web.
#
#   deploy/build-images.sh                      load <repo>:<role>-<tag> into the local Docker
#   deploy/build-images.sh --push               push them for PLATFORM (publish.sh)
#
# IMAGE_REPO and IMAGE_TAG name the images (default silkroad-local and test,
# the names deploy/test/smoke.sh runs). The client is always rebuilt first:
# the web image copies its dist, and a stale dist must never ship.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"

IMAGE_REPO="${IMAGE_REPO:-silkroad-local}"
IMAGE_TAG="${IMAGE_TAG:-test}"
mode=(--load)
if [ "${1:-}" = "--push" ]; then
    : "${PLATFORM:?--push needs PLATFORM (for example linux/amd64,linux/arm64)}"
    mode=(--push --provenance=false --platform "$PLATFORM")
    if [ -n "${BUILDX_BUILDER:-}" ]; then mode+=(--builder "$BUILDX_BUILDER"); fi
fi

game_data="$(generated_dir game-data)"
if [ ! -f "$game_data/server.srogz" ]; then
    echo "no $game_data/server.srogz: run \`pnpm task build server-game-data\` in the main checkout" >&2
    exit 1
fi

echo "==> client build" >&2
(cd "$REPO_ROOT" && pnpm --filter @sro/client-next build) >&2

for role in server identity web; do
    echo "==> $IMAGE_REPO:$role-$IMAGE_TAG" >&2
    contexts=()
    case "$role" in
        server) contexts=(--build-context "gamedata=$game_data") ;;
        web) contexts=(--build-context "dist=$REPO_ROOT/apps/client-next/temp/artifacts/dist") ;;
    esac
    docker buildx build "${mode[@]}" \
        --file "$DEPLOY_DIR/docker/$role.Dockerfile" \
        "${contexts[@]}" \
        --tag "$IMAGE_REPO:$role-$IMAGE_TAG" \
        "$REPO_ROOT" >&2
done
