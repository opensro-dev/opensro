#!/usr/bin/env bash
# deploy_test.sh - deploy.sh's guards and its asset release, without a server.
#
#   deploy/test/deploy_test.sh
#
#   a. DEPLOY_PATH values that could reach something else are refused
#   b. the asset release holds every pack the packs manifest names, the
#      manifests, delivery/ and the files the browser loads by URL
#      (native-assets.ts: boot screen, cursors), and nothing else of the
#      build tree
#   c. the smoke test passes with the web edge serving that release
set -euo pipefail

TEST_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$(cd "$TEST_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"
fail() { echo "deploy_test: $*" >&2; exit 1; }

echo "==> a. DEPLOY_PATH guard"
for bad in "" "/" "." ".." "../x" "a'b" "a b"; do
    if DEPLOY_SETTINGS_FILE=/nonexistent DEPLOY_HOST=nobody@example.invalid DEPLOY_PATH="$bad" DRY_GUARD=1 \
        "$DEPLOY_DIR/deploy.sh" >/dev/null 2>&1; then
        fail "a: DEPLOY_PATH='$bad' was accepted"
    fi
done
DEPLOY_SETTINGS_FILE=/nonexistent DEPLOY_HOST=nobody@example.invalid DEPLOY_PATH=silkroad DRY_GUARD=1 \
    "$DEPLOY_DIR/deploy.sh" >/dev/null || fail "a: DEPLOY_PATH=silkroad was refused"
echo "ok    a"

echo "==> b. asset release"
release="$(mktemp -d)"
trap 'rm -rf "$release"' EXIT
sync_asset_release "$(generated_dir client-public)" "$release" ""
[ -f "$release/assets/packs/manifest.json" ] || fail "b: no packs manifest"
[ -d "$release/assets/delivery" ] || fail "b: no delivery/"
missing="$(python3 - "$release" <<'PY'
import json, os, sys
root = sys.argv[1]
manifest = json.load(open(os.path.join(root, "assets/packs/manifest.json")))
paths = [pack["path"] for group in manifest["groups"] for pack in group["packs"]]
missing = [p for p in paths if not os.path.isfile(os.path.join(root, p.lstrip("/")))]
print(len(paths), len(missing), *missing[:5])
PY
)"
set -- $missing
[ "$1" -gt 0 ] || fail "b: the packs manifest names no pack"
[ "$2" = 0 ] || fail "b: packs missing from the release: ${*:3}"
[ ! -e "$release/assets/world" ] || fail "b: the build tree (assets/world) came along"
native=0
while read -r url; do
    [ -f "$release$url" ] || fail "b: $url (loaded by URL, native-assets.ts) is missing from the release"
    native=$((native + 1))
done < <(native_asset_urls)
[ "$native" -gt 0 ] || fail "b: native-assets.ts listed no URL"
echo "ok    b ($1 packs, $native native files)"

echo "==> c. smoke over the release"
SMOKE_ASSETS_DIR="$release" "$TEST_DIR/smoke.sh" >/dev/null || fail "c: smoke failed over the release"
echo "ok    c"

echo "deploy_test: passed"
