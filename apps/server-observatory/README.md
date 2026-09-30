# Server observatory

Local, read-only operations dashboard for running shards. Node 22+, no install
or build step. Not required to play.

```powershell
cd apps/server-observatory
npm run catalog   # export the item catalog (after building server game data)
npm start         # http://localhost:5190
```

`SRO_OBSERVATORY_PORT` overrides the port. Shards come from
`../server/config/shards.json`.

## Views

- **Overview:** online players, resident population, living uniques, outdoor
  density atlas, tick-duration history, persistence and transport health.
- **Players:** online characters with level, health, mana and coordinates.
- **World census:** resident monsters, searchable by name, GID, reference or
  region, with grade and sector filters, sorting and an entity inspector.
- **Runtime:** tick counters, Go heap, allocations, GC cycles, goroutines,
  network rates, queues, write errors, respawn queue, storage health.
- **Items:** searchable server item table with icons, details and a copyable
  `/MAKEITEM` command (the dashboard never sends it).
- Realm strip, population hotspots, a per-session realm chronicle and operator
  alerts for unavailable realms, stale captures and storage errors.

## Item catalog

`npm run catalog` runs the server's own item loader through
`go run ./cmd/tools/sro-item-catalog` and writes `temp/artifacts/items.json`
from the verified `apps/server/.generated/game-data/1.150/server/textdata`
projection (path relative to the repository root).
Rerun it after rebuilding server game data. Artwork comes from the published
client assets.

To use a command: open the in-game GM console with Shift + Backquote, paste, press
Enter, then pick up the ground item. GM permission is required.

## Measurement notes

- Tick mean, max and overruns are since server boot, not rolling percentiles.
  The chart samples the most recent completed tick on each refresh.
- Network rates derive from monotonic counters and reset on restart.
- Heap is Go heap-object memory, not process RSS.
- Player count means world sessions, not authenticated accounts.
- The census captures at most 50,000 monster rows and reports truncation;
  `resident` is always the full count.
- Domain snapshots are synchronized independently, not as one atomic tick.

## Access

GameWorld serves `/internal/diagnostics/observatory` only to loopback callers
with a literal loopback Host, the diagnostics header and no Origin or
forwarding headers. It copies state under the existing owner locks and caches
the encoded result for two seconds. The Node gateway binds 127.0.0.1, accepts
only local same-origin requests, uses the fixed shard URLs from the catalog,
coalesces requests per shard, times out after five seconds and caps payloads at
24 MiB. There are no mutating endpoints. Remote hosting would need its own
authenticated access design.

## Tests

```powershell
npm test                                   # Node tests
npm run test:browser                       # browser probe against live shards
node tools/items-browser-probe.mjs         # item view probe
```

Browser probes use the repository Chrome launcher in
`scripts/lib/probeBrowser.mjs` and write screenshots to `temp/artifacts/`.
