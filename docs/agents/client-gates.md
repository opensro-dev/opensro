# Client gates

- **gate** Design a new module against `ownership.json` and `execution-contract.json`, and run `verify:quick` after
  every new module or call edge.
- **gate** Shared module state is an immutable scalar or a readonly literal table.
- **review** Before its PR, a client change runs the targeted suites covering the change plus `verify:quick`,
  locally. The whole client check and the full test suite are CI's job; never run them locally to prove a change.
- **review** A device- or flag-gated path needs a test that runs that path, not only one that picks it.
- **review** A saved Experimental choice outlives a changed default, and a removed option's key is simply never read
  again. To flip a default for everyone, bump the storage key's version and migrate.
- **review** Never destroy a GPU resource directly. Retire it through the device's retirement path.
- **review** Performance work that changes the picture needs the owner's approval. By default it changes nothing visible.
- **review** Do not write an asset format `main` cannot read into the shared `.generated`. Add fields compatibly, or
  build into a private `SRO_GENERATED_ROOT`.
- **review** After merging a PR that touches the asset build, rebuild the assets.

## What the gates cost (measured 2026-11-08, issue #2907)

One developer machine, several agents working: the per-checkout caches under `apps/client-next/temp/cache` decide
whether a fresh worktree's first gates take minutes or seconds. The caches are content-addressed, so
`tools/warm-cache.mjs` shares them through the main checkout's `.state/client-warm-cache`: `pnpm run typecheck` and
`verify:test-types` seed before tsc runs and publish after, and the execution graph seeds itself on a cache miss.

| Gate | Cold (fresh worktree) | Warm / seeded |
| --- | --- | --- |
| `pnpm run typecheck` (client program) | 223 s | 8-13 s |
| `verify:test-types` (tests program) | 173 s (346 s in the #2866 incident) | seeded the same |
| `generate-execution-map` | 104 s | ~1 s |
| `verify-ownership`, `verify-capabilities` | 6-9 s each (no cache; they parse in-process) | same |
| `verify:quick` total | ~340 s serial | ~16 s |

Per test file, the runner (`tools/run-tests.mjs`) pays a fixed cost per file - a fresh Node process plus the
TypeScript graph it loads - before the first test runs: observed ~0.5-2 s per file even for trivial tests, so a
suite's floor is its file count times that cost, divided by the worker concurrency. Targeted suites land at
13-15 s. The runner prints the thirty slowest files per run (the client check echoes the line, so it
reaches the CI log on green runs too) and, when the budgets are set as the CI source lane sets them,
fails any file over the per-file budget (20 s there) and the run itself over the whole-suite budget
(300 s). Both are wall time, so neither binds by default: the full check runs every file beside its
other heavy gates, and its wall times measure that contention (#3459) (`SRO_TEST_FILE_BUDGET_MS` and `SRO_TEST_SUITE_BUDGET_MS` are the
knobs); a file near the budget is split, not tolerated. Named exceptions
with their own caps live in `apps/client-next/tests/architecture/test-file-budgets.txt` and only shrink
(one entry today: `execution.test.mjs`, 109 s on the two-core CI runner). The full suite is CI's
measurement, never a local one.
