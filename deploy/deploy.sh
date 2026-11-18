#!/usr/bin/env bash
# deploy.sh - publish, sync the assets and roll out on the Docker host.
#
#   deploy/deploy.sh                                  everything
#   IMAGE_TAG=<tag> SKIP_PUBLISH=1 deploy/deploy.sh   roll out a tag already in the registry
#
# 1. publish.sh builds, smoke-tests and pushes the images (it prints the tag).
# 2. The game assets go by rsync to <DEPLOY_PATH>/assets/releases/<id>; files
#    the release that is up already has become hard links, so only what
#    changed crosses the wire. A release already on the host is not resent.
# 3. The stack files (compose, rollout.sh, config/, the .example env files) are
#    swapped in whole; .env.prod, shard.env, shard-pvp.env, release.env, pending.env,
#    backups/ and assets/ stay.
# 4. rollout.sh runs on the host with the tag and the release.
#
# Settings: deploy/deploy.env (cp deploy/deploy.example.env deploy/deploy.env).
# DRY_GUARD=1 only checks DEPLOY_PATH and exits (deploy/test/deploy_test.sh).
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"
load_settings DEPLOY_HOST DEPLOY_PATH IMAGE_REPO PLATFORM

: "${DEPLOY_HOST:?set DEPLOY_HOST in deploy/deploy.env (cp deploy/deploy.example.env deploy/deploy.env)}"
check_deploy_path
if [ "${DRY_GUARD:-}" = 1 ]; then
    echo "DEPLOY_PATH '$DEPLOY_PATH' accepted"
    exit 0
fi
: "${IMAGE_REPO:?set IMAGE_REPO in deploy/deploy.env}"
export IMAGE_REPO PLATFORM

#
# remote <script>
#
# Runs a bash script on the host inside DEPLOY_PATH (created when missing).
# The marker .silkroad says the folder is this stack's: a folder that holds
# something else is refused before anything is written.
#
remote() {
    ssh "$DEPLOY_HOST" "DEPLOY_PATH='$DEPLOY_PATH' bash -s" <<SCRIPT
set -euo pipefail
mkdir -p "\$DEPLOY_PATH" && cd "\$DEPLOY_PATH"
if [ ! -f .silkroad ] && [ -n "\$(find . -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    echo "refusing: \$PWD is not empty and is not the Silkroad stack (no .silkroad marker)" >&2
    exit 1
fi
touch .silkroad
$1
SCRIPT
}

if [ "${SKIP_PUBLISH:-}" = 1 ]; then
    : "${IMAGE_TAG:?SKIP_PUBLISH=1 needs IMAGE_TAG}"
else
    IMAGE_TAG="$("$DEPLOY_DIR/publish.sh")"
fi

client_public="$(generated_dir client-public)"
ASSETS_RELEASE="$(asset_release_id "$client_public")"
echo "==> assets $ASSETS_RELEASE"
state="$(remote '
mkdir -p assets/releases
if [ -d "assets/releases/'"$ASSETS_RELEASE"'" ]; then echo present; exit 0; fi
current="$(readlink assets/current 2>/dev/null || true)"
echo "missing ${current#releases/}"
')"
case "$state" in
    present) echo "already on the host" ;;
    missing*)
        up="${state#missing}"; up="${up# }"
        link=""
        [ -n "$up" ] && link="../$up"
        sync_asset_release "$client_public" "$DEPLOY_HOST:$DEPLOY_PATH/assets/releases/$ASSETS_RELEASE.partial" "$link"
        remote 'mv "assets/releases/'"$ASSETS_RELEASE"'.partial" "assets/releases/'"$ASSETS_RELEASE"'"'
        ;;
    *) echo "unexpected answer from the host: $state" >&2; exit 1 ;;
esac

echo "==> stack files to $DEPLOY_HOST:$DEPLOY_PATH"
flags=()
if tar --version 2>/dev/null | grep -q bsdtar; then flags+=(--no-mac-metadata --no-xattrs); fi
COPYFILE_DISABLE=1 tar -C "$DEPLOY_DIR" -czf - ${flags[@]+"${flags[@]}"} \
    docker-compose.prod.yml rollout.sh config .env.prod.example shard-pvp.env.example \
    | ssh "$DEPLOY_HOST" "cd '$DEPLOY_PATH' && rm -rf .incoming && mkdir .incoming && tar -xzf - -C .incoming \
        && rm -rf docker-compose.prod.yml rollout.sh config .env.prod.example shard-pvp.env.example \
        && mv .incoming/* .incoming/.env.prod.example . && rmdir .incoming"

echo "==> roll out $IMAGE_TAG with assets $ASSETS_RELEASE"
ssh "$DEPLOY_HOST" "cd '$DEPLOY_PATH' && IMAGE_REPO='$IMAGE_REPO' IMAGE_TAG='$IMAGE_TAG' ASSETS_RELEASE='$ASSETS_RELEASE' bash ./rollout.sh"
