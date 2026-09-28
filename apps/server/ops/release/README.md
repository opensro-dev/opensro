# Component release tools for self-hosted servers

This directory owns reusable candidate validation, component publication,
rollback, monitoring and Linux installation tools. It contains no credentials
and does not own the OpenSRO production fleet. Host configuration, production
workflows, approval policy and monitoring destinations belong in the operator's
private infrastructure repository. OpenSRO uses `opensro-dev/opensro-ops`.

After installation, `/releases/` on your origin shows live component identities,
pending candidates, deployment state and retained production history. The page
is read-only. Your private automation must own approval and credential access.

Configure your automation to build and test candidates before approval.
Approve the latest ready candidate for each component you want
to update: it includes preceding merged fixes, so intermediate releases do not
need to be deployed. A client-only change does not restart game services. A
server-only change does not replace the browser entry. Changes to shared release
controls can prepare both candidates, each with its own approval job.

The production job checks component inputs against current `main` after approval.
The host then checks the live generation under one shared deployment lock. Old
approvals fail even if a deployment and rollback returned to the same release.
New builds can cancel older builds, but never cancel an active production job.

## Candidate verification

The browser candidate builder uses the inspected live asset
manifest. It cannot change data files, data routes or asset schema. The staging
key creates an immutable preview and shared hashed application files without
changing `/play`. A cold HTTPS browser must pass title, login, roster, world,
inventory controls and authenticated reload using a dedicated non-GM account.
Its report identifies the exact archive and entry hash. Publication rehashes the
actual staged bytes, switches the client symlink atomically and checks `/play`
through HTTPS. A failed check restores and verifies the previous entry while
advancing the generation to invalidate the failed approval.

Server build automation must run Linux Go and receiver tests, compare the compiled
protocol/schema report with `compatibility.json`, and builds static binaries.
The staging job retains those exact bytes. After approval the receiver:

1. Rechecks production identity and compatibility and retains verified inputs.
2. Requires the configured Nomad version and completes a verified remote backup.
3. Issues a one-hour `sro-deployer` token and validates deployment inputs.
4. Announces the restart in game and on Discord, waits two minutes, then asks
   Nomad to deploy and verifies fleet health.
5. Records the healthy release and revokes the temporary token. A cleanup
   failure after successful health checks records a warning without claiming
   that the healthy deployment failed.

## Rollback and interrupted operations

Use `ci.py rollback COMPONENT RELEASE --reason REASON` with the staging role.
Select a full release identity from production history. The host
rehashes retained production bytes and creates a new plan against today's live
generation. Client rollback repeats the browser smoke against today's server.
Publication still requires your approval policy. Database snapshots are
never restored by this workflow, schema downgrade is rejected, and a server
cannot abandon protocols accepted by existing browser tabs.

Nomad owns per-job health checks and auto-reversion; deployment is not an atomic
transaction across two services and their databases. A failed or interrupted
operation remains in `production.json` and blocks subsequent publication. Do not
clear that journal merely to retry. Inspect the Actions log, Nomad job versions,
running allocation identities, database schema and the actual client symlink.
Reconcile the ledger only after those agree with a known, healthy release. If
restoring client routing cannot be verified, the journal says
`rollback-unverified` and also blocks publication.

The ledger retains 32 previous component records. Host candidate archives and
verified rollback inputs are kept independently of GitHub's 30-day artifacts;
never remove an artifact referenced by live state, history or a pending approval.

## Availability notifications

`opensro-monitor.timer` checks the public fleet every 30 seconds, independently
of the game processes. Two consecutive failures or recoveries trigger a message
to the configured server-status webhook. The heartbeat is persisted before
delivery; acknowledgement is persisted only after Discord accepts it, so failed
delivery is retried. Maintenance suppression expires after ten minutes. The
monitor also reports completion of observed maintenance.

Run `monitor.py --edge-workflow TEMP_DIRECTORY --origin https://game.example.com`
outside the game host, persisting `TEMP_DIRECTORY/edge/state.json` between runs.
Supply `MONITOR_WEBHOOK` through your secret store. This catches loss of the host
or its local monitor. GitHub
scheduled jobs can be delayed, so the external check is not a guaranteed paging
latency. Tests inject failures and notification delivery; they do not stop live
services or send fake public outages.

Browser smoke uses ordinary browser settings and records negotiated worker
protocols. Choose edge transport settings from measurements on your infrastructure.
The game transport is a proxied WebSocket.

## Host installation

Run the tested installer as root with an inspected live manifest, its source
commit, and separate Ed25519 staging and publication public keys:

```sh
python3 install.py --client-manifest /path/to/verified-live-client.json \
  --client-commit FULL_COMMIT --stage-key /path/to/stage.pub \
  --publish-key /path/to/publish.pub --source-commit CONTROLS_SOURCE_COMMIT
```

The installer verifies live application and server hashes before initial ledger
creation. Reinstallation never resets generations. It first installs and hashes
a complete immutable control version, then binds the root-owned entry points to
that directory. Existing processes cannot mix old and new module bytes. The
installed manifest records the source commit and each control file's digest;
reinstalling that commit rejects drift instead of overwriting it.
It installs root-owned forced
commands for `sro-stage` and `sro-release`, private candidate records, the public
status files, and the unprivileged monitor. Staging cannot publish; publication
cannot upload replacement bytes or create its own test receipt. The production
key is privileged and must remain behind required review.

Import `/etc/caddy/opensro-releases.caddy` inside the site's ordered route, before its
normal client handler. Validate the complete Caddy configuration before reload.
The installer does not rewrite an existing edge configuration automatically.

Store the host-specific configuration in the private operations repository
and install it as root-only `/etc/opensro-release/config.json`:

```json
{
  "module": "/opt/opensro/apps/server",
  "game_data": "/opt/opensro/.generated/game-data/1.150/server.srogz",
  "nomad_bootstrap": "/root/opensro-secrets/nomad-bootstrap.json",
  "nomad_version": "2.0.7",
  "origin": "https://game.example.com",
  "shard": "global-official",
  "agent_memory_mb": 256,
  "gameworld_memory_mb": 1024,
  "public_webhook": "/etc/opensro-release/discord-announcements-webhook",
  "staff_webhook": "/etc/opensro-backup/discord-webhook"
}
```

Webhook URLs and private keys never enter git. Include `/etc/opensro-release`
in the encrypted host backup. Retain the management token root-only; it is
used only to issue and revoke the scoped token and never enters the deployer's
environment. Task processes continue to run as `sro` under Nomad.

For the first upgrade from a server without the notice endpoint, the operator
may set `bootstrap_notice: true`. This permits installation only when the
endpoint is absent and all listed shards have zero connected players, checked
before and after the public warning window. It cannot bypass a failed existing
notice endpoint. A successful deployment removes this setting. The first
upgrade cannot show an in-game notice on a server that lacks that capability.

## Automation and approval

Keep production credentials restricted to your reviewed publication workflow.
If using GitHub environments, verify that your plan supports required reviewers
for the repository's visibility before storing credentials. An environment name
alone provides no approval protection.

Run the tools from a checkout of the public game source. A private workflow's
`GITHUB_SHA` identifies infrastructure code, not the game artifact. Pass the
reviewed game commit explicitly to `ci.py publish COMPONENT CANDIDATE --commit SHA`.
The tool compares it with the game checkout's `origin/main` before publication.

`ci.py` consumes these environment variables for its selected role:

- `RELEASE_KEY`: the dedicated role's SSH private key, never the operator key.
- `RELEASE_HOST`: your host address.
- `RELEASE_KNOWN_HOSTS`: its verified SSH host-key entry. Do not discover
  and trust a new key during the deployment job.

Keep staging and publication credentials separate. Browser smoke also requires
`RELEASE_ORIGIN` and `RELEASE_PROBE_ACCOUNT`. The latter secret is JSON containing
`username`, `password`, `character` and `shard` for a dedicated account
with exactly one character. Do not use a player's account. Pull-request build
tests must have no production credentials.

Keep `MONITOR_WEBHOOK` in your private monitoring automation. It must identify
the intended server-status channel, not a similarly named backup/staff channel.
Website publication and full game-asset generation remain separate operations.

## Verification

```sh
pnpm task check:release
pnpm check source
pnpm --filter @sro/client-next check
```

Linux CI must run the real symlink and hard-link publication tests. Windows
skips these POSIX publication cases because its directory-symlink replacement
semantics differ, even when the account can create symlinks. Coverage includes
archive tampering, retained-byte rollback, stale approvals, compatibility,
credential roles, failed edge recovery, maintenance and notification retries.
