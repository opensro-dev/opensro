#!/usr/bin/env bash
# publish_test.sh - publish.sh without pushing anything.
#
#   deploy/test/publish_test.sh
#
#   a. with no IMAGE_REPO it refuses and names the setting
#   b. a dry run prints exactly one line on stdout: the tag (the commit's
#      12 characters, with -dirty-<UTC time> when the images' sources have
#      uncommitted changes), and pushes nothing
set -euo pipefail

TEST_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$(cd "$TEST_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
fail() { echo "publish_test: $*" >&2; exit 1; }

echo "==> a. no IMAGE_REPO"
# An empty deploy.env stand-in: point the loader at a folder without one.
if err="$(cd /tmp && IMAGE_REPO= DEPLOY_SETTINGS_FILE=/nonexistent PUBLISH_DRY_RUN=1 SKIP_SMOKE=1 "$DEPLOY_DIR/publish.sh" 2>&1 >/dev/null)"; then
    fail "a: publish.sh ran without IMAGE_REPO"
fi
case "$err" in *IMAGE_REPO*) ;; *) fail "a: the refusal does not name IMAGE_REPO: $err" ;; esac
echo "ok    a"

echo "==> b. dry run"
stdout="$(IMAGE_REPO=ghcr.io/example/silkroad-test DEPLOY_SETTINGS_FILE=/nonexistent PUBLISH_DRY_RUN=1 SKIP_SMOKE=1 \
    "$DEPLOY_DIR/publish.sh" 2>/dev/null)" || fail "b: dry run failed"
[ "$(printf '%s\n' "$stdout" | wc -l | tr -d ' ')" = 1 ] || fail "b: stdout has more than the tag: $stdout"
head="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD)"
[[ "$stdout" =~ ^$head(-dirty-[0-9]{8}T[0-9]{6}Z)?$ ]] || fail "b: tag '$stdout' is not '$head[-dirty-<time>]'"
echo "ok    b ($stdout)"

echo "publish_test: passed"
