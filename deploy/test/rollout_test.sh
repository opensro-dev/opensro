#!/usr/bin/env bash
# rollout_test.sh - rollout.sh against a scratch copy of ~/silkroad.
#
#   deploy/test/rollout_test.sh
#
# Builds a stack folder in a temporary directory (the files deploy.sh would
# send, a .env.prod, two tiny asset releases), then runs rollout.sh there
# with the local images (silkroad-local:<role>-test, deploy/build-images.sh)
# under its own compose project. Creates the external `traefik` network when
# it is missing and removes it again. Cases:
#   a. first rollout with no seed account stops before the Agent starts,
#      names the bootstrap command and leaves pending.env
#   b. with a seed it comes up; release.env names the tag and the release
#   c. a second release moves assets/current and records the previous one
#   d. each successful rollout leaves exactly one new backup
#   e. a rollback to the previous release moves current back
#   f. a release that was never synced is refused before anything stops
#   g. restarting the Agent alone keeps the GameWorld's control calls authorized
#   h. restarting identity alone: the Agent recovers by itself
#   i. a failure after the stop (the backup) brings the previous release back up
#   j. a rollback across a schema upgrade is refused before anything stops
# and in b: the backup is readable by its owner only.
set -euo pipefail

TEST_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$(cd "$TEST_DIR/.." && pwd)"

export COMPOSE_PROJECT_NAME=silkroad-rollout-test
export IMAGE_REPO="${SMOKE_IMAGE_REPO:-silkroad-local}"
export IMAGE_TAG="${SMOKE_IMAGE_TAG:-test}"
export ROLLOUT_SKIP_PULL=1

stack="$(mktemp -d)"
created_network=0
cleanup() {
    status=$?
    if [ "$status" -ne 0 ]; then
        echo "rollout_test: FAILED (stack folder kept at $stack)" >&2
        (cd "$stack" && docker compose -f docker-compose.prod.yml --env-file .env.prod logs --tail 30 agent gameworld-global-official >&2 2>/dev/null) || true
    fi
    (cd "$stack" && docker compose -f docker-compose.prod.yml --env-file .env.prod down -v --remove-orphans >/dev/null 2>&1) || true
    if [ "$created_network" = 1 ]; then docker network rm traefik >/dev/null 2>&1 || true; fi
    if [ "$status" -eq 0 ]; then rm -rf "$stack"; fi
    exit "$status"
}
trap cleanup EXIT

if ! docker network inspect traefik >/dev/null 2>&1; then
    docker network create traefik >/dev/null
    created_network=1
fi

cp "$DEPLOY_DIR/docker-compose.prod.yml" "$DEPLOY_DIR/rollout.sh" "$stack/"
cp -R "$DEPLOY_DIR/config" "$stack/config"
printf 'SRO_DOMAIN=localhost\nLOG_LEVEL=info\n' > "$stack/.env.prod"
for release in r1 r2; do
    mkdir -p "$stack/assets/releases/$release/assets/packs"
    echo "{\"release\":\"$release\"}" > "$stack/assets/releases/$release/assets/packs/manifest.json"
done

rollout() { (cd "$stack" && bash ./rollout.sh) > "$stack/last-rollout.log" 2>&1; }
compose() { (cd "$stack" && docker compose -f docker-compose.prod.yml --env-file .env.prod "$@"); }
value() { sed -nE "s/^$1=(.*)$/\1/p" "$stack/release.env"; }
backups() { find "$stack/backups" -name 'silkroad-*.tar.gz' 2>/dev/null | wc -l | tr -d ' '; }
served_release() { compose exec -T web cat /srv/game/assets/packs/manifest.json; }
fail() { echo "rollout_test: $*" >&2; tail -n 25 "$stack/last-rollout.log" >&2 2>/dev/null || true; exit 1; }

compose down -v --remove-orphans >/dev/null 2>&1 || true

echo "==> a. first rollout without a seed account"
if ASSETS_RELEASE=r1 rollout; then fail "a: rollout succeeded with no account"; fi
output="$(cat "$stack/last-rollout.log")"
case "$output" in *"accounts.sh --bootstrap"*) ;; *) fail "a: no bootstrap instruction in: $output" ;; esac
[ -f "$stack/pending.env" ] || fail "a: no pending.env"
[ "$(backups)" = 0 ] || fail "a: a stopped first rollout left a backup"
[ -z "$(compose ps -q agent)" ] || fail "a: the Agent started"
echo "ok    a"

echo "==> b. with a seed account"
password="$(openssl rand -hex 12)"
hash="$(printf '%s\n' "$password" | (cd "$DEPLOY_DIR/identity" && go run ./cmd/seedhash))"
printf '[{"id":"rollout","passwordHash":"%s"}]\n' "$hash" \
    | compose --env-file "$stack/pending.env" run --rm -T --no-deps --entrypoint sh provision -c \
        'umask 077; cat > /cluster/accounts.json'
ASSETS_RELEASE=r1 rollout || fail "b: rollout failed"
[ "$(value IMAGE_TAG)" = "$IMAGE_TAG" ] || fail "b: release.env IMAGE_TAG is '$(value IMAGE_TAG)'"
[ "$(value ASSETS_RELEASE)" = r1 ] || fail "b: release.env ASSETS_RELEASE is '$(value ASSETS_RELEASE)'"
[ ! -f "$stack/pending.env" ] || fail "b: pending.env left behind"
compose exec -T agent curl -fsS -o /dev/null http://127.0.0.1:8787/readyz || fail "b: the Agent is not ready"
case "$(served_release)" in *r1*) ;; *) fail "b: web does not serve r1" ;; esac
[ "$(backups)" = 1 ] || fail "b: $(backups) backups after one rollout"
mode="$(ls -l "$stack"/backups/silkroad-*.tar.gz | head -n 1 | cut -c1-10)"
[ "$mode" = "-rw-------" ] || fail "b: the backup holds secrets and is $mode"
echo "ok    b"

echo "==> c. a second release"
ASSETS_RELEASE=r2 rollout || fail "c: rollout failed"
[ "$(readlink "$stack/assets/current")" = releases/r2 ] || fail "c: current is $(readlink "$stack/assets/current")"
[ "$(value PREVIOUS_ASSETS_RELEASE)" = r1 ] || fail "c: previous release is '$(value PREVIOUS_ASSETS_RELEASE)'"
case "$(served_release)" in *r2*) ;; *) fail "c: web does not serve r2" ;; esac
echo "ok    c"

echo "==> d. one backup per rollout"
[ "$(backups)" = 2 ] || fail "d: $(backups) backups after two rollouts"
echo "ok    d"

echo "==> e. rollback"
(cd "$stack" && bash ./rollout.sh --rollback) > "$stack/last-rollout.log" 2>&1 || fail "e: rollback failed"
[ "$(readlink "$stack/assets/current")" = releases/r1 ] || fail "e: current is $(readlink "$stack/assets/current")"
case "$(served_release)" in *r1*) ;; *) fail "e: web does not serve r1 after the rollback" ;; esac
echo "ok    e"

echo "==> f. an unsynced release"
before="$(backups)"
if ASSETS_RELEASE=never-synced rollout; then fail "f: rollout accepted a missing release"; fi
[ "$(backups)" = "$before" ] || fail "f: a refused rollout made a backup"
compose exec -T agent curl -fsS -o /dev/null http://127.0.0.1:8787/readyz || fail "f: the refused rollout stopped the Agent"
echo "ok    f"

echo "==> g. the Agent restarts alone"
since="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
compose restart agent >/dev/null 2>&1
compose up -d --wait agent >/dev/null 2>&1 || fail "g: the Agent did not come back healthy"
# Long enough for several lease heartbeats.
until_at=$((SECONDS + 40))
while [ $SECONDS -lt $until_at ]; do
    if compose logs --since "$since" gameworld-global-official 2>/dev/null | grep -q "401"; then
        fail "g: the GameWorld got 401 after the Agent restarted"
    fi
    compose exec -T agent curl -fsS -o /dev/null http://127.0.0.1:8080/jwks.json || fail "g: the JWKS is gone after the Agent restarted"
    [ "$(compose ps --format '{{.Health}}' gameworld-global-official)" = healthy ] || fail "g: the GameWorld is not healthy"
    sleep 5
done
echo "ok    g"

echo "==> h. identity restarts alone"
compose restart identity >/dev/null 2>&1
recovered=0
until_at=$((SECONDS + 90))
while [ $SECONDS -lt $until_at ]; do
    if compose exec -T agent curl -fsS -o /dev/null http://127.0.0.1:8080/jwks.json 2>/dev/null \
        && [ "$(compose ps --format '{{.Health}}' agent)" = healthy ]; then
        recovered=1
        break
    fi
    sleep 5
done
[ "$recovered" = 1 ] || fail "h: the Agent did not recover after identity restarted"
echo "ok    h"

echo "==> i. a failure after the stop"
if ROLLOUT_BACKUP_IMAGE=alpine:does-not-exist-silkroad ASSETS_RELEASE=r2 rollout; then fail "i: the rollout succeeded with a broken backup"; fi
grep -q "previous release" "$stack/last-rollout.log" || fail "i: no recovery message"
compose exec -T agent curl -fsS -o /dev/null http://127.0.0.1:8787/readyz || fail "i: the Agent was left stopped"
[ "$(compose ps --format '{{.Health}}' gameworld-global-official)" = healthy ] || fail "i: the GameWorld was left stopped"
echo "ok    i"

echo "==> j. rollback across a schema upgrade"
printf 'UPGRADED_SCHEMA=1\nPRE_UPGRADE_BACKUP=backups/example.tar.gz\n' >> "$stack/release.env"
if (cd "$stack" && bash ./rollout.sh --rollback) > "$stack/last-rollout.log" 2>&1; then fail "j: a rollback across a schema upgrade ran"; fi
grep -q "backups/example.tar.gz" "$stack/last-rollout.log" || fail "j: the refusal does not name the backup to restore"
[ "$(compose ps --format '{{.Health}}' gameworld-global-official)" = healthy ] || fail "j: the refused rollback stopped the GameWorld"
echo "ok    j"

echo "rollout_test: passed"
