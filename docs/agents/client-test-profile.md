# Client test profile

Where the client check's CI time goes, measured from one named run at main
(#2929, parent #2907). To re-measure, read the `tests slowest:` line of any
green client-source job — the runner prints it every run, and the client
check echoes it into the job log — and copy the per-gate `[pass ]` lines
beside it.

## The named run

- Workflow: Validate Linux source, run
  [42485001013](https://github.com/opensro-dev/opensro/actions/runs/42485001013)
  at main commit `c2992818206e8e04c9f165ce0504109cd173a852` (2026-11-08).
- Job: client-source (128744867907), `node apps/client-next/tools/check.mjs
  --source` on ubuntu-latest, 4 cores.
- Result: PASS, 7 gates in 128.0 s of wall time.

## Per gate

The gates run concurrently, so the job's wall is the slowest gate, not the
sum. `verify:test-types` is the critical path of this run.

| Gate | Wall | Where the time goes |
| --- | --- | --- |
| `verify:test-types` | 128.0 s | The TypeScript program over the tests; cold on a fresh checkout (the shared warm cache is a worktree convenience, not a CI one). |
| `source tests` | 113.5 s | One file: `execution.test.mjs`, below. |
| `typecheck:gallery` | 112.4 s | The UI gallery's own TypeScript program. |
| `typecheck` | 111.2 s | The client TypeScript program. |
| `verify:dependencies` | 9.1 s | In-process source parses. |
| `verify:ui-census` | 4.6 s | In-process source parses. |
| `verify:layout` | 1.6 s | In-process source parses. |

So even an instant test suite would leave this job waiting ~128 s on the
three TypeScript programs; the three small gates are noise.

## Per file: the source tests gate

27 files ran, none reused (a fresh CI checkout has no test cache), 113.4 s
of runner wall. One file is the gate: `tests/architecture/execution.test.mjs`
took 113.2 s of it — it computes the execution graph over fixture trees and
then over the real application graph. It is the budget ledger's one named
exception (`tests/architecture/test-file-budgets.txt`, a 150 s cap against
the 60 s default); no file breached its budget in this run. The other 26
files total ~74 s of process time, hidden under it on four cores; the next
slowest would set a ~15 s gate if the first file were split (#2924-#2928).

| # | File | Wall |
| --- | --- | --- |
| 1 | tests/architecture/execution.test.mjs | 113.2 s |
| 2 | tests/architecture/test-cache.test.mjs | 14.6 s |
| 3 | tests/architecture/panel-navigation.test.mjs | 10.9 s |
| 4 | tests/runtime/application-release.test.mjs | 9.6 s |
| 5 | tests/runtime/input-network.test.mjs | 5.9 s |
| 6 | tests/runtime/beta-release.test.mjs | 2.9 s |
| 7 | tests/runtime/lifecycle.test.mjs | 2.8 s |
| 8 | tests/runtime/terrain-frustum-cull.test.mjs | 2.7 s |
| 9 | tests/architecture/ownership.test.mjs | 2.5 s |
| 10 | tests/architecture/ownership-fixtures.test.mjs | 2.2 s |
| 11 | tests/runtime/cos-item-use.test.mjs | 1.9 s |
| 12 | tests/architecture/warm-cache.test.mjs | 1.7 s |
| 13 | tests/runtime/persistent-recovery.test.mjs | 1.7 s |
| 14 | tests/runtime/release-smoke.test.mjs | 1.7 s |
| 15 | tests/architecture/ui-census.test.mjs | 1.6 s |
| 16 | tests/architecture/wave0-move-tools.test.mjs | 1.6 s |
| 17 | tests/architecture/frame-barriers.test.mjs | 1.6 s |
| 18 | tests/runtime/persistent-settlement.test.mjs | 1.3 s |
| 19 | tests/runtime/device-recovery.test.mjs | 1.1 s |
| 20 | tests/runtime/renderer-diagnostics.test.mjs | 1.1 s |
| 21 | tests/architecture/bsr-dependency-audit.test.mjs | 1.0 s |
| 22 | tests/runtime/cos-hud.test.mjs | 1.0 s |
| 23 | tests/runtime/runtime-text-admission.test.mjs | 0.9 s |
| 24 | tests/architecture/test-loaders.test.mjs | 0.8 s |
| 25 | tests/architecture/item-sound-catalog-expanded.test.mjs | 0.5 s |
| 26 | tests/architecture/census-workers.test.mjs | 0.4 s |
| 27 | tests/architecture/dev-updates.test.mjs | 0.4 s |

The tail shows the runner's per-file floor — a fresh Node process plus the
module graph it loads, ~0.5-2 s before the first test
(docs/agents/client-gates.md): below the top five, no file's own work is
measurable against it.

## Scope

This is the source lane (`check.mjs --source`) — the only client check CI
runs at main. The full `pnpm --filter @sro/client-next check` adds gates
(`verify:ui-action-census`, the asset audits, the whole `tests/runtime`
lane) that need the retail asset build, so no CI run at main measures them;
their cost lives on licensed asset workspaces. The slowest-30 table above
is therefore the source lane's 27 files, whole.

## Tightening (2026-11-08, #2929)

Picked from this table, and set where this table was measured: the CI
source lane now runs with a 20 s per-file budget and a 300 s whole-suite
wall budget (`SRO_TEST_FILE_BUDGET_MS`, `SRO_TEST_SUITE_BUDGET_MS`, set
in source-linux.yml) — the slowest ordinary file above took 14.6 s in
the worse of two profiled runs, so 20 s binds at a third of the old cap
with headroom for runner jitter. The runner itself sets neither budget
(the per-file default was 60 s until #3459): the full check runs every
file beside its other heavy gates, and inside it ui-behavior measured
83-114 s against 48 s alone, so a default there measured contention, not
the files. The named caps bind wherever a per-file budget is set. The
ledger entry above is unchanged.
