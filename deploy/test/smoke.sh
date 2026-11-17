#!/usr/bin/env bash
# smoke.sh - run the production compose stack locally and prove it serves.
#
#   deploy/test/smoke.sh            build nothing, run, check, tear down
#   deploy/test/smoke.sh --keep     leave the stack up (project silkroad-smoke)
#
# Uses the images deploy/build-images.sh loads (silkroad-local:<role>-test,
# or SMOKE_IMAGE_REPO / SMOKE_IMAGE_TAG), fresh volumes, the production
# compose file with docker-compose.smoke.yml over it (no Traefik, the web
# edge on 127.0.0.1:$SMOKE_PORT), and the main checkout's published client
# assets. It fails on the first check that does not hold.
set -euo pipefail

TEST_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$(cd "$TEST_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
. "$DEPLOY_DIR/settings.sh"

keep=0
[ "${1:-}" = "--keep" ] && keep=1

export IMAGE_REPO="${SMOKE_IMAGE_REPO:-silkroad-local}"
export IMAGE_TAG="${SMOKE_IMAGE_TAG:-test}"
export SMOKE_PORT="${SMOKE_PORT:-18080}"
export SMOKE_ADMIN_PORT="${SMOKE_ADMIN_PORT:-19090}"
ASSETS_DIR="${SMOKE_ASSETS_DIR:-$(generated_dir client-public)}"
export ASSETS_DIR
PROJECT=silkroad-smoke
base="http://127.0.0.1:$SMOKE_PORT"

compose() {
    docker compose -p "$PROJECT" --project-directory "$DEPLOY_DIR" \
        -f "$DEPLOY_DIR/docker-compose.prod.yml" -f "$DEPLOY_DIR/docker-compose.smoke.yml" \
        --env-file "$TEST_DIR/smoke.env" "$@"
}

cleanup() {
    status=$?
    if [ "$status" -ne 0 ]; then
        echo "smoke: FAILED; last log lines:" >&2
        compose logs --tail 40 agent gameworld-global-official web >&2 2>/dev/null || true
    fi
    if [ "$keep" = 0 ]; then compose down -v --remove-orphans >/dev/null 2>&1 || true; fi
    exit "$status"
}
trap cleanup EXIT

# Digest of every secret the provision jobs own: a re-run must not change one.
secrets_digest() {
    docker run --rm -v "${PROJECT}_cluster:/c:ro" -v "${PROJECT}_identity:/i:ro" -v "${PROJECT}_admin-secrets:/a:ro" alpine \
        sh -c 'cd / && find c i a -type f | sort | xargs sha256sum'
}

check() { # name, url
    if ! curl -fsS -o /dev/null --max-time 10 "$2"; then
        echo "smoke: $1 failed ($2)" >&2
        return 1
    fi
    echo "ok    $1"
}

compose down -v --remove-orphans >/dev/null 2>&1 || true

echo "==> provision"
compose run --rm provision
compose run --rm derive-public

echo "==> seed account"
password="$(openssl rand -hex 12)"
hash="$(printf '%s\n' "$password" | (cd "$DEPLOY_DIR/identity" && go run ./cmd/seedhash))"
printf '[{"id":"smoke","passwordHash":"%s"}]\n' "$hash" \
    | compose run --rm -T --entrypoint sh provision -c \
        'set -e; test ! -e /cluster/accounts.json; umask 077; cat > /cluster/accounts.json'

echo "==> upgrade"
compose run --rm upgrade

echo "==> up"
compose up -d --wait agent identity gameworld-global-official web

check "agent through /api" "$base/api/readyz"
check "gameworld through /shards" "$base/shards/global-official/transport/readyz"
check "client page" "$base/play"
check "packs manifest" "$base/assets/packs/manifest.json"
check "boot screen image" "$base/assets/images/Media_extracted/interface/outer/logo-big.png"

# Text is compressed on the wire; packs never are (a Range over an encoded
# body would cut the compressed bytes; ASSET_DELIVERY.md "Sizes, measured").
encoding() { curl -fsS -o /dev/null -D - -H 'Accept-Encoding: gzip' "$@" | tr -d '\r' | sed -nE 's/^[Cc]ontent-[Ee]ncoding: *//p'; }
# Matched in bash, not in a pipe: under pipefail, `head` closing the pipe
# would fail the command before it.
page="$(curl -fsS "$base/play")"
[[ "$page" =~ (/assets/[A-Za-z0-9_-]+\.js) ]] || { echo "smoke: no bundle in /play" >&2; exit 1; }
bundle="${BASH_REMATCH[1]}"
[ "$(encoding "$base$bundle")" = gzip ] || { echo "smoke: $bundle is not gzip-encoded" >&2; exit 1; }
echo "ok    client bundle gzip"
packs="$(curl -fsS "$base/assets/packs/manifest.json")"
[[ "$packs" =~ (/assets/packs/[A-Za-z0-9_-]+\.bin) ]] || { echo "smoke: no pack in the manifest" >&2; exit 1; }
pack="${BASH_REMATCH[1]}"
[ -z "$(encoding -H 'Range: bytes=0-1023' "$base$pack")" ] || { echo "smoke: $pack came back content-encoded" >&2; exit 1; }
echo "ok    packs not re-encoded"

# A real title login through the edge: the seeded account and the session key ring.
protocol="$(sed -nE 's/^export const RELEASE_PROTOCOL = ([0-9]+);/\1/p' \
    "$REPO_ROOT/apps/client-next/src/engine/foundation/release/protocol.ts")"
login() { # password [id]
    printf '{"id":"%s","password":"%s","serverId":"global-official"}' "${2:-smoke}" "$1" \
        | curl -fsS --max-time 10 -H "X-OpenSRO-Protocol: $protocol" -H 'content-type: application/json' \
            --data @- "$base/api/title/login"
}
case "$(login "$password")" in
    *'"ok":true'*) echo "ok    title login" ;;
    *) echo "smoke: the seeded account cannot log in" >&2; exit 1 ;;
esac
case "$(login "not-$password")" in
    *'"ok":false'*) echo "ok    wrong password refused" ;;
    *) echo "smoke: a wrong password was not refused" >&2; exit 1 ;;
esac

echo "==> accounts.sh"
accounts() { ACCOUNTS_LOCAL=1 ACCOUNTS_PROJECT="$PROJECT" "$DEPLOY_DIR/accounts.sh" "$@"; }
second="$(openssl rand -hex 12)"
printf '%s\n' "$second" | accounts smoke2 >/dev/null
case "$(login "$second" smoke2)" in
    *'"ok":true'*) echo "ok    accounts.sh created a working account" ;;
    *) echo "smoke: the account accounts.sh created cannot log in" >&2; exit 1 ;;
esac
if printf '%s\n' "$second" | accounts smoke2 >/dev/null 2>&1; then
    echo "smoke: accounts.sh created a duplicate account" >&2; exit 1
fi
echo "ok    duplicate account refused"
if printf '%s\n' "$second" | accounts 'bad"id' >/dev/null 2>&1; then
    echo "smoke: accounts.sh accepted an unsafe id" >&2; exit 1
fi
echo "ok    unsafe id refused"

echo "==> admin panel"
compose up -d --no-deps admin-relay admin
admin="http://127.0.0.1:$SMOKE_ADMIN_PORT"
for _ in $(seq 1 30); do curl -fsS -o /dev/null --max-time 2 "$admin/accounts" && break; sleep 1; done
check "admin panel" "$admin/accounts"
third="$(openssl rand -hex 12)"
created="$(curl -fsS -o /dev/null -w '%{redirect_url}' --max-time 10 --data-urlencode action=create \
    --data-urlencode id=smoke3 --data-urlencode "password=$third" --data-urlencode "confirm=$third" "$admin/accounts")"
case "$created" in *ok=*) ;; *) echo "smoke: the panel did not create an account: $created" >&2; exit 1 ;; esac
case "$(login "$third" smoke3)" in
    *'"ok":true'*) echo "ok    the panel created a working account (relay to the Agent)" ;;
    *) echo "smoke: the account the panel created cannot log in" >&2; exit 1 ;;
esac
# No character exists on a fresh shard: "not found" proves the operator API answered
# the panel's token over the GameWorld's loopback, where a 403 would not.
case "$(curl -fsS --max-time 10 "$admin/character?name=Nobody")" in
    *"character not found"*) echo "ok    the panel reaches the player operations" ;;
    *) echo "smoke: the panel cannot reach the player operations" >&2; exit 1 ;;
esac
if curl -fsS -o /dev/null --max-time 5 -H 'Host: evil.example' "$admin/accounts" 2>/dev/null; then
    echo "smoke: the panel answered a foreign Host" >&2; exit 1
fi
echo "ok    foreign Host refused"

echo "==> provision is idempotent"
before="$(secrets_digest)"
compose run --rm provision >/dev/null
compose run --rm derive-public >/dev/null
after="$(secrets_digest)"
if [ "$before" != "$after" ]; then
    echo "smoke: a provision re-run changed a secret" >&2
    diff <(echo "$before") <(echo "$after") >&2 || true
    exit 1
fi
echo "ok    secrets unchanged"

echo "smoke: passed"
