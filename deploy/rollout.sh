#!/usr/bin/env bash
# rollout.sh - roll a published release out on the Docker host. Compiles nothing.
#
#   IMAGE_REPO=<repo> IMAGE_TAG=<tag> ASSETS_RELEASE=<id> ~/silkroad/rollout.sh
#   ~/silkroad/rollout.sh                    the release in release.env again
#   ~/silkroad/rollout.sh --rollback         the previous image tag and asset release
#
# deploy.sh runs it after publishing the images and syncing the assets.
# --rollback takes the PREVIOUS_* values of release.env together, and is
# refused when the release that is up upgraded the shard's schema: an older
# binary refuses a newer authority, so that rollback needs the backup the
# upgrade was taken from (README "Backups").
#
# Order, stopping at the first failure: check the folder, pull and check the
# image architecture, provision what is missing (never replacing anything),
# stop for the first account when there is none, stop the GameWorld and then
# the Agent so no SQLite is written while the backup copies it, back up every
# state volume, upgrade the shard authority, point assets/current at the
# release, start everything waiting for its health check, and only then
# rewrite release.env and prune old images and asset releases.
#
# A failure after the services stop brings the previous release back up when
# it happens before the upgrade, and says what is down when it happens after.
#
# ROLLOUT_SKIP_PULL=1 uses local images (deploy/test/rollout_test.sh).
set -euo pipefail

# The backups hold every secret of the stack (session keys, tokens, hashes).
umask 077

cd "$(dirname "$0")"

rollback=0
if [ "${1:-}" = "--rollback" ]; then rollback=1; fi

PROJECT="${COMPOSE_PROJECT_NAME:-silkroad}"
KEEP_BACKUPS=5
KEEP_RELEASES=2
STATE_VOLUMES=(cluster agent identity shard-global-official shard-pvp)
BACKUP_IMAGE="${ROLLOUT_BACKUP_IMAGE:-alpine:3.20}"

fail() { echo "rollout: $*" >&2; exit 1; }

if [ ! -f .env.prod ]; then
    fail "missing $PWD/.env.prod: cp .env.prod.example .env.prod and fill it in"
fi
if grep -q '=change-me' .env.prod; then
    fail "$PWD/.env.prod still has change-me values"
fi
grep -Eq '^SRO_DOMAIN=.+' .env.prod || fail "$PWD/.env.prod has no SRO_DOMAIN"
# The PVP shard must not come up native (port-only, pvpserver).
[ -f shard-pvp.env ] || fail "missing $PWD/shard-pvp.env: cp shard-pvp.env.example shard-pvp.env"

release_value() { sed -nE "s/^$1=(.*)$/\1/p" release.env 2>/dev/null; }

if [ "$rollback" = 1 ]; then
    [ -f release.env ] || fail "--rollback: no release.env, nothing was rolled out here"
    if [ "$(release_value UPGRADED_SCHEMA)" = 1 ]; then
        fail "--rollback refused: the release that is up upgraded the shard's schema, and the previous binary would refuse it. Restore $(release_value PRE_UPGRADE_BACKUP) first (README \"Backups\"), then roll out the previous release"
    fi
    IMAGE_TAG="$(release_value PREVIOUS_IMAGE_TAG)"
    ASSETS_RELEASE="$(release_value PREVIOUS_ASSETS_RELEASE)"
    [ -n "$IMAGE_TAG" ] && [ -n "$ASSETS_RELEASE" ] || fail "--rollback: release.env names no previous release"
fi

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
    compose --profile jobs pull provision derive-public agent identity web admin admin-relay admin-relay-pvp \
        || fail "pull failed: is $IMAGE_TAG published, and did $(whoami) run 'docker login ${IMAGE_REPO%%/*}'?"
    # Pulled now, not at the backup: nothing may fetch from the network once the game is down.
    docker pull -q "$BACKUP_IMAGE" >/dev/null || fail "cannot pull $BACKUP_IMAGE for the backup"
fi

# An image of another architecture only fails at its first exec: check before anything stops.
host_arch="$(docker info --format '{{.Architecture}}')"
case "$host_arch" in x86_64) host_arch=amd64 ;; aarch64) host_arch=arm64 ;; esac
for role in server identity web admin; do
    image_arch="$(docker image inspect --format '{{.Architecture}}' "$IMAGE_REPO:$role-$IMAGE_TAG")" \
        || fail "$IMAGE_REPO:$role-$IMAGE_TAG is not on this host"
    [ "$image_arch" = "$host_arch" ] || fail "$IMAGE_REPO:$role-$IMAGE_TAG is $image_arch, this host is $host_arch"
done

echo "==> provision"
compose run --rm provision
compose run --rm provision-pvp
compose run --rm derive-public

if ! compose run --rm -T --no-deps --entrypoint test provision -e /cluster/accounts.json; then
    printf 'IMAGE_REPO=%s\nIMAGE_TAG=%s\n' "$IMAGE_REPO" "$IMAGE_TAG" > pending.env
    fail "no account yet: run deploy/accounts.sh --bootstrap <id> from your machine, then this rollout again"
fi

#
# recover
#
# Runs when a step after the stop fails. Before the upgrade the previous
# release is untouched, so it comes back up; after it, only a person can
# tell whether the authority is usable, so it says what is down instead.
#
stage=stopped
recover() {
    status=$?
    [ "$status" -ne 0 ] || return 0
    echo "rollout: FAILED with the services stopped" >&2
    if [ "$stage" = stopped ] && [ -n "$previous_tag" ] && [ -n "$previous_release" ]; then
        echo "rollout: bringing the previous release back up ($previous_tag, assets $previous_release)" >&2
        IMAGE_TAG="$previous_tag" ASSETS_DIR="./assets/releases/$previous_release" bash -c '
            compose() { docker compose -p "$0" -f docker-compose.prod.yml --env-file .env.prod "$@"; }
            compose up -d identity &&
            compose up -d --wait --force-recreate --no-deps agent &&
            compose up -d --wait gameworld-global-official web &&
            { compose up -d --force-recreate --no-deps admin-relay admin-relay-pvp admin || true; }' "$PROJECT" >&2 \
            || echo "rollout: the previous release did not come back; the stack is DOWN" >&2
    else
        echo "rollout: the stack is DOWN (stage: $stage). The backup taken before the upgrade is ${backup:-none}; see README \"Backups\"" >&2
    fi
    exit "$status"
}
trap recover EXIT

echo "==> stop the GameWorlds, then the Agent"
compose stop gameworld-global-official gameworld-pvp
compose stop agent

mkdir -p backups
chmod 700 backups
backup="backups/silkroad-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
echo "==> backup $backup"
mounts=()
for volume in "${STATE_VOLUMES[@]}"; do mounts+=(-v "${PROJECT}_$volume:/backup/$volume:ro"); done
docker run --rm "${mounts[@]}" "$BACKUP_IMAGE" tar -C /backup -czf - . > "$backup.partial"
mv "$backup.partial" "$backup"
ls -1 backups/silkroad-*.tar.gz | sort -r | tail -n +$((KEEP_BACKUPS + 1)) | xargs -r rm -f --

echo "==> upgrade"
stage=upgrade
upgrade_report="$(compose run --rm upgrade 2>&1)"
upgrade_report_pvp="$(compose run --rm upgrade-pvp 2>&1)"
echo "$upgrade_report"
echo "$upgrade_report_pvp"
upgraded=0
for report in "$upgrade_report" "$upgrade_report_pvp"; do
    case "$report" in
        *"already in the current format"*|*"no authority yet"*) ;;
        *) upgraded=1 ;;
    esac
done
stage=starting

echo "==> assets $ASSETS_RELEASE"
ln -sfn "releases/$ASSETS_RELEASE" assets/current.next
mv -fh assets/current.next assets/current 2>/dev/null || mv -fT assets/current.next assets/current

echo "==> start"
# identity first: the Agent joins its network namespace, and a recreated
# identity leaves the old Agent container without a network, so the Agent
# is always recreated after it.
compose up -d identity
compose up -d --wait --force-recreate --no-deps agent
compose up -d --wait gameworld-global-official gameworld-pvp web
# The panel and its relay live in the GameWorld's and identity's namespaces:
# recreated after them, never left in a namespace that is gone.
compose up -d --force-recreate --no-deps admin-relay admin-relay-pvp admin
compose ps
stage=up

{
    printf 'IMAGE_REPO=%s\nIMAGE_TAG=%s\nASSETS_RELEASE=%s\n' "$IMAGE_REPO" "$IMAGE_TAG" "$ASSETS_RELEASE"
    if [ "$previous_tag" != "$IMAGE_TAG" ] || [ "$previous_release" != "$ASSETS_RELEASE" ]; then
        printf 'PREVIOUS_IMAGE_TAG=%s\nPREVIOUS_ASSETS_RELEASE=%s\n' "$previous_tag" "$previous_release"
    else
        sed -nE '/^PREVIOUS_/p' release.env
    fi
    if [ "$upgraded" = 1 ]; then
        printf 'UPGRADED_SCHEMA=1\nPRE_UPGRADE_BACKUP=%s\n' "$backup"
    fi
} > release.env.next
mv release.env.next release.env
rm -f pending.env
echo "==> up: $IMAGE_REPO:<role>-$IMAGE_TAG, assets $ASSETS_RELEASE"

keep_tag="$(sed -nE 's/^PREVIOUS_IMAGE_TAG=(.*)$/\1/p' release.env)"
keep_release="$(sed -nE 's/^PREVIOUS_ASSETS_RELEASE=(.*)$/\1/p' release.env)"

# This stack's images only: the tag that is up and the previous one stay.
docker image ls "$IMAGE_REPO" --format '{{.Tag}}' | while read -r tag; do
    case "$tag" in server-*|identity-*|web-*|admin-*) ;; *) continue ;; esac
    case "${tag#*-}" in "$IMAGE_TAG"|"$keep_tag") continue ;; esac
    docker image rm "$IMAGE_REPO:$tag" >/dev/null 2>&1 || true
done

# Asset releases: the newest KEEP_RELEASES, never the current or the previous one.
ls -1t assets/releases | tail -n +$((KEEP_RELEASES + 1)) | while read -r release; do
    case "$release" in "$ASSETS_RELEASE"|"$keep_release") continue ;; esac
    rm -rf "assets/releases/$release"
done
