# settings.sh - shared helpers for the deploy scripts; sourced, never run.
# shellcheck shell=bash
#
#   . "$DEPLOY_DIR/settings.sh"
#
# DEPLOY_DIR and REPO_ROOT are set by the sourcing script.

#
# generated_dir <client-public|game-data>
#
# The main checkout's generated trees through the repository's own resolver
# (scripts/lib/generatedRoot.mjs, AGENTS.md "Worktrees"): never a path built
# by hand, so a worktree reads what the main checkout built.
#
generated_dir() {
    case "$1" in
        client-public)
            (cd "$REPO_ROOT" && node --input-type=module -e \
                'import { CLIENT_PUBLIC_ROOT } from "./scripts/lib/generatedRoot.mjs"; console.log(CLIENT_PUBLIC_ROOT)') ;;
        game-data)
            # The directory holding server/ and server.srogz.
            (cd "$REPO_ROOT" && node --input-type=module -e \
                'import { resolveServerGameDataRoot } from "./scripts/build/world/paths.mjs"; import path from "node:path";
                 console.log(path.dirname(resolveServerGameDataRoot()))') ;;
        *) echo "generated_dir: unknown tree '$1'" >&2; return 1 ;;
    esac
}

#
# load_settings VAR...
#
# Sets each VAR the environment leaves empty from deploy/deploy.env (yours,
# ignored by git; deploy.example.env shows its keys). A value ends at its
# first blank, so a comment after it is ignored.
#
load_settings() {
    local file key value wanted
    for file in "${DEPLOY_SETTINGS_FILE:-$DEPLOY_DIR/deploy.env}"; do
        [ -f "$file" ] || continue
        while IFS='=' read -r key value; do
            value="${value%%[[:space:]]*}"
            for wanted in "$@"; do
                [ "$key" = "$wanted" ] || continue
                [ -n "${!wanted:-}" ] || printf -v "$wanted" '%s' "$value"
            done
        done < <(grep -E '^[A-Z_]+=' "$file")
    done
}

#
# check_deploy_path
#
# Refuses a DEPLOY_PATH that could reach something else on the host: empty,
# the home itself, a parent, a quote. A folder holding another stack is caught on the host by the .silkroad marker.
#
check_deploy_path() {
    DEPLOY_PATH="${DEPLOY_PATH#\~/}"
    DEPLOY_PATH="${DEPLOY_PATH%/}"
    case "$DEPLOY_PATH" in
        ""|"/"|"."|".."|../*|*/..|*/../*) echo "refusing DEPLOY_PATH='$DEPLOY_PATH'" >&2; return 1 ;;
        *\'*|*\"*|*[[:space:]]*) echo "DEPLOY_PATH cannot contain quotes or spaces" >&2; return 1 ;;
    esac
}

#
# native_asset_urls
#
# The files the browser loads by URL instead of through the packs: the boot
# screen and the CSS cursors. The upstream owns the list
# (apps/client-next/src/engine/foundation/assets/native-assets.ts); Node
# reads it with its built-in type stripping, so the deploy never copies it.
#
native_asset_urls() {
    (cd "$REPO_ROOT" && node --input-type=module -e \
        'import { nativeAssetUrls } from "./apps/client-next/src/engine/foundation/assets/native-assets.ts";
         for (const url of nativeAssetUrls()) console.log(url);')
}

#
# asset_filter
#
# rsync filter rules for the game assets the browser fetches: the packs,
# their manifest, the web manifest, the delivery lists and the native
# files. Everything else under client-public is the build's loose tree,
# which the packs already carry (docs/architecture/ASSET_DELIVERY.md).
#
asset_filter() {
    local url dir
    local -A seen=()
    printf '%s\n' --include=/assets/ --include=/assets/packs/*** --include=/assets/delivery/*** \
        --include=/assets/manifest.json --include=/assets/manifest.json.gz
    while read -r url; do
        # rsync descends only into included directories: name every parent.
        dir="${url%/*}"
        while [ "$dir" != "/assets" ] && [ -n "$dir" ]; do
            if [ -z "${seen[$dir]:-}" ]; then seen[$dir]=1; printf '%s\n' "--include=$dir/"; fi
            dir="${dir%/*}"
        done
        printf '%s\n' "--include=$url"
    done < <(native_asset_urls)
    printf '%s\n' --exclude=*
}

#
# asset_release_id <client-public>
#
# The release's name: the first 16 hex of the packs manifest's sha256, so
# one data build is one release however often it is deployed.
#
asset_release_id() {
    shasum -a 256 "$1/assets/packs/manifest.json" | cut -c1-16
}

#
# sync_asset_release <client-public> <destination> <link-dest|"">
#
# rsync the release; files equal to the ones in link-dest (the release that
# is up) become hard links there instead of crossing the wire again. The
# destination is local or host:path.
#
sync_asset_release() {
    local source="$1" destination="$2" link=()
    [ -n "$3" ] && link=(--link-dest="$3")
    local filter=()
    mapfile -t filter < <(asset_filter)
    [ "${#filter[@]}" -gt 6 ] || { echo "asset_filter: native-assets.ts gave no URL" >&2; return 1; }
    rsync -a --delete "${link[@]}" "${filter[@]}" "$source/" "$destination/"
}
