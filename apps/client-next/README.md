# Browser client

Direct-WebGPU TypeScript client for the server in `apps/server`, built with
Vite. It needs the generated assets from `pnpm assets build` and a running
server; see [docs/GETTING_STARTED.md](../../docs/GETTING_STARTED.md).

## Commands

Run from the repository root with `pnpm --filter @sro/client-next <script>`,
or from this folder with `pnpm <script>`.

| Script | Does |
| --- | --- |
| `dev` | Development server on <http://127.0.0.1:5180>, proxying `/api` and `/shards/<id>` |
| `dev:https` | Same over TLS, for other devices ([HOSTING.md](docs/HOSTING.md)) |
| `check` | Every client gate, fast static checks first: typecheck, test types, ownership, capabilities, execution contract, layout, retail panels, asset delivery, item-sound catalog freshness, audio census, tests |
| `build` | Production build into `temp/artifacts/dist/` (run `check` separately; `build:beta` runs it for releases) |
| `preview` | Serve the production build on port 4180 |
| `test` | Architecture and runtime tests |
| `test:browser` | Browser tests against a running dev server and server cluster |
| `audit:audio`, `audit:bsr`, `audit:vfx` | Parity censuses. Add `--verify` to execute the production tests and native validators, `--strict` to fail on any open item, and (BSR only) `--packed` to include packed assets |
| `verify:<name>` | One gate from `check` on its own |

## Ownership

Every runtime owner has exactly one parent, declared in
`src/engine/ownership.json`. Parents construct their children; siblings never
import each other and communicate only through parent-issued typed
capabilities. `execution-contract.json` declares those grants and the
frame and disposal order. `pnpm verify:execution` resolves the actual call flow,
fails on anything the contract does not grant, and writes the resolved graph to
`temp/artifacts/execution/execution-map.json` for inspection (not committed).

Raw GPU objects stay inside the renderer subtree. Device loss invalidates old
capabilities and rebuilds retained resources. Buffers, images, requests,
entities and queues all have explicit limits. Imports outside a file's folder
use `@/`; local children and worker URLs use `./`.

The ownership tree is summarized in
[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md#browser-client-appsclient-next).

## Layout

| Folder | Contents |
| --- | --- |
| `src/` | Bootstrap and the owned engine; shared foundation code by domain |
| `tests/` | `architecture/`, `runtime/`, `browser/` tests; `fixtures/native/` reference captures; `oracles/legacy/` differential oracles |
| `tools/` | Checks, execution-map generation, published-asset access, profiling (`perf/`, see [PROFILING.md](../../docs/PROFILING.md)), `publish-*` asset publishers, beta packaging |
| `docs/` | [HOSTING.md](docs/HOSTING.md) |
| `temp/` | Ignored build output and scratch files |

`pnpm verify:layout` limits the root to these five folders (`node_modules/`
excluded).
