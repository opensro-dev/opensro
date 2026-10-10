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
controls can prepare both candidates, each with its own approval job. A change
to the browser protocol is the exception: it publishes both components as one
coordinated release (below).

The production job checks component inputs against current `main` after approval.
The host then checks the live generation under one shared deployment lock. Old
approvals fail even if a deployment and rollback returned to the same release.
New builds can cancel older builds, but never cancel an active production job.

## Candidate verification

The browser candidate builder uses the inspected live asset
manifest. Such an application candidate cannot change data files, data routes or
asset schema; a data candidate (below) can. The staging
key creates an immutable preview and shared hashed application files without
changing `/play`. The release gate (`apps/client-next/tools/beta/release-gate.mjs`)
must pass within 30 seconds using a dedicated non-GM account: a browser boots
the staged candidate (title), and the release's own simulation worker runs
headlessly through login, roster, world entry, a server-confirmed move
(gameplay) and a session-restoring reload (resume). No scene is rendered, so
the gate needs no GPU and does not slow down as players come into view. The
rendered browser flow (`release-smoke.mjs`) remains a diagnostic probe for
machines with a GPU. The report identifies the exact archive and entry hash. Publication rehashes the
actual staged bytes, switches the client symlink atomically and checks `/play`
through HTTPS. A failed check restores and verifies the previous entry while
advancing the generation to invalidate the failed approval.

Server build automation must run Linux Go and receiver tests, compare the compiled
protocol/schema report with `compatibility.json`, and builds static binaries.
The staging job retains those exact bytes. After approval the receiver:

1. Rechecks production identity and compatibility and retains verified inputs.
2. Requires the configured Nomad version and completes a verified remote backup.
3. Issues a one-hour `sro-deployer` token and validates deployment inputs.
4. Announces the restart in game and on Discord and waits two minutes. When
   the candidate cannot open the live database schema but declares an offline
   upgrade from it (`storeUpgradeFrom` in `compatibility.json`, reported by
   `sro-release-contract`), the receiver stops the fleet and runs the
   release's own `sro-authority-upgrade` on every enabled shard, as the
   database owner: it validates, keeps a backup beside the database
   (`state.before-mall-*.db`) and commits. Then it asks Nomad to deploy and
   verifies fleet health.
5. Records the healthy release and revokes the temporary token. A cleanup
   failure after successful health checks records a warning without claiming
   that the healthy deployment failed.

## Data releases

A release that changes the asset data (a new `ASSET_SCHEMA` in
`scripts/build/assetSchema.mjs`, or any new pack content) is a data candidate
(`client_data.py`). Only the machine holding the licensed asset build can
produce it, so the operator stages it with `data_release.py`:

```sh
python data_release.py PACKAGE OUTPUT --origin https://game.example.com   --ssh-target sro-stage@host --identity ~/.ssh/operator-stage [--coordinated] [--max-batch-mib 16]
```

On a slow link (a VPN), `--max-batch-mib` makes smaller batches. Each
upload's time limit grows with its size (at least 32 KiB/s), and a timed-out
upload is retried like any other transport failure.

It reads the live release from the origin, asks the host which of the needed
payloads its store already holds intact (`payload-inventory`: length and sha256
match; each hit is kept from the 14-day prune), and uploads, in payload batches
under the upload limit, only the content neither the live release nor the store
has (keyed by sha256). The host's controls must include `payload-inventory`:
after a release-controls change, reinstall them (Host installation) before the
next data release. Older controls refuse the inventory, and `data_release.py`
stops with that message rather than uploading everything.
The host verifies and stores each batch, then stages the candidate: every
served file the live release already has is hard-linked, the rest is written
from the store, and the sha256 of every served file is recorded. Publication
re-verifies the whole served tree against that record. Interrupted uploads
resume; stored payloads unused for 14 days are pruned.

### Server game data

The GameWorld opens the licensed server game-data archive at the host's
`game_data` path; a server release carries binaries, not that archive
(`server_data.py`). When a server change needs newer data (a new catalogue
version, a new authority file), stage the archive built from the release's
commit, with or without a client package:

```sh
python data_release.py [PACKAGE OUTPUT] --server-data apps/server/.generated/game-data/1.150/server.srogz   --origin https://game.example.com --ssh-target sro-stage@host --identity ~/.ssh/operator-stage
```

Every server publication runs the release's own `sro-game-data-check` on
the archive it will open before the backup, the notice or any stop: a
staged archive must pass, else the installed one; a revert whose release
refuses the installed archive uses the newest retained archive it accepts.
A refusal leaves the running server untouched and says what to stage. The
chosen archive is installed inside the maintenance window, unpacked as
root, and the replaced one is retained by digest (three are kept).

A publication onto a fleet whose GameWorld processes are all down skips the
notice and its wait: nobody is connected to warn. Each publication also
writes `maintenance-servers.json` (every shard, none operating) beside
`production.json`; the edge serves it for `/api/title/servers` while the
Agent is down, so the title lists each shard as "Check".

## Coordinated releases

A new browser protocol (`releaseprotocol.Current` in the server,
`RELEASE_PROTOCOL` in the client) cannot move one component at a time. Build
both candidates with `--coordinated` (`bundle.py`, `client_bundle.py` or
`data_release.py`); staging checks each half for its own rules only, and
neither can be published alone. After approval:

1. `ci.py coordinate SERVER CLIENT --commit SHA` checks both sources against
   `main`, then the host admits the pair together, retains both live
   releases, deploys the server (notice, backup, Nomad health) and switches
   the client. The journal stays open in `verifying`.
2. The workflow runs the browser smoke against the now-live pair and records
   its evidence with the staging key (`ci.py evidence client REPORT`), as for
   every candidate: the publication key never writes a test receipt.
3. `ci.py confirm` requires that evidence, recorded after the switch, and
   advances both generations; on failure, `ci.py revert --reason REASON`
   restores the retained client and redeploys the retained server without a
   notice.

A new protocol is any change the live counterpart cannot decode, including
new bytes in an existing packet: a field added, a length changed, a layout
version raised. Those need a protocol bump and a coordinated release. A new
opcode or a new JSON key is not one; the browser client ignores both. Before
publishing one component alone, diff the other component's decoders between
its live commit and the candidate, and pair every changed decoder with the
encoder that forced it.

Open tabs of the old client are refused with HTTP 426 and told to reload. A
plain pair is admitted only when the old server can read what the new one
writes, so its revert never needs a database restore. A store upgrade that
must move with a new protocol ships as a maintenance pair (below); otherwise
it ships as a server release on its own. A server rollout that fails its own
health checks leaves the journal `failed`; revert is the recovery. While the
journal is open, every other publication is refused.

## Maintenance releases

A maintenance release is a coordinated pair published inside a closed
window, so it may also upgrade the persisted schema. Build both candidates
with `--maintenance` (`bundle.py`, `client_bundle.py` or `data_release.py`;
it implies `--coordinated`), and publish with
`ci.py coordinate SERVER CLIENT --commit SHA --maintenance`, which refuses
unless both staged candidates declare it. Admission keeps every rule of a
pair except one: the live server need not read the new state, because the
revert restores it. The server must still take the live schema over through
its offline upgrade, and nothing may downgrade it.

The host configuration must name the accounts that may still play during
the window, normally the release probe account:
`"maintenance_accounts": ["<probe username>"]`. A maintenance publication
without them is refused before it changes anything.

1. `publish` closes the gate: `<module>/.state/cluster/maintenance-gate.json`
   lists those accounts, and every GameWorld then mints EnterWorld tokens for
   them alone (`SRO_MAINTENANCE_GATE_PATH`, set by `sro-nomad`). It then
   announces, backs up, stops the fleet and runs the server's offline upgrade
   on every enabled shard. Each shard is journaled before its commit, then
   with the backup the upgrade kept and its sha256, before the new server
   deploys and the client switches.
2. The release gate runs as the probe account against the closed window, and
   its evidence is recorded as for any pair.
3. `confirm` records the release, then removes the gate: players refresh and
   play on the new pair.
4. `revert`, at any point before confirm, checks every journaled backup
   against its sha256, stops the fleet, keeps each upgraded database as
   `state.failed-<time>.db`, restores the backup, redeploys the retained
   server and client, and only then removes the gate. Nobody but the probe
   played on the new state, so the restore loses nothing. A backup that is
   missing or changed, or a shard journaled without one, refuses the restore
   and leaves the journal `revert-failed` with the gate closed for an
   operator.

After confirm, no workflow crosses the schema back; that takes restoring
the upgrade's backup by hand. After any manual recovery, reopen the game by
deleting `<module>/.state/cluster/maintenance-gate.json` (as root); the
GameWorlds read it at every EnterWorld token, so no restart is needed.
`maintenance_accounts` lists login usernames (case-insensitive), normally
the probe account the release gate signs in with.

## Rollback and interrupted operations

Use `ci.py rollback COMPONENT RELEASE --reason REASON` with the staging role.
Select a full release identity from production history. The host
rehashes retained production bytes and creates a new plan against today's live
generation. Client rollback repeats the browser smoke against today's server.
Publication still requires your approval policy. Database snapshots are
never restored by this workflow, schema downgrade is rejected, and a server
published alone cannot abandon protocols accepted by existing browser tabs.

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
commit, and separate Ed25519 staging and publication public keys. The staging
key file may list several keys (CI, and the workstation that stages data
releases); each is confined to the same forced command:

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
  "stack_sizes": "",
  "public_webhook": "/etc/opensro-release/discord-announcements-webhook",
  "staff_webhook": "/etc/opensro-backup/discord-webhook",
  "maintenance_accounts": ["release-probe"]
}
```

`stack_sizes` is a port-only override; missing or empty keeps native item caps.
For example, `"potion=2000,elixir=50"` raises those groups without lowering any
native cap. Keep the value in this host configuration: each server release
passes it as `SRO_STACK_SIZES` to `sro-nomad`, which validates it and supplies
the Nomad job variable. Direct `sro-nomad` invocations accept the same environment
variable. Changes take effect on server restart; they do not update a running
server. Removing the override preserves existing quantities and permits moving
or splitting retained oversized stacks without adding to them above the new cap.

The in-game bug reporter (`/bug`) is on when root-only
`/etc/opensro-release/bug-report-webhook` holds one line, a Discord webhook URL
(a forum channel works: each report opens its own post). Each release passes
it to `sro-nomad deploy`. A missing file keeps the webhook already stored, and
the word `off` turns reports off. Changing it needs no stop: the Agent restarts
itself with the new value. Override the path with `bug_report_webhook` in
`config.json`.

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
credential roles, failed edge recovery, maintenance and notification retries,
data staging, and coordinated publish, confirm and revert.
