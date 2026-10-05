# AGENTS.md - browser client

Area rules for `apps/client-next/`. The repository rules in
[`../../AGENTS.md`](../../AGENTS.md), including the id Software code style,
apply here too.

## Style in TypeScript

`dprint` owns the layout (tabs, `if ( x ) {`, `call( a, b )`, same-line
braces). Most client files predate it and are minified; when you change one,
format it with `pnpm exec dprint fmt <path>`, add the header and function
banners, and drop it from `scripts/checks/format-baseline.txt` with
`node scripts/checks/check_formatting.mjs --update` (run from the repository
root).

## Rules

- Module state is owned by one module and changed through its functions. Do
  not add another mutable `let` to a large closure such as `createUi`; give
  the state a named owner.
- Tools and profilers never patch source text (`.replace()` on a `.ts` file
  served through Playwright). Add an explicit instrumentation hook instead.
- `execution-contract.json` is the reviewed source of truth for capabilities,
  frame order and disposal order. The resolved graph is a derived artifact
  (`temp/artifacts/execution/execution-map.json`); never commit it.

## Tests

Tests load TypeScript sources through the shared loader, which uses Node's
built-in type stripping instead of an esbuild bundle:

```
import '../helpers/native-source-loader.mjs';
const { thing } = await import( '../../src/engine/.../thing.ts' );
```

Do not bundle sources with esbuild in a test: a private bundle can diverge
from what the client ships. `tests/architecture/test-loaders.test.mjs` fails
on any test that imports esbuild without an entry in
`tests/architecture/legacy-test-loaders.txt`. That ledger only shrinks. Plain
`path` entries are legacy waiting to migrate; `path # reason` entries are
tests where esbuild is the point (a build plugin that instruments or mutates
the source, a synthetic stdin entry, build-time defines).

Native captures under `tests/fixtures/native/` that the Go server also
checks are canonical here; the server keeps byte-identical copies
(`check:shared-fixtures`). After regenerating one, run
`node scripts/checks/check_shared_fixtures.mjs --sync` from the repository
root.

Tests are type-checked too (`pnpm verify:test-types`, over
`tsconfig.tests.json`): strict null checks on, implicit `any` and indexed
access relaxed for untyped test JavaScript. Files in
`tests/architecture/test-type-debt.txt` still have errors; a new test must be
clean, and a fixed file leaves the ledger (`node tools/verify-test-types.mjs
--update`). Browser tests are included; `tests/browser/types/page-globals.d.ts`
declares the probes their page code reads.

## Verification

| Scope | Command |
| --- | --- |
| Inner loop, after every new module or call edge (about 7 s) | `pnpm --filter @sro/client-next verify:quick` |
| Every client gate | `pnpm --filter @sro/client-next check` |
| Unit and architecture tests | `pnpm --filter @sro/client-next test` |
| Browser tests | `pnpm --filter @sro/client-next test:browser` |
| Frame rate and allocations (goal: 500 fps in every scenario) | `node tools/perf/bench/fps-bench.mjs`; process in [docs/PROFILING.md](../../docs/PROFILING.md) |

`verify:quick` runs the typecheck and the ownership, capability and
execution-flow gates. A new module, async function, timer, stored callback
or cross-module call needs an entry in `src/engine/ownership.json`,
`execution-contract.json` or the owner lists in `tools/verify-capabilities.mjs`;
design against those before writing the code, not after the gate fails.

Read a gate's own output and exit code. Do not pipe it through a filter such
as `grep "error TS"`: a command that fails before it reaches the checker
(an unknown flag, a missing tool) prints nothing the filter matches, and the
pipe returns the filter's status instead of the gate's.
