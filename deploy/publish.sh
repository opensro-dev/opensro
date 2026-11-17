#!/usr/bin/env bash
# publish.sh - build the stack's images here and push them to the private registry.
#
#   deploy/publish.sh                  prints the pushed tag on stdout; everything else on stderr
#
# Settings (environment, then deploy/deploy.env):
#   IMAGE_REPO   one repository for every image: <repo>:server-<tag>, identity-<tag>, web-<tag>, admin-<tag>.
#                Must be private: the images carry data derived from the retail client.
#                ghcr.io creates a new package private on its first push.
#   PLATFORM     default linux/amd64,linux/arm64.
#   IMAGE_TAG    default the commit (12 characters), plus -dirty-<UTC time> when the
#                images' sources have uncommitted changes.
#
# Before pushing it builds the images for this machine and runs
# deploy/test/smoke.sh against them: a merge from upstream that broke a
# contract the deploy relies on stops here, not on the server.
# SKIP_SMOKE=1 skips that (with a warning); PUBLISH_DRY_RUN=1 stops before
# the push and prints the tag it would have used.
#
# Needs `docker login ghcr.io` with a token that has write:packages.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"
load_settings IMAGE_REPO PLATFORM

: "${IMAGE_REPO:?set IMAGE_REPO=ghcr.io/<github user>/silkroad (deploy/deploy.env)}"
PLATFORM="${PLATFORM:-linux/amd64,linux/arm64}"

# What the images build from: a change there makes a dirty tag.
SOURCES=(
    apps/server/cmd apps/server/internal apps/server/config apps/server/go.mod apps/server/go.sum
    apps/client-next pnpm-lock.yaml deploy/docker deploy/identity deploy/admin
)
if [ -z "${IMAGE_TAG:-}" ]; then
    IMAGE_TAG="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)"
    if [ -n "$(git -C "$REPO_ROOT" status --porcelain -- "${SOURCES[@]}")" ]; then
        IMAGE_TAG="$IMAGE_TAG-dirty-$(date -u +%Y%m%dT%H%M%SZ)"
        echo "warning: uncommitted changes in the images' sources; tagging $IMAGE_TAG" >&2
    fi
fi
case "$IMAGE_TAG" in
    *[!A-Za-z0-9_.-]*|"") echo "invalid IMAGE_TAG '$IMAGE_TAG'" >&2; exit 1 ;;
esac

#
# visibility
#
# What an anonymous client sees of IMAGE_REPO: public, private-or-missing, or
# unchecked for a registry other than ghcr.io and Docker Hub.
#
visibility() {
    local registry="${IMAGE_REPO%%/*}" path code token
    case "$registry" in
        *.*|*:*|localhost) path="${IMAGE_REPO#*/}" ;;
        *) registry=docker.io; path="$IMAGE_REPO" ;;
    esac
    case "$registry" in
        docker.io)
            code="$(curl -s -o /dev/null -w '%{http_code}' "https://hub.docker.com/v2/repositories/$path/" || true)" ;;
        ghcr.io)
            token="$(curl -s "https://ghcr.io/token?scope=repository:$path:pull" | sed -nE 's/.*"token":"([^"]+)".*/\1/p' || true)"
            if [ -z "$token" ]; then echo private-or-missing; return; fi
            code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "https://ghcr.io/v2/$path/tags/list" || true)" ;;
        *) echo unchecked; return ;;
    esac
    case "$code" in
        200) echo public ;;
        401|403|404) echo private-or-missing ;;
        *) echo "unknown ($code)" ;;
    esac
}

if [ "${PUBLISH_DRY_RUN:-}" != 1 ] && [ "$(visibility)" = public ]; then
    echo "refusing: $IMAGE_REPO is public, and the images carry game data. Make it private and run again." >&2
    exit 1
fi

if [ "${SKIP_SMOKE:-}" = 1 ]; then
    echo "warning: SKIP_SMOKE=1, the images are pushed untested" >&2
else
    echo "==> local images and the smoke test" >&2
    IMAGE_REPO=silkroad-local IMAGE_TAG=test "$DEPLOY_DIR/build-images.sh" >&2
    "$DEPLOY_DIR/test/smoke.sh" >&2
fi

if [ "${PUBLISH_DRY_RUN:-}" = 1 ]; then
    echo "dry run: would push $IMAGE_REPO:<role>-$IMAGE_TAG for $PLATFORM" >&2
    echo "$IMAGE_TAG"
    exit 0
fi

# The docker driver builds one platform at a time unless Docker uses the
# containerd image store; a docker-container builder builds and pushes the
# multi-architecture tag in one go.
# Captured first: with pipefail, `grep -q` closing the pipe early would fail the inspect.
current_builder="$(docker buildx inspect 2>/dev/null || true)"
if [ -z "${BUILDX_BUILDER:-}" ] && grep -Eq '^Driver: +docker$' <<<"$current_builder"; then
    if ! docker buildx inspect silkroad-publish >/dev/null 2>&1; then
        echo "==> creating the buildx builder silkroad-publish (docker-container)" >&2
        docker buildx create --name silkroad-publish --driver docker-container >/dev/null
    fi
    export BUILDX_BUILDER=silkroad-publish
fi

echo "==> push $IMAGE_REPO:<role>-$IMAGE_TAG for $PLATFORM" >&2
IMAGE_REPO="$IMAGE_REPO" IMAGE_TAG="$IMAGE_TAG" PLATFORM="$PLATFORM" "$DEPLOY_DIR/build-images.sh" --push >&2

if [ "$(visibility)" = public ]; then
    echo "error: $IMAGE_REPO is PUBLIC after the push. Make it private now." >&2
    exit 1
fi
echo "==> pushed $IMAGE_REPO:<role>-$IMAGE_TAG" >&2
echo "$IMAGE_TAG"
