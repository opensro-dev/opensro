#!/usr/bin/env bash
# ===========================================================================
#
# test-in-container.sh - exercise opensro-backup end to end on Ubuntu 24.04
#
#	docker run --rm -v "$PWD:/src:ro" ubuntu:24.04 bash /src/test-in-container.sh
#
# A local rclone remote stands in for Google Drive and a tiny HTTP server
# for the Discord webhook. A writer keeps inserting rows while the backup
# runs, as a live game would. Checks: a verified nightly run, an
# incremental second run, restore contents, the weekly deep check, the
# failure alert, and the freshness alert.
#
# ===========================================================================
set -Eeuo pipefail

fail() { echo "FAIL: $*" >&2; echo "--- webhook log:" >&2; cat /tmp/hook.log >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends restic rclone sqlite3 rsync python3 curl ca-certificates sudo >/dev/null
install -m 0755 /src/opensro-backup.sh /usr/local/sbin/opensro-backup

# Webhook stand-in: records every JSON body it receives.
cat >/tmp/hook.py <<'PY'
import http.server, sys
class Hook(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        open("/tmp/hook.log", "ab").write(body + b"\n")
        self.send_response(204); self.end_headers()
    def do_GET(self):
        open("/tmp/hook.log", "ab").write(b"GET " + self.path.encode() + b"\n")
        self.send_response(200); self.end_headers()
    def log_message(self, *args): pass
http.server.HTTPServer(("127.0.0.1", 8765), Hook).serve_forever()
PY
python3 /tmp/hook.py &
touch /tmp/hook.log

# A game-like state root: a live database and secret files.
STATE=/opt/opensro/apps/server/.state
mkdir -p "$STATE/shards/global-official/authority" "$STATE/cluster/agent"
DB="$STATE/shards/global-official/authority/state.db"
sqlite3 "$DB" "PRAGMA journal_mode=WAL; CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);"
echo '{"secret": true}' >"$STATE/cluster/agent-session-keys.json"
( while true; do sqlite3 "$DB" ".timeout 5000" "INSERT INTO items (name) VALUES (hex(randomblob(16)));"; done ) &
WRITER=$!

install -d -m 0700 /etc/opensro-backup
sed -e "s|^RCLONE_REMOTE=.*|RCLONE_REMOTE=drive:/backups/repo|" /src/backup.conf.example >/etc/opensro-backup/backup.conf
head -c 32 /dev/urandom | base64 >/etc/opensro-backup/restic-password
rclone config create drive local --config /etc/opensro-backup/rclone.conf >/dev/null
echo "http://127.0.0.1:8765/" >/etc/opensro-backup/discord-webhook
echo "http://127.0.0.1:8765/ping" >/etc/opensro-backup/healthcheck-url

echo "== nightly run (writer active)"
opensro-backup run
[[ -s /var/lib/opensro-backup/last-success ]] || fail "no success stamp"
grep -qx "GET /ping" /tmp/hook.log || fail "no heartbeat ping after success"

echo "== second run, weekly path"
sqlite3 "$DB" ".timeout 5000" "INSERT INTO items (name) VALUES ('marker');"
OPENSRO_BACKUP_WEEKLY_DAY="$(date +%u)" opensro-backup run
grep -q "Weekly backup check passed" /tmp/hook.log || fail "no weekly summary posted"
[[ "$(opensro-backup status | grep -c nightly)" -ge 2 ]] || fail "expected two snapshots"

echo "== restore"
kill "$WRITER"; wait "$WRITER" 2>/dev/null || true
opensro-backup restore latest /tmp/restored
RESTORED=/tmp/restored/var/backups/opensro/staging
[[ "$(sqlite3 "$RESTORED/state/shards/global-official/authority/state.db" "SELECT count(*) FROM items WHERE name='marker'")" == 1 ]] || fail "marker row missing"
grep -q secret "$RESTORED/state/cluster/agent-session-keys.json" || fail "secret file missing"
opensro-backup restore latest /tmp/restored 2>/dev/null && fail "restored into a non-empty directory"

echo "== failure alert"
mv "$DB" /tmp/db.moved
if opensro-backup run 2>/dev/null; then fail "run without databases succeeded"; fi
grep -q "Backup FAILED" /tmp/hook.log || fail "no failure alert posted"
grep -qx "GET /ping/fail" /tmp/hook.log || fail "no failure heartbeat"
mv /tmp/db.moved "$DB"

echo "== freshness alert"
echo $(( $(date +%s) - 30 * 3600 )) >/var/lib/opensro-backup/last-success
opensro-backup check-freshness
grep -q "30 hours old" /tmp/hook.log || fail "no stale alert posted"
opensro-backup check-freshness
[[ "$(grep -c "hours old" /tmp/hook.log)" == 1 ]] || fail "stale alert repeated within 12 hours"

echo "ALL BACKUP CHECKS PASSED"
