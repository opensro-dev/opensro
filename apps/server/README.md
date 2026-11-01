# Server

Go implementation of the Silkroad Online v1.150 (Legend III) server contracts
for the browser client. WebTransport is the primary game channel, with a
WebSocket fallback on the same port.

Local setup: [docs/guides/GETTING_STARTED.md](../../docs/guides/GETTING_STARTED.md).
How the pieces fit: [docs/architecture/ARCHITECTURE.md](../../docs/architecture/ARCHITECTURE.md).

## Processes

| Process | Player endpoint | Private control endpoint | Owns |
| --- | --- | --- | --- |
| Agent | `127.0.0.1:8787` TCP | Agent listener | Accounts, title API, shard leases and routing |
| GameWorld `global-official` | `127.0.0.1:8788` TCP+UDP | `127.0.0.1:8791` TCP | Its shard's database, sessions, world tick, gameplay |
| GameWorld `test` (disabled by default) | `127.0.0.1:8793` TCP+UDP | `127.0.0.1:8792` TCP | Same, for the `test` shard |

Shards are declared in [config/shards.json](config/shards.json). Control
endpoints are never advertised to players. Nomad supervises every process; do
not start `agent.exe`, `gameworld.exe` or `nomad agent -dev` by hand. Use
`sro-nomad` for start, deploy, status, key rotation and stop.

## Layout

| Folder | Contents |
| --- | --- |
| `cmd/services/` | `sro-agent`, `sro-gameworld` |
| `cmd/operations/` | `sro-nomad`, `sro-bootstrap-development` and other operator commands ([catalog](cmd/README.md)) |
| `cmd/tools/` | Offline tools (game-data export, item catalog, reference export) |
| `internal/` | Implementation: `agent`, `cluster`, `config`, `data`, `domain`, `game`, `gamedata`, `gates`, `platform`, `security`, `testsupport`, `transport` |
| `config/` | Shipped data-only configuration and the development account |
| `ops/` | Nomad jobs ([guide](ops/nomad/README.md)), [deployment contracts](ops/docs/DEPLOYMENT.md), [accepted advisories](ops/docs/ACCEPTED_ADVISORIES.md) |

Ignored: `temp/` (build output, logs, profiles) and `.state/` (runtime state,
development certificates, Nomad data).

## Gates

`pnpm check source` runs these (through `scripts/checks/check_go_server.mjs`,
which shards the test run). Directly, from `apps/server`:

```powershell
go mod tidy -diff
gofmt -l cmd internal
go vet ./...
go test -count=1 -timeout 300s ./...
go test -race -count=1 ./internal/agent/api ./internal/agent/server ./internal/security/auth ./internal/transport/worldsession ./internal/cluster/shard ./internal/data/store ./internal/transport
go tool govulncheck ./...
```

Executable source and tests stay under 1,000 lines per file; `testdata/`
fixtures are exempt. `sro-nomad validate` is an integration check that needs a
running Nomad.

## License

AGPL-3.0-or-later, like the rest of the repository ([LICENSE](../../LICENSE)).
The upstream fork's original license is kept in
[LICENSE-UPSTREAM.md](LICENSE-UPSTREAM.md); see [NOTICE.md](../../NOTICE.md).
