# Approved component releases

[Release status](https://opensro.online/releases/) shows live client and server
identities, pending candidates, deployment state and retained production history.
GitHub Actions owns approval; the status page is read-only.

Relevant pushes to `main` prepare candidates automatically. Builds and tests run
before approval. Approve the latest ready candidate for each component you want
to update: it includes preceding merged fixes, so intermediate releases do not
need to be deployed. A client-only change does not restart game services. A
server-only change does not replace the browser entry. Changes to shared release
controls can prepare both candidates, each with its own approval job.

The production job checks component inputs against current `main` after approval.
The host then checks the live generation under one shared deployment lock. Old
approvals fail even if a deployment and rollback returned to the same release.
New builds can cancel older builds, but never cancel an active production job.

## Candidate verification

`Release browser client` builds the application over the inspected live asset
manifest. It cannot change data files, data routes or asset schema. The staging
key creates an immutable preview and shared hashed application files without
changing `/play`. A cold HTTPS browser must pass title, login, roster, world,
inventory controls and authenticated reload using a dedicated non-GM account.
Its report identifies the exact archive and entry hash. Publication rehashes the
actual staged bytes, switches the client symlink atomically and checks `/play`
through HTTPS. A failed check restores and verifies the previous entry while
advancing the generation to invalidate the failed approval.

`Release game server` runs Linux Go and receiver tests, compares the compiled
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

Run `Roll back a production component` from `main`, select the component and
paste a full release identity from production history, with a reason. The host
rehashes retained production bytes and creates a new plan against today's live
generation. Client rollback repeats the browser smoke against today's server.
The production environment still requires approval. Database snapshots are
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

`Monitor production edge` runs outside the VPS on GitHub Actions every five
minutes and verifies HTTPS plus the host heartbeat. Its transition state survives
runs in an artifact. This catches loss of the host or its local monitor. GitHub
scheduled jobs can be delayed, so the external check is not a guaranteed paging
latency. Tests inject failures and notification delivery; they do not stop live
services or send fake public outages.

The production edge explicitly serves HTTP/1.1 and HTTP/2. Credential-free asset
requests selected HTTP/3 and repeatedly exceeded cold-load budgets on the
measured route. The TCP control completed identical ranges promptly. Browser
smoke uses ordinary browser settings and records negotiated worker protocols;
it does not rely on `--disable-quic`. The game transport is a proxied WebSocket.

## Host installation

Run the tested installer as root with an inspected live manifest, its source
commit, and separate Ed25519 staging and publication public keys:

```sh
python3 install.py --client-manifest /path/to/verified-live-client.json \
  --client-commit FULL_COMMIT --stage-key /path/to/stage.pub \
  --publish-key /path/to/publish.pub
```

The installer verifies live application and server hashes before initial ledger
creation. Reinstallation never resets generations. It installs root-owned forced
commands for `sro-stage` and `sro-release`, private candidate records, the public
status files, and the unprivileged monitor. Staging cannot publish; publication
cannot upload replacement bytes or create its own test receipt. The production
key is privileged and must remain behind required review.

Import `/etc/caddy/opensro-transport.caddy` before the site blocks. Import
`/etc/caddy/opensro-releases.caddy` inside the site's ordered route, before its
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
  "origin": "https://opensro.online",
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

## GitHub settings

Keep `production` restricted to `main`, with the operator as required reviewer
and administrator bypass disabled. Allow self-review when the same operator
initiates the workflow. Its existing publication secret names are retained for
compatibility and are shared by the two component publication jobs:

- `SERVER_DEPLOY_KEY`: the dedicated SSH private key, never the operator key.
- `SERVER_DEPLOY_HOST`: the production host address.
- `SERVER_DEPLOY_KNOWN_HOSTS`: its verified SSH host-key entry. Do not discover
  and trust a new key during the deployment job.

Create `release-staging`, restricted to `main`, with `RELEASE_STAGE_KEY`,
`RELEASE_HOST`, `RELEASE_KNOWN_HOSTS` and `RELEASE_PROBE_ACCOUNT`. The last secret
is JSON containing `username`, `password` and `character` for a dedicated account
with exactly one character. Do not use a player's account. `release-validation`
has no secrets and permits pull-request build tests.

Create `monitoring`, restricted to `main`, with `MONITOR_WEBHOOK`. It must identify
the intended server-status channel, not a similarly named backup/staff channel.
Website publication and full game-asset generation remain separate operations.

## Verification

```sh
pnpm task check:release
pnpm check source
pnpm --filter @sro/client-next check
```

Linux CI must run the real symlink and hard-link publication tests. Windows
accounts without symlink privilege skip those filesystem cases. Coverage includes
archive tampering, retained-byte rollback, stale approvals, compatibility,
credential roles, failed edge recovery, maintenance and notification retries.
