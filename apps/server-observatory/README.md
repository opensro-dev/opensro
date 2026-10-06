# Server observatory

Operations dashboard for running shards, read-only by default. Node 22+, no install
or build step. Not required to play.

```powershell
cd apps/server-observatory
npm run catalog   # export the item catalog (after building server game data)
npm start         # http://localhost:5190
```

`SRO_OBSERVATORY_PORT` overrides the port. Shards come from
`../server/config/shards.json`.

## Views

- **Session history:** `/history.html` searches durable login, connection,
  reconnect, disconnect, incident and server-log records. Choose Agent for
  authentication and client reports, or a shard for world time and transport
  events. A session link follows the same boot-qualified identifier across
  services. Search an account to see connected minutes and last login/seen
  dates; no identity filter means no personal time total.

### Durable history setup

Agent stores `history.sqlite` beside its shard directory state file
(`SRO_AGENT_DIRECTORY_STATE_PATH`, normally `.state/agent/shard-leases.json`).
Each GameWorld stores its journal beside its character authority. Collection
starts with this release; older sessions cannot be reconstructed from it.

Put an operator token of at least 32 characters in `operator-token` beside
each journal before starting the service. The Observatory's existing
`operatorTokenFile` config supplies the GameWorld credential. Set
`agentOperatorTokenFile` when Agent uses a separate token, and optionally
`agentURL` (default `http://127.0.0.1:8787`). Only literal loopback HTTP
service URLs are accepted. The existing console edge authentication also
protects history. Tokens are never sent to the browser.

Connected time means an attached, world-ready character, excluding reconnect
grace and character selection. Per-account totals merge overlapping sessions
within a service. They are not AFK-adjusted playtime, and totals from multiple
shards should not be added as a unique wall-clock total. Active intervals are
checkpointed every 15 seconds. After a crash, unfinished sessions end at their
last checkpoint and are marked estimated. Raw events retain up to 90 days or
200,000 records per service; session summaries and intervals persist. The
page displays pruning, queued records, dropped records and database failures.
Disk-full or queue-overflow conditions therefore cannot masquerade as a
complete timeline. Back up SQLite with its backup API or stop the service
before copying its database; copying a live WAL database alone is incomplete.

The collector preserves original failures and later close events separately.
Unknown closures stay unknown; a timeout does not prove a player's internet
failed. Client-supplied fields are labelled `client_reported`, while account
identity comes from authenticated admission. Invalid login names remain
unverified `claimedAccount` fields. Logs index bounded messages and a selected
set of context fields; credentials and packet bodies are not indexed.

Incident reports retry four times while the page remains open, with the same
reference. A successful receipt follows a database commit. Refreshing or
closing the page ends pending retries. The game exposes delivery status and
adds the incident reference to manual bug-report context. The history page
can refresh every 15 seconds and export its current evidence; it does not
send Discord messages or operate an external paging service.

- **Overview:** online players, resident population, living uniques, outdoor
  density atlas, tick-duration history, persistence and transport health.
- **Players:** online characters with level, health, mana and coordinates.
- **World census:** resident monsters, searchable by name, GID, reference or
  region, with grade and sector filters, sorting and an entity inspector.
- **Runtime:** tick counters, Go heap, allocations, GC cycles, goroutines,
  network rates, queues, write errors, respawn queue, storage health.
- **Items:** searchable server item table with icons, details and a copyable
  `/MAKEITEM` command (the dashboard never sends it).
- **Player recovery:** inspect private character state, download evidence, and
  explicitly confirm an audited relocation to an authored town. Requires the
  separate operator configuration described below.
- Realm strip, population hotspots, a per-session realm chronicle and operator
  alerts for unavailable realms, stale captures and storage errors.

## Item catalog

`pnpm task build server-game-data` builds the server game-data projection and
then runs `tools/item-catalog.mjs`, which runs the server's own item loader
(`go run ./cmd/tools/sro-item-catalog`) over the verified
`apps/server/.generated/game-data/1.150/server/textdata` projection and writes
`.generated/observatory/items.json` (paths relative to the repository root).
`npm run catalog` reruns only the export. Artwork comes from the published
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
24 MiB. Player recovery is optional and uses a separate credential.

### Authenticated hosting and player recovery

Set `SRO_OBSERVATORY_CONFIG` to a private JSON file:

```json
{
  "shards": "/absolute/path/to/shards.json",
  "operatorTokenFile": "/private/path/operator-token",
  "edge": {
    "origin": "https://console.example.com",
    "operator": "operator",
    "secret": "REPLACE_WITH_A_RANDOM_SECRET_AT_LEAST_32_CHARACTERS"
  }
}
```

The HTTPS reverse proxy must authenticate the operator, overwrite
`X-SRO-Operator` and `X-SRO-Console-Auth` with the configured values, and remove
the incoming `Authorization` header. The gateway still binds only loopback.
All console pages and API routes require edge authentication; rescue additionally
requires an exact matching Origin, JSON content type and `X-SRO-Console: 1`.
The edge secret and upstream token are independent, private credentials.
Never expose either value in browser JavaScript or commit the configuration.

Install the same upstream token (at least 32 characters) in the GameWorld
shard authority directory as `operator-token` before starting that GameWorld.
The endpoint is absent when this file is absent. Keep this directory private;
the gateway needs only its own token copy, not access to the authority database.
Host service configuration, DNS, credentials and rollout procedures belong in
the private operations repository.

Open **Player recovery**, select the shard and inspect a character. Download
the diagnostic JSON to preserve evidence, select a town, enter a reason and
type the exact character name to confirm rescue. The server admits only
outdoor recall gates from its authored teleport catalog. It closes that
character's session and uses a binding-control lease to prevent login during
relocation. Ordinary character authority persists the destination; no direct
SQL writes are used. The player logs in again afterward. Living/dead state,
property and durable companions are retained; normal session cleanup retires
transient combat and movement. Rescue does not grant GM privileges or revive.

Every rescue writes and fsyncs an intent with the original diagnostic state
before mutation, then appends its outcome to `operator-audit.jsonl` beside the
authority store. Request IDs remain consumed after restart, including failed
attempts. A timeout or uncertain response requires inspection, never an
automatic retry. The journal is capped at 16 MiB and refuses further writes
when full. Archive it under the host's backup policy during maintenance,
retaining consumed IDs in the replacement journal; never silently truncate it.
Snapshots omit credentials, account IDs and chat, but include character state
and companion data: treat exports and the journal as private operations data.

## Tests

```powershell
npm test                                   # Node tests
npm run test:browser                       # browser probe against live shards
node tools/items-browser-probe.mjs         # item view probe
```

Browser probes use the repository Chrome launcher in
`scripts/lib/probeBrowser.mjs` and write screenshots to `temp/artifacts/`.
