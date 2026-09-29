# AGENTS.md - Go server

Area rules for `apps/server/`. The repository rules in
[`../../AGENTS.md`](../../AGENTS.md), including the id Software code style,
apply here too.

## Style in Go

`gofmt` owns the layout. id style in Go is the file header banner, a banner
on every function and significant type, explanatory comments, plain structs
with functions, and explicit lifecycles. Most of the server already reads
this way (see `internal/game/world/movement/runtime.go`); match it.

## Rules

- Anything read at run time (files, the database, the network, operator
  config) returns `error`. `panic` is for broken programmer invariants, which
  includes `go:embed` data validated at package init: it is compiled into the
  binary and covered by tests, so a bad row is a build defect (the
  `regexp.MustCompile` pattern), not an input error. Do not convert those
  panics into error plumbing.
- Prefer a sub-package over another prefix-named file group inside
  `internal/game/action` or `internal/game/enterworld`.
- Avoid new package-level mutable state and `context.Background()` in
  request paths.
- A new runtime hook is not done until it is wired in
  `cmd/services/sro-gameworld`, not only in tests. Check the wiring before
  reporting a feature complete.
- Tests read only files inside this module (the module is also built and
  tested on its own). A native capture the client also checks lives in the
  client's `tests/fixtures/native/` as the canonical file, with a
  byte-identical copy in the server package's `testdata/`;
  `check:shared-fixtures` enforces the match, and `--sync` refreshes copies.
- Tests never call `time.Sleep`. Wait for asynchronous results with
  `internal/testsupport/wait` (`Eventually` to wait for a condition,
  `Consistently` to prove an absence), and drive time-dependent logic with
  the simulation tick or an injected clock. forbidigo enforces it; the only
  exceptions are real-time fault injection and stress runs, each with a
  reasoned `//nolint:forbidigo`. Do not write another local wait helper.
- The verified game-data projection lives in the module, at
  `.generated/game-data/1.150/server` (git-ignored). Tests reach it only
  through `internal/testsupport/gamedatatest` (which gates on
  `licensed.RequireGameData`), never through a relative path, and never at
  package initialization: reads before the test log starts are invisible to
  Go's test cache, which the gate keeps on.
- Test fixtures in `internal/game/action` use the level-1 leveldata row;
  raising a fixture character's level silently breaks `playerCombatStats`.

## Hash-pinned evidence files

Some files are sha256-pinned by native verification: a test hashes the
source and compares it with the value frozen in its native corpus. Find all
of them before any bulk edit (a module rename, `gofmt -w` on a package):

```
rg -n 'ReadFile\((filepath\.Join\()?"[A-Za-z_./]+\.go"' -g '*_test.go'
rg -n -i 'candidate_?sha256' -g '*_test.go'
```

As of 2026-09-27 that is `combat/defensemodifier.go`,
`enterworld/{skill_ai_timing,skillreplacement,spawnskillparams}.go`,
`world/monster/{actor_distance,ai_time_manager,homing_destination,help_event}.go`
and `item/statuseffect/castingstate.go`. After any byte change,
re-run the matching `../research/tools/re/verify_*.py` script and diff the new cases
against the old: only `candidate_sha256` may change.

## Porting float math

Where a port must match x87 behaviour, keep intermediates in `float64` and
round to `float32` only where the original stores a float (`fstp dword`).
Compare ports bit-exactly in difference tests, not with tolerances.

## Verification

```
gofmt -l cmd internal
go vet ./...
go test -p 2 ./...
go tool -modfile=tools/golangci-lint/go.mod golangci-lint run ./...
```

golangci-lint is pinned in its own tool module so the repository's Go
toolchain builds it; do not use a globally installed binary. A `//nolint`
must name its linter and give a reason (nolintlint enforces both). The
reasons each linter is on or off are in `.golangci.yml`.

`pnpm task check:server` runs the full gate (tidy, gofmt, vet, tests, race
subset, govulncheck). Each step is skipped when its inputs match a recorded
pass, so an unchanged tree checks in about half a second;
`SRO_CHECK_FORCE=1` runs every step and `SRO_GO_TEST_CACHE=off` reruns every
test.
