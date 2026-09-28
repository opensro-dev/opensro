# Approved server releases

`Release game server` builds static Linux binaries on a GitHub-hosted runner,
runs the Go tests, and stores a seven-day artifact. Run it from `main` with
the Actions button, or push a `server-v*` tag pointing at a commit on `main`.
The `production` environment must require the operator's review. Keep the
SSH secrets in that environment, so the build job cannot read them.

The deployment job streams the artifact to a dedicated SSH account whose
only permitted command is the installed `deploy.py` receiver. The receiver:

1. Locks out concurrent releases and verifies the complete archive before
   writing its files. Traversal, links, duplicates and hash mismatches fail.
2. Requires the configured Nomad version and takes a verified remote backup.
3. Issues a one-hour `sro-deployer` token, installs deployment inputs, preserves
   the signing identity, and validates through `sro-nomad`.
4. Announces the restart in game and on Discord, waits two minutes, then
   deploys through Nomad and checks fleet status.
5. Records the deployed commit and revokes the temporary token in `finally`.

Nomad owns per-job health checks and auto-reversion. A failed deployment can
leave updated deployment inputs on disk; inspect Nomad's actual job versions
before retrying. This is not an atomic transaction across shard databases.
An SSH failure after the upload may leave the operation in progress: inspect
the fleet and release record before retrying. The client timeout does not
authorize a second simultaneous deployment.

## Host installation

Install `deploy.py` and `bundle.py` together in a root-owned directory such
as `/usr/local/lib/opensro-release`, without group/other write access. A
root-owned executable `/usr/local/sbin/opensro-release` should run:

```sh
#!/bin/sh
exec /usr/bin/python3 /usr/local/lib/opensro-release/deploy.py
```

The dedicated `sro-release` SSH account has no password and no writable home.
Give only this key a forced command in its root-owned authorized-keys file:

```text
restrict,command="sudo -n /usr/local/sbin/opensro-release" ssh-ed25519 <public-key>
```

Its sudoers rule permits that exact receiver with no arguments. It must not
permit a shell, arbitrary Python, or another root command. This key can deploy
trusted server executables, so it remains a privileged production credential;
the forced command does not make an unreviewed binary safe.

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

Create the `production` environment with the operator as required reviewer,
allow `main` and `server-v*` tags, and disable administrator bypass. Allow
self-review if that same operator initiates the workflow. Add:

- `SERVER_DEPLOY_KEY`: the dedicated SSH private key, never the operator key.
- `SERVER_DEPLOY_HOST`: the production host address.
- `SERVER_DEPLOY_KNOWN_HOSTS`: its verified SSH host-key entry. Do not discover
  and trust a new key during the deployment job.

These are server-only releases. They do not publish client code or rebuild
assets. Website and client releases have their own build and publication paths.

## Verification

```sh
python3 -m unittest discover -s apps/server/ops/release -p 'test_*.py'
```

The archive tests cover round trips, tampering, traversal, symlinks, duplicate
members and incomplete manifests. The Actions build also runs them before
publishing an artifact.
