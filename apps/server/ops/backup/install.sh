#!/usr/bin/env bash
# ===========================================================================
#
# install.sh - install opensro-backup on a host (run as root, idempotent)
#
# Installs the tools and units and generates the restic password when
# absent. It does not connect Google Drive or set the Discord webhook: an
# operator does those once by hand (README.md, "Setup"), so no credential
# ever passes through a script or a log.
#
# ===========================================================================
set -Eeuo pipefail
umask 077

HERE="$(cd "$(dirname "$0")" && pwd)"
readonly HERE
readonly CONFIG_DIR=/etc/opensro-backup

[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }

apt-get install -y --no-install-recommends restic rclone sqlite3 rsync python3 curl >/dev/null
install -m 0755 "$HERE/opensro-backup.sh" /usr/local/sbin/opensro-backup
install -d -m 0700 "$CONFIG_DIR"
[[ -f "$CONFIG_DIR/backup.conf" ]] || install -m 0600 "$HERE/backup.conf.example" "$CONFIG_DIR/backup.conf"
if [[ ! -s "$CONFIG_DIR/restic-password" ]]; then
	head -c 32 /dev/urandom | base64 >"$CONFIG_DIR/restic-password"
	chmod 0600 "$CONFIG_DIR/restic-password"
	echo "generated $CONFIG_DIR/restic-password: copy it OFF this server now (password manager)."
fi
for unit in opensro-backup.service opensro-backup.timer opensro-backup-freshness.service opensro-backup-freshness.timer; do
	install -m 0644 "$HERE/$unit" "/etc/systemd/system/$unit"
done
systemctl daemon-reload
systemctl enable --now opensro-backup.timer opensro-backup-freshness.timer
echo "installed. Next: connect Google Drive and the webhook (README.md, Setup), then: opensro-backup run"
