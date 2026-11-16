#!/usr/bin/env bash
# rollout.sh - roll a published release out on the Docker host. Compiles nothing.
#
#   IMAGE_REPO=<repo> IMAGE_TAG=<tag> ASSETS_RELEASE=<id> ~/silkroad/rollout.sh
#   ~/silkroad/rollout.sh                    the release in release.env again
#
# deploy.sh runs it after publishing the images and syncing the assets. A
# rollback is the same call with the PREVIOUS_* values of release.env.
#
# Order, stopping at the first failure: check the folder, pull and check the
# image architecture, provision what is missing (never replacing anything),
# stop for the first account when there is none, stop the GameWorld and then
# the Agent so no SQLite is written while the backup copies it, back up every
# state volume, upgrade the shard authority, point assets/current at the
# release, start everything waiting for its health check, and only then
# rewrite release.env and prune old images and asset releases.
#
# ROLLOUT_SKIP_PULL=1 uses local images (deploy/test/rollout_test.sh).
set -euo pipefail

cd "$(dirname "$0")"

PROJECT="${COMPOSE_PROJECT_NAME:-silkroad}"
KEEP_BACKUPS=5
KEEP_RELEASES=2
STATE_VOLUMES=(cluster agent identity shard-global-official)

fail() { echo "rollout: $*" >&2; exit 1; }

if [ ! -f .env.prod ]; then
    fail "missing $PWD/.env.prod: cp .env.prod.example .env.prod and fill it in"
fi
if grep -q '=change-me' .env.prod; then
    fail "$PWD/.env.prod still has change-me values"
fi
grep -Eq '^SRO_DOMAIN=.+' .env.prod || fail "$PWD/.env.prod has no SRO_DOMAIN"

# The environment wins over release.env, which holds the release that is up.
previous_tag="" previous_release=""
if [ -f release.env ]; then
    previous_tag="$(sed -nE 's/^IMAGE_TAG=(.*)$/\1/p' release.env)"
    previous_release="$(sed -nE 's/^ASSETS_RELEASE=(.*)$/\1/p' release.env)"
    : "${IMAGE_REPO:=$(sed -nE 's/^IMAGE_REPO=(.*)$/\1/p' release.env)}"
    : "${IMAGE_TAG:=$previous_tag}"
    : "${ASSETS_RELEASE:=$previous_release}"
fi
[ -n "${IMAGE_REPO:-}" ] && [ -n "${IMAGE_TAG:-}" ] && [ -n "${ASSETS_RELEASE:-}" ] \
    || fail "nothing to roll out: set IMAGE_REPO, IMAGE_TAG and ASSETS_RELEASE (deploy.sh does), or keep them in release.env"
case "$IMAGE_REPO$IMAGE_TAG$ASSETS_RELEASE" in
    *[!A-Za-z0-9_./:-]*) fail "IMAGE_REPO, IMAGE_TAG or ASSETS_RELEASE has characters a reference cannot have" ;;
esac
[ -d "assets/releases/$ASSETS_RELEASE/assets" ] || fail "asset release $ASSETS_RELEASE was never synced to $PWD/assets/releases"

# The web container mounts the release itself, not the assets/current link:
# a bind mount resolves a link once, at creation, so a changed path is what
# makes compose recreate it.
export IMAGE_REPO IMAGE_TAG
export ASSETS_DIR="./assets/releases/$ASSETS_RELEASE"
compose() { docker compose -p "$PROJECT" -f docker-compose.prod.yml --env-file .env.prod "$@"; }

if ! err="$(docker info 2>&1 >/dev/null)"; then fail "cannot use Docker as $(whoami): $err"; fi
if ! err="$(docker network inspect traefik 2>&1 >/dev/null)"; then fail "docker network 'traefik' is not usable: $err"; fi

if [ "${ROLLOUT_SKIP_PULL:-}" != 1 ]; then
    echo "==> pull $IMAGE_REPO:<role>-$IMAGE_TAG"
    compose --profile jobs pull provision derive-public agent identity web \
        || fail "pull failed: is $IMAGE_TAG published, and did $(whoami) run 'docker login ${IMAGE_REPO%%/*}'?"
fi

# An image of another architecture only fails at its first exec: check before anything stops.
host_arch="$(docker info --format '{{.Architecture}}')"
case "$host_arch" in x86_64) host_arch=amd64 ;; aarch64) host_arch=arm64 ;; esac
for role in server identity web; do
    image_arch="$(docker image inspect --format '{{.Architecture}}' "$IMAGE_REPO:$role-$IMAGE_TAG")" \
        || fail "$IMAGE_REPO:$role-$IMAGE_TAG is not on this host"
    [ "$image_arch" = "$host_arch" ] || fail "$IMAGE_REPO:$role-$IMAGE_TAG is $image_arch, this host is $host_arch"
done

echo "==> provision"
compose run --rm provision
compose run --rm derive-public

if ! compose run --rm -T --no-deps --entrypoint test provision -e /cluster/accounts.json; then
    printf 'IMAGE_REPO=%s\nIMAGE_TAG=%s\n' "$IMAGE_REPO" "$IMAGE_TAG" > pending.env
    fail "no account yet: run deploy/accounts.sh --bootstrap <id> from your machine, then this rollout again"
fi

echo "==> stop the GameWorld, then the Agent"
compose stop gameworld-global-official
compose stop agent

mkdir -p backups
backup="backups/silkroad-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
echo "==> backup $backup"
mounts=()
for volume in "${STATE_VOLUMES[@]}"; do mounts+=(-v "${PROJECT}_$volume:/backup/$volume:ro"); done
docker run --rm "${mounts[@]}" alpine tar -C /backup -czf - . > "$backup.partial"
mv "$backup.partial" "$backup"
ls -1 backups/silkroad-*.tar.gz | sort -r | tail -n +$((KEEP_BACKUPS + 1)) | xargs -r rm -f --

echo "==> upgrade"
compose run --rm upgrade

echo "==> assets $ASSETS_RELEASE"
ln -sfn "releases/$ASSETS_RELEASE" assets/current.next
mv -fh assets/current.next assets/current 2>/dev/null || mv -fT assets/current.next assets/current

echo "==> start"
# identity first: the Agent joins its network namespace, and a recreated
# identity leaves the old Agent container without a network, so the Agent
# is always recreated after it.
compose up -d identity
compose up -d --wait --force-recreate --no-deps agent
compose up -d --wait gameworld-global-official web
compose ps

{
    printf 'IMAGE_REPO=%s\nIMAGE_TAG=%s\nASSETS_RELEASE=%s\n' "$IMAGE_REPO" "$IMAGE_TAG" "$ASSETS_RELEASE"
    if [ "$previous_tag" != "$IMAGE_TAG" ] || [ "$previous_release" != "$ASSETS_RELEASE" ]; then
        printf 'PREVIOUS_IMAGE_TAG=%s\nPREVIOUS_ASSETS_RELEASE=%s\n' "$previous_tag" "$previous_release"
    else
        sed -nE '/^PREVIOUS_/p' release.env
    fi
} > release.env.next
mv release.env.next release.env
rm -f pending.env
echo "==> up: $IMAGE_REPO:<role>-$IMAGE_TAG, assets $ASSETS_RELEASE"

keep_tag="$(sed -nE 's/^PREVIOUS_IMAGE_TAG=(.*)$/\1/p' release.env)"
keep_release="$(sed -nE 's/^PREVIOUS_ASSETS_RELEASE=(.*)$/\1/p' release.env)"

# This stack's images only: the tag that is up and the previous one stay.
docker image ls "$IMAGE_REPO" --format '{{.Tag}}' | while read -r tag; do
    case "$tag" in server-*|identity-*|web-*) ;; *) continue ;; esac
    case "${tag#*-}" in "$IMAGE_TAG"|"$keep_tag") continue ;; esac
    docker image rm "$IMAGE_REPO:$tag" >/dev/null 2>&1 || true
done

# Asset releases: the newest KEEP_RELEASES, never the current or the previous one.
ls -1t assets/releases | tail -n +$((KEEP_RELEASES + 1)) | while read -r release; do
    case "$release" in "$ASSETS_RELEASE"|"$keep_release") continue ;; esac
    rm -rf "assets/releases/$release"
done
