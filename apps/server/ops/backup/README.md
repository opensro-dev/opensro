# Backups

`opensro-backup` keeps encrypted, verified, off-host copies of everything a
rebuilt host cannot get back from git. It covers the game databases, the
account authority, cluster secrets, the website database, and the host's
service configuration. Code and release files are not backed up; they
come from GitHub.

| Where | What | How often |
| --- | --- | --- |
| Google Drive (`gdrive:opensro-backups`) | restic repository: encrypted, deduplicated, incremental | nightly, 20:30 UTC |
| An operator's PC | a copy of that repository, add-only | whenever `pull-backups.ps1` runs |

Retention is 7 daily, 4 weekly and 6 monthly snapshots.

Every run does the following:
1. It copies each SQLite database online (players stay connected) and checks its integrity.
2. It uploads the copy and prunes old snapshots.
3. It **restores the new snapshot** to a scratch directory and compares every file's hash.

On Sundays it also reads back 10% of the stored data and posts a summary to
the staff Discord channel. A failed run, or no good backup for 26 hours,
posts an alert.

## Setup (once per host, as root)

1. Install the tools, units and a fresh restic password:

   ```sh
   sudo ./install.sh
   ```

   Copy `/etc/opensro-backup/restic-password` off the server (password
   manager). Without it the backups cannot be read.

2. Connect Google Drive. The server has no browser, so authorize on a PC
   with rclone installed (`winget install Rclone.Rclone`):

   ```sh
   rclone authorize "drive" --drive-scope drive.file
   ```

   Sign in with the Google account that will hold the backups. rclone prints
   a token. On the server:

   ```sh
   sudo rclone config create gdrive drive scope=drive.file token='<paste the token JSON>' \
     --config /etc/opensro-backup/rclone.conf
   sudo chmod 600 /etc/opensro-backup/rclone.conf
   ```

   `drive.file` lets rclone see only the files it created, not the rest
   of the Drive.

   rclone's built-in Google client ID is shared by everyone, rate limited,
   and being retired during 2026. Create your own (free; see
   https://rclone.org/drive/#making-your-own-client-id) and add
   `client_id=... client_secret=...` to the `config create` line above.
   Re-run step 2 on the server and on the PC with it before the shared one
   stops working.

3. Alerts (optional): create a webhook in the private staff Discord
   channel (Channel settings → Integrations → Webhooks), then:

   ```sh
   sudo sh -c 'umask 077; cat > /etc/opensro-backup/discord-webhook'   # paste the URL, Enter, Ctrl-D
   ```

4. First run, then check:

   ```sh
   sudo opensro-backup run && sudo opensro-backup status
   ```

## The copy on a PC

Set up the same `gdrive` remote in rclone on the PC (`rclone config`, drive,
scope `drive.file`), then run or schedule:

```powershell
powershell -ExecutionPolicy Bypass -File pull-backups.ps1
```

To run it daily, register a scheduled task:
`schtasks /Create /SC DAILY /ST 12:00 /TN "OpenSRO backup pull" /TR "powershell -ExecutionPolicy Bypass -File <path>\pull-backups.ps1"`.

## Restore

Restoring never overwrites anything by itself. First, get the files out:

```sh
sudo opensro-backup status                          # pick a snapshot, or use latest
sudo opensro-backup restore latest /root/restore    # -> /root/restore/var/backups/opensro/staging
```

Or from the PC copy: `restic -r <path to the copy> restore latest --target <dir>`.

The restored tree has three parts:
- `state/`: the game state root. It holds `shards/*/authority/state.db`,
  `cluster/*` (accounts, keys) and `cluster/agent/*`.
- `host/`: `/etc/caddy`, `/etc/nomad.d`, systemd units, `/etc/opensro-web`,
  and Caddy's certificates.
- `postgres/`: `pg_dump -Fc` files, restored with
  `pg_restore --clean --if-exists -d <db> <file>`.

To put state back on a host:
1. Stop the fleet (`sro-nomad stop`).
2. Copy `state/` over `STATE_ROOT`, keeping owners (`chown -R sro:sro` on the
   `agent` and `authority` directories).
3. Deploy.
