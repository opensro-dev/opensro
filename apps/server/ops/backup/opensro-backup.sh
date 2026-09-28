#!/usr/bin/env bash
# ===========================================================================
#
# opensro-backup.sh - encrypted, verified, off-host backups of a live host
#
# Everything a rebuilt host cannot get back from git: the game databases,
# the account authority, cluster secrets, the website database, and the
# host's service configuration. Databases are copied with SQLite's online
# backup (and pg_dump), so players stay connected while it runs.
#
# The copies go into a restic repository (encrypted, deduplicated,
# incremental) on an rclone remote, then the snapshot is restored into a
# scratch directory and checked: a backup that has never been restored is a
# hope, not a backup.
#
#	opensro-backup run              nightly: snapshot, upload, prune, verify
#	opensro-backup check-freshness  hourly: alert when no good backup in 26 h
#	opensro-backup status           list snapshots and the last good run
#	opensro-backup restore ID DIR   restore snapshot ID (or "latest") into DIR
#
# Configuration: /etc/opensro-backup/backup.conf (see backup.conf.example).
# Secrets live beside it, root-only, never in git. An optional outside
# heartbeat (healthcheck-url) catches the host itself going down.
#
# ===========================================================================
set -Eeuo pipefail
umask 077

readonly CONFIG_DIR="${OPENSRO_BACKUP_CONFIG_DIR:-/etc/opensro-backup}"
readonly WORK_DIR="/var/backups/opensro"
readonly STAGING="$WORK_DIR/staging"
readonly VERIFY="$WORK_DIR/verify"
readonly STATUS_DIR="/var/lib/opensro-backup"
readonly LAST_SUCCESS="$STATUS_DIR/last-success"
readonly LAST_ALERT="$STATUS_DIR/last-stale-alert"
readonly LOCK_FILE="/run/opensro-backup.lock"
readonly STALE_AFTER_SECONDS=$((26 * 3600))
readonly STALE_REALERT_SECONDS=$((12 * 3600))
# ISO weekday of the deep check and summary: Sunday (overridable for tests).
readonly WEEKLY_DAY="${OPENSRO_BACKUP_WEEKLY_DAY:-7}"

# ===========================================================================
# Configuration and helpers
# ===========================================================================

# ================
# load_config
#
# backup.conf sets STATE_ROOT, RCLONE_REMOTE, the KEEP_* retention counts,
# EXTRA_PATHS, SQLITE_PATHS and POSTGRES_DATABASES. restic reads its password from a file
# so it never appears in the environment of other processes' listings.
# ================
load_config() {
	# shellcheck source=/dev/null
	source "$CONFIG_DIR/backup.conf"
	: "${STATE_ROOT:?STATE_ROOT is not set}"
	: "${RCLONE_REMOTE:?RCLONE_REMOTE is not set}"
	KEEP_DAILY="${KEEP_DAILY:-7}"
	KEEP_WEEKLY="${KEEP_WEEKLY:-4}"
	KEEP_MONTHLY="${KEEP_MONTHLY:-6}"
	export RESTIC_REPOSITORY="rclone:$RCLONE_REMOTE"
	export RESTIC_PASSWORD_FILE="$CONFIG_DIR/restic-password"
	export RCLONE_CONFIG="$CONFIG_DIR/rclone.conf"
	export RESTIC_CACHE_DIR="/var/cache/opensro-backup"
}

# ================
# log
# ================
log() {
	printf '%s opensro-backup: %s\n' "$(date -u +%FT%TZ)" "$*"
}

# ================
# notify
#
# Posts one line to the staff Discord channel. Missing webhook or a failed
# post never fails the backup itself; the journal still has the message.
# ================
notify() {
	local message="$1" webhook_file="$CONFIG_DIR/discord-webhook"
	log "$message"
	[[ -s "$webhook_file" ]] || return 0
	local payload
	payload=$(printf '%s' "**$(hostname)** · $message" | python3 -c 'import json,sys; print(json.dumps({"content": sys.stdin.read()[:1900]}))')
	curl --silent --show-error --max-time 20 -H 'Content-Type: application/json' \
		-d "$payload" "$(cat "$webhook_file")" >/dev/null || log "Discord notification failed"
}

# ================
# heartbeat
#
# Pings the outside dead man's switch (healthchecks.io or compatible) with
# the given suffix ("" for success, "/fail"). The freshness check below
# runs on this host and cannot report the host itself being down; the
# outside service alerts when the success ping stops arriving.
# ================
heartbeat() {
	local url_file="$CONFIG_DIR/healthcheck-url"
	[[ -s "$url_file" ]] || return 0
	curl --silent --show-error --max-time 20 --retry 3 "$(cat "$url_file")$1" >/dev/null || log "heartbeat ping failed"
}

# ================
# on_error
# ================
on_error() {
	local line="$1"
	heartbeat /fail
	notify "❌ Backup FAILED (line $line of opensro-backup). Check: journalctl -u opensro-backup"
}

# ================
# ensure_repository
#
# Creates the restic repository on first use.
# ================
ensure_repository() {
	if ! restic cat config >/dev/null 2>&1; then
		log "initializing restic repository $RESTIC_REPOSITORY"
		restic init
	fi
}

# ===========================================================================
# Collect
# ===========================================================================

# ================
# copy_sqlite
#
# Online backup of one live SQLite database, then an integrity check of
# the copy: a corrupt copy fails the run before anything is uploaded.
# ================
copy_sqlite() {
	local source="$1" target="$2"
	mkdir -p "$(dirname "$target")"
	sqlite3 "$source" ".timeout 10000" ".backup '$target'"
	local verdict
	verdict=$(sqlite3 "$target" "PRAGMA integrity_check;")
	[[ "$verdict" == "ok" ]] || { log "integrity check failed for $source: $verdict"; return 1; }
}

# ================
# collect
#
# Fills STAGING with this night's copy. Databases are backed up online;
# other state files and the host configuration are copied as they are.
# ================
collect() {
	rm -rf "$STAGING"
	mkdir -p "$STAGING/state" "$STAGING/host" "$STAGING/postgres"

	# Game state: every SQLite database under the state root, online.
	local db count=0
	while IFS= read -r -d '' db; do
		copy_sqlite "$db" "$STAGING/state/${db#"$STATE_ROOT"/}"
		count=$((count + 1))
	done < <(find "$STATE_ROOT" -type f -name '*.db' -print0)
	[[ $count -gt 0 ]] || { log "no databases found under $STATE_ROOT"; return 1; }

	# Everything else in the state root (secrets, catalogs, leases, certs),
	# skipping the databases already copied and their WAL side files.
	rsync -a --exclude='*.db' --exclude='*.db-wal' --exclude='*.db-shm' --exclude='releases/' \
		"$STATE_ROOT/" "$STAGING/state/"

	# Host configuration (Caddy, Nomad, systemd units, website env).
	local path
	for path in ${EXTRA_PATHS:-}; do
		[[ -e "$path" ]] || continue
		mkdir -p "$STAGING/host$(dirname "$path")"
		cp -a "$path" "$STAGING/host$path"
	done

	# Companion SQLite stores may live outside the game state root. Never
	# copy a live database and WAL separately through EXTRA_PATHS.
	for db in ${SQLITE_PATHS:-}; do
		[[ "$db" == /* && -f "$db" ]] || { log "required companion database missing: $db"; return 1; }
		copy_sqlite "$db" "$STAGING/host$db"
		count=$((count + 1))
	done

	# PostgreSQL databases (custom format, restorable with pg_restore). A
	# listed database is required: a missing one fails the run rather than
	# silently leaving the website out of the backup.
	local database
	for database in ${POSTGRES_DATABASES:-}; do
		# pg_dump runs as postgres; root (this script) writes the file.
		# shellcheck disable=SC2024
		sudo -u postgres pg_dump -Fc "$database" >"$STAGING/postgres/$database.dump"
	done

	(cd "$STAGING" && find . -type f -print0 | sort -z | xargs -0 sha256sum >"$WORK_DIR/staging.sha256")
	log "collected $count database(s), $(du -sh "$STAGING" | cut -f1) in total"
}

# ===========================================================================
# Commands
# ===========================================================================

# ================
# verify_latest
#
# Restores the snapshot just taken and proves it matches what was staged:
# every file's hash, and every database's integrity.
# ================
verify_latest() {
	rm -rf "$VERIFY"
	mkdir -p "$VERIFY"
	restic restore latest --host "$(hostname)" --tag nightly --target "$VERIFY" >/dev/null
	(cd "$VERIFY$STAGING" && sha256sum --quiet -c "$WORK_DIR/staging.sha256")
	local db
	while IFS= read -r -d '' db; do
		[[ "$(sqlite3 "$db" 'PRAGMA integrity_check;')" == "ok" ]] || { log "restored $db is corrupt"; return 1; }
	done < <(find "$VERIFY" -type f \( -name '*.db' -o -name '*.sqlite3' \) -print0)
	rm -rf "$VERIFY"
}

# ================
# cmd_run
# ================
cmd_run() {
	exec 9>"$LOCK_FILE"
	flock -n 9 || { log "another backup is running"; exit 0; }
	trap 'on_error $LINENO' ERR
	mkdir -p "$WORK_DIR" "$STATUS_DIR" "$RESTIC_CACHE_DIR"

	ensure_repository
	collect
	restic backup "$STAGING" --host "$(hostname)" --tag nightly --one-file-system --quiet
	restic forget --host "$(hostname)" --tag nightly --prune --quiet \
		--keep-daily "$KEEP_DAILY" --keep-weekly "$KEEP_WEEKLY" --keep-monthly "$KEEP_MONTHLY"
	verify_latest

	if [[ "$(date +%u)" == "$WEEKLY_DAY" ]]; then
		# Weekly: read back a tenth of the stored data to catch silent rot.
		restic check --read-data-subset=10% --quiet
		local snapshots size
		snapshots=$(restic snapshots --host "$(hostname)" --tag nightly --json | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')
		size=$(restic stats --mode raw-data --json | python3 -c 'import json,sys; print(round(json.load(sys.stdin)["total_size"]/1048576, 1))')
		notify "✅ Weekly backup check passed: $snapshots snapshots, ${size} MiB stored, latest restored and verified."
	fi

	date -u +%s >"$LAST_SUCCESS"
	heartbeat ""
	rm -rf "$STAGING"
	log "backup complete and verified"
}

# ================
# cmd_check_freshness
#
# Catches the failure the run itself cannot report: the timer not firing,
# the host having been down, or a run hanging.
# ================
cmd_check_freshness() {
	mkdir -p "$STATUS_DIR"
	local now last age alerted
	now=$(date -u +%s)
	last=$(cat "$LAST_SUCCESS" 2>/dev/null || echo 0)
	age=$((now - last))
	((age > STALE_AFTER_SECONDS)) || return 0
	alerted=$(cat "$LAST_ALERT" 2>/dev/null || echo 0)
	((now - alerted > STALE_REALERT_SECONDS)) || return 0
	if ((last == 0)); then
		notify "⚠️ No successful backup has ever been recorded on this host."
	else
		notify "⚠️ Last good backup is $((age / 3600)) hours old."
	fi
	echo "$now" >"$LAST_ALERT"
}

# ================
# cmd_status
# ================
cmd_status() {
	local last
	last=$(cat "$LAST_SUCCESS" 2>/dev/null || echo 0)
	if ((last == 0)); then echo "last good backup: never"; else echo "last good backup: $(date -u -d "@$last" +%FT%TZ)"; fi
	restic snapshots --host "$(hostname)" --tag nightly
}

# ================
# cmd_restore
#
# Restores into an empty directory; putting files back in place is a
# deliberate, separate step (see README.md, "Restore").
# ================
cmd_restore() {
	local snapshot="${1:?usage: opensro-backup restore SNAPSHOT|latest TARGET_DIR}"
	local target="${2:?usage: opensro-backup restore SNAPSHOT|latest TARGET_DIR}"
	if [[ -e "$target" ]] && [[ -n "$(ls -A "$target")" ]]; then
		echo "refusing to restore into non-empty $target" >&2
		exit 2
	fi
	mkdir -p "$target"
	restic restore "$snapshot" --target "$target"
	echo "restored into $target$STAGING (state/, host/, postgres/)"
}

main() {
	[[ $EUID -eq 0 ]] || { echo "opensro-backup must run as root" >&2; exit 1; }
	load_config
	case "${1:-}" in
		run) cmd_run ;;
		check-freshness) cmd_check_freshness ;;
		status) cmd_status ;;
		restore) shift; cmd_restore "$@" ;;
		*) echo "usage: opensro-backup run|check-freshness|status|restore SNAPSHOT DIR" >&2; exit 2 ;;
	esac
}

main "$@"
