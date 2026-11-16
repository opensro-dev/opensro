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
