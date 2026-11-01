# Architecture

Three parts that meet at two contracts: the wire protocol between the browser
client and the server, and the generated data both of them load.

```text
v1.150 client ──► asset pipeline (scripts/) ──► .generated/
                                                          ├── client-public/  ──► client-next (browser)
                                                          └── game-data/      ──► server (Agent + GameWorld)
client-next ◄── HTTP /api (Agent) + WebTransport/WebSocket (GameWorld) ──► server
```

## Server (`apps/server`)

A Go reimplementation of the Silkroad Online v1.150 server contracts, split by
ownership into two process roles:

| Process | Owns |
| --- | --- |
| **Agent** (`cmd/services/sro-agent`), one per cluster | Accounts, title login, shard discovery and leases, routing of shard-bound requests. Never opens gameplay state. |
| **GameWorld** (`cmd/services/sro-gameworld`), one per enabled shard | That shard's SQLite authority database, transport, sessions, world tick and gameplay. Refuses any shard but its own `SRO_SHARD_ID`. |

**Nomad** supervises both: placement, health checks, restarts, logs and rolling
updates. `cmd/operations/sro-nomad` is the only fleet entry point (`dev-agent`,
`validate`, `deploy`, `status`, `stop`, `rotate-session-key`). It is a
stateless desired-state client, not a supervisor.

Shards are declared in `config/shards.json`. Each shard is an authority
boundary: one database, one process, one transport, its own characters. Only
one node may own a shard's database.

Transport: each GameWorld serves WebTransport (HTTP/3 over UDP) and a WebSocket
fallback on the same port, and terminates its own TLS. Every connection is
admitted with a short-lived, single-use Agent ticket. Credentials (Agent
signing keys, account catalog) reach the processes through Nomad Variables,
never files in the repository.

Source layout: `cmd/` (services, operations, offline tools), `internal/`
(`agent`, `cluster`, `config`, `data`, `domain`, `game`, `gamedata`,
`gates`, `platform`, `security`, `testsupport`, `transport`), `config/` (shipped catalog and dev account),
`ops/` (Nomad jobs and configs, operator docs). The Go module path is
`opensro.online/server`.

## Browser client (`apps/client-next`)

A direct-WebGPU TypeScript client built with Vite. It owns a WebGPU surface plus
independent simulation and asset workers. Every runtime owner has exactly one
parent, declared in `src/engine/ownership.json` and enforced by
`pnpm verify:ownership`:

- the root owns frame scheduling and shutdown;
- platform owns browser listeners and the viewport; input owns sequenced command batches;
- simulation owns clocks, transport and session state, and authoritative entity/gameplay evolution;
- assets own fetching, verified packs, bounded decoding and cancellation;
- world and character presentation derive visual intent, never simulation truth;
- the renderer subtree owns scene resources, poses, the GPU device, surface and pass submission;
- UI owns display intent; audio exclusively owns the AudioContext and playback.

The client reaches the Agent through same-origin `/api` and each GameWorld
through `/shards/<id>` edge routes (or directly, for WebTransport). Hosting
requirements are in [HOSTING.md](../../apps/client-next/docs/HOSTING.md).

`apps/client-next/tests/` holds architecture tests (ownership, capabilities,
execution), runtime unit tests and browser tests. Native reference captures
used as test oracles live in `tests/fixtures/native/`.

## Asset pipeline (`scripts/`)

Node and Python build steps that read the client extraction and
produce the browser asset tree (content-addressed packs plus a verified
manifest) and the server game-data projection. See
[ASSET_PIPELINE.md](ASSET_PIPELINE.md).

## Server observatory (`apps/server-observatory`)

A local, read-only dashboard (Node, no build step) for inspecting running
shards and the item catalog. Not required to play.

## Checks

| Gate | Covers |
| --- | --- |
| `pnpm check source` (CI) | Source size (one ledger for every language), source encoding (UTF-8, no BOM, LF), shared native fixtures (server copies match the client canonicals), dprint formatting ratchet, script type checks, workspace typecheck, Go gates (`go mod tidy -diff`, gofmt, vet, golangci-lint, tests, race subset, `govulncheck`). No game data needed. |
| `pnpm --filter @sro/client-next check` | Client layout, delivery, audio, typecheck, ownership, capabilities, execution map, tests. Needs generated assets. |
| `pnpm check` | Everything above plus the asset build and asset-dependent suites. |
