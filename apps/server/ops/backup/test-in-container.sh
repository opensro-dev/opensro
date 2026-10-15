#!/usr/bin/env bash
# ===========================================================================
#
# test-in-container.sh - exercise opensro-backup end to end on Ubuntu 24.04
#
#	docker run --rm -v "$PWD:/src:ro" ubuntu:24.04 bash /src/test-in-container.sh
#
# A local rclone remote stands in for Google Drive and a tiny HTTP server
# for the Discord webhook. A writer keeps inserting rows while the backup
# runs, as a live game would, into the authority store and into a
# .sqlite history journal. Checks: a verified nightly run, an incremental
# second run, restore contents (both databases snapshotted online and
# intact, no raw WAL copied), the weekly deep check, the failure alert,
# and the freshness alert.
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
# The operator history journal is a .sqlite file in WAL mode, written live
# through one connection the journal holds open, as the shard does. A
# connection that closes checkpoints and deletes the WAL, which would make a
# raw file copy look consistent; with checkpoints off and the connection
# open, committed rows live only in the WAL, so a raw copy is caught.
HISTORY="$STATE/shards/global-official/authority/history.sqlite"
sqlite3 "$HISTORY" "PRAGMA journal_mode=WAL; CREATE TABLE events (seq INTEGER PRIMARY KEY, data TEXT);"
mkfifo /tmp/history.in
sqlite3 "$HISTORY" </tmp/history.in &
HISTORY_DB=$!
exec 3>/tmp/history.in
echo "PRAGMA wal_autocheckpoint=0;" >&3
( while true; do echo "INSERT INTO events (data) VALUES (hex(randomblob(64)));"; sleep 0.01; done ) >&3 &
HISTORY_WRITER=$!

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
echo "INSERT INTO events (data) VALUES ('history-marker');" >&3
for _ in $(seq 100); do
	[[ "$(sqlite3 "$HISTORY" "SELECT count(*) FROM events WHERE data='history-marker'")" == 1 ]] && break
	sleep 0.1
done
[[ -s "$HISTORY-wal" ]] || fail "the history writer is not holding a WAL open"
OPENSRO_BACKUP_WEEKLY_DAY="$(date +%u)" opensro-backup run
grep -q "Weekly backup check passed" /tmp/hook.log || fail "no weekly summary posted"
[[ "$(opensro-backup status | grep -c nightly)" -ge 2 ]] || fail "expected two snapshots"

echo "== restore"
kill "$WRITER" "$HISTORY_WRITER"; wait "$WRITER" "$HISTORY_WRITER" 2>/dev/null || true
exec 3>&-
wait "$HISTORY_DB" 2>/dev/null || true
opensro-backup restore latest /tmp/restored
RESTORED=/tmp/restored/var/backups/opensro/staging
RESTORED_HISTORY="$RESTORED/state/shards/global-official/authority/history.sqlite"
# Before anything opens the restored journal: opening and closing it would
# checkpoint a raw WAL away.
[[ ! -e "$RESTORED_HISTORY-wal" && ! -e "$RESTORED_HISTORY-shm" ]] || fail "a raw WAL side file was backed up"
[[ "$(sqlite3 "$RESTORED/state/shards/global-official/authority/state.db" "SELECT count(*) FROM items WHERE name='marker'")" == 1 ]] || fail "marker row missing"
grep -q secret "$RESTORED/state/cluster/agent-session-keys.json" || fail "secret file missing"
[[ "$(sqlite3 "$RESTORED_HISTORY" "SELECT count(*) FROM events WHERE data='history-marker'")" == 1 ]] || fail "history marker row missing"
[[ "$(sqlite3 "$RESTORED_HISTORY" "PRAGMA integrity_check;")" == "ok" ]] || fail "restored history journal is corrupt"
opensro-backup restore latest /tmp/restored 2>/dev/null && fail "restored into a non-empty directory"

echo "== failure alert"
mv "$DB" /tmp/db.moved
mv "$HISTORY" /tmp/history.moved
if opensro-backup run 2>/dev/null; then fail "run without databases succeeded"; fi
grep -q "Backup FAILED" /tmp/hook.log || fail "no failure alert posted"
grep -qx "GET /ping/fail" /tmp/hook.log || fail "no failure heartbeat"
mv /tmp/db.moved "$DB"
mv /tmp/history.moved "$HISTORY"

echo "== freshness alert"
echo $(( $(date +%s) - 30 * 3600 )) >/var/lib/opensro-backup/last-success
opensro-backup check-freshness
grep -q "30 hours old" /tmp/hook.log || fail "no stale alert posted"
opensro-backup check-freshness
[[ "$(grep -c "hours old" /tmp/hook.log)" == 1 ]] || fail "stale alert repeated within 12 hours"

echo "ALL BACKUP CHECKS PASSED"
