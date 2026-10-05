# Commands

Run commands from the server module root:

```powershell
go run ./cmd/operations/sro-nomad status
```

New operators should follow [Getting started](../../../docs/GETTING_STARTED.md). This file is a
command catalog, not a second setup procedure.

Commands are grouped by ownership while each executable remains an independent
Go `main` package:

- `services/` contains the long-running Agent and GameWorld processes;
- `operations/` contains fleet, provisioning, migration, and account tools;
- `tools/` contains offline evidence and diagnostic utilities.

The operation binaries deliberately do not collapse into one `sroctl`.
Password hashing, identity provisioning, migration, shard splitting, and
offline account/character mutation require different credentials and file
access. Separate executables preserve narrow capability and deployment
boundaries instead of making every operator installation carry their union.
Shared conventions belong in small libraries only when an actual common
policy emerges. `sro-evidence` stays separate and read-only by design.

## Runtime and fleet control

- `sro-nomad` — the only fleet entry point. Its subcommands are:

  - `dev-agent`: start or safely reuse the checkout's pinned local Nomad;
  - `validate`: parse and plan jobs and validate Variable inputs without
    mutation;
  - `deploy`: reconcile Agent and enabled GameWorld jobs;
  - `status`: report configured and running jobs;
  - `stop`: gracefully drain and deregister the managed fleet;
  - `rotate-session-key`: perform acknowledged online Agent signing-key
    rotation.

  It uses the official Nomad API and is stateless; Nomad, not this command,
  owns process supervision.

- `sro-agent` — global Agent/Login process. It owns accounts, title sessions,
  shard leases and shard-bound HTTP routing. It owns no gameplay database.

- `sro-gameworld` — per-shard GameWorld process. It owns one shard's
  persistence, transport, sessions, world tick, and gameplay.

Build the runtime roles from their command packages:

```powershell
go build -o gameworld.exe ./cmd/services/sro-gameworld
go build -o agent.exe ./cmd/services/sro-agent
```

Do not run those binaries directly during normal operation.
[Getting started](../../../docs/GETTING_STARTED.md) owns the source-checkout build/bootstrap/start
sequence. [DEPLOYMENT.md](../ops/docs/DEPLOYMENT.md) owns runtime security and
state contracts. [The Nomad guide](../ops/nomad/README.md) owns production
fleet operations.

## Provisioning

- `sro-init` — atomically creates one empty current-schema authority for a
  required catalog shard; it has no unscoped store default.
- `sro-bootstrap-development` — source-checkout-only setup for the local
  browser login; creates Agent's missing Ed25519 identity and the matching bcrypt
  account from the client's `.env.development`, creates each missing enabled
  shard authority, and validates or refuses every existing file and world.
- `sro-provision-identity` — creates Agent's missing Ed25519 session-signing
  key ring without printing or replacing it; it deliberately does not install
  login accounts.
- `hash-agent-password` — reads a password without terminal echo and emits its
  accepted bcrypt hash.
- `sro-account-owner` — previews/commits an offline atomic ownership transfer.
- `sro-archive-character` — previews/commits an offline soft archive, including
  store-owned social cleanup. It is the explicit tool for pre-release roster
  overflow; it never hard-deletes bytes.

## Evidence and audit

`sro-evidence` is the single entry point for read-only evidence utilities:

- `moveclip-oracle` runs the server movement clip implementation over shared
  differential-oracle chords;
- `performance-ring` measures worst-case scoped monster population;
- `spawnable-npcs` emits the data-authored, evidence-filtered NPC asset roster;
- `spawnable-monsters` emits the evidence-filtered monster asset roster;
- `fortress-structures` emits every fortress structure reference with hit points.

Run a utility from the module root:

```powershell
go run ./cmd/tools/sro-evidence <subcommand> [options]
```
