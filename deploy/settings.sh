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
    for file in "$DEPLOY_DIR/deploy.env"; do
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
