# Profiling the browser client

How to find out why a frame is slow, fix it, and prove the fix. All the
tools live in `apps/client-next/tools/perf`; run them from
`apps/client-next` with the dev server and the local server cluster up
(see [GETTING_STARTED.md](GETTING_STARTED.md)).

## In-game developer diagnostics

The ordinary minimap FPS icon shows only FPS and gameplay ping. To reveal the
separate `</>` developer icon beside it, run this in the browser console:

```js
sroDebug.setDiagnostics(true)
```

The preference is stored locally for that browser and site. The icon survives
reloads, but its panel starts closed. `sroDebug.setDiagnostics(false)` closes
and hides it and remembers that choice. If storage is blocked, the command
still works for the current tab. This grants no server privileges.

The developer panel shows average/p95 frame and CPU times, rendering counts,
client and Agent revisions, commit subjects, and uptime. Agent means the HTTP
Agent process, not a claim about the separate GameWorld build. While the panel
is open and the tab is visible, build information refreshes every 60 seconds;
reopening or reconnecting requests an earlier refresh with a short throttle.
Requests time out after 10 seconds, failures retry after 30 seconds, and old
information is marked stale. A missing endpoint shows unavailable.

Ping uses an opaque echo on the active gameplay WebSocket every five seconds.
It expires after 15 seconds without a reply and clears when disconnected;
`— ms` means no current measurement. The display does not use HTTP timing.

## The target

The client renders a 25-year-old game. Every scenario below should run at
**500 frames a second or more** in the benchmark (uncapped, 1600x900), so
that a 240 Hz display never misses a frame. Two budgets follow from it:

- the main thread spends **under 2 ms per frame**;
- a steady frame (nothing loading) **allocates close to nothing**: every
  kilobyte allocated per frame is paid back by the garbage collector as a
  long frame later.

## 1. Measure: the benchmark

```
node tools/perf/bench/fps-bench.mjs
```

The benchmark resets the scratch character `asd2` to fixed places, boots
the dev client in an uncapped browser, revives the character if it died,
and runs these scenarios:

| Location | Scenarios |
| --- | --- |
| `field` (quiet Europe field) | `still`, `drag` (camera), `move`, `cross` (a region crossing) |
| `jangan` (water ghost field outside Jangan, busier) | `still`, `drag`, `move` |
| `hunt` (a field with a live monster) | `skill` (skills and attacks on it) |

Each row gives frames per second, frame-interval percentiles (p99 and max
show spikes), and the main thread's frame time and world-preparation time
per frame. The last line says whether the slowest scenario meets 500.

Useful options:

| Option | Adds |
| --- | --- |
| `--at jangan,hunt` / `--only drag` | Run some locations or scenarios |
| `--seconds N` | Longer spans for steadier numbers (default 3) |
| `--heap` | Allocation rate per scenario (MB/s, KB/frame) and an allocation profile each |
| `--spans` | Where a frame goes without a profiler's overhead: ms per frame of each runtime stage (`@stage`, since the previous mark) and of each detail span (`stage`, with `stage n` spans per frame) |
| `--cpu` | A CPU profile per scenario |
| `--counts` | WebGPU commands per frame (draws, bundles, buffer writes, submits); slows the frame, so read only its counts |
| `--trace` | A Chrome trace per location (main thread, workers, GPU process) |

Captures go to `temp/artifacts/fps-bench/<location>-<scenario>.*`.

The machine is shared with whatever else runs on it: numbers move by up to
a quarter between runs. Compare runs made back to back, never a number
from this morning with one from now, and measure a change more than once.

### Effects without a server

```
node tools/perf/bench/effects-bench.mjs [--count 36] [--match ^skill/] [--cpu] [--heap]
```

Renders many published effect programs at once (36 looping skill effects
by default) through the production renderer, with no game server or
character: the same scene on every run, so it suits particle and ribbon
work, and it needs no live monster. It prints frames a second, the main
thread's frame time, and what the frame drew (particles, ribbon vertices,
draws); `--cpu` and `--heap` capture the measured span into
`temp/artifacts/effects-bench/`.

To compare against another tree (say, the last commit), start a second
dev server from a worktree of it on another port and point the benchmark
at it:

```
SRO_PROBE_CLIENT_NEXT_BASE_URL=http://127.0.0.1:5199 node tools/perf/bench/effects-bench.mjs
```

## 2. Find the cost: the analyzers

CPU profile of a scenario, by function, then inside one:

```
node tools/perf/analyze/profile.mjs temp/artifacts/fps-bench/hunt-skill.cpuprofile
node tools/perf/analyze/profile.mjs FILE --children "prepare runtime/renderer/characters"
node tools/perf/analyze/profile.mjs FILE --lines "prepare runtime/renderer/world/world.ts"
```

Allocations: the same tool reads the `.heapprofile` (bytes, not time).
Built-ins such as `subarray`, `next` or `Set` allocate on behalf of their
callers; `--callers` names them:

```
node tools/perf/analyze/profile.mjs temp/artifacts/fps-bench/jangan-still.heapprofile --seconds 3
node tools/perf/analyze/profile.mjs FILE --callers subarray
```

A trace (from `--trace`, or one a player records in DevTools and sends):
threads and workers, the GPU process, frame percentiles, long frames and
what ran in them, per-second timeline, and comparison of two windows:

```
node --max-old-space-size=12000 tools/perf/analyze/trace.mjs TRACE.json --timeline
node --max-old-space-size=12000 tools/perf/analyze/trace.mjs TRACE.json --window 1:3 --compare TRACE.json@4:8
```

A DevTools allocation timeline (`.heaptimeline`): churn and what stayed
alive, by allocating stack:

```
node --max-old-space-size=14000 tools/perf/analyze/heap-timeline.mjs FILE.heaptimeline --callers jsonChars
```

All of them map frames back to source lines through the dev server's
transform; a production trace needs `--maps` pointing at the beta
package's private maps.

## 3. Fix the cause, not the symptom

What has paid off so far, in order of size:

- **Do work on change, not every frame.** Most per-frame cost was work
  whose inputs had not changed: re-culling the world when the camera
  turned, rebuilding shadow receivers when any terrain anywhere changed,
  recomputing caches. Decide what a result depends on and recompute it
  only when that changes.
- **Touch less memory.** Walking hundreds of objects to learn that most
  need nothing costs more than the work. Keep the deciding numbers in
  typed arrays (`renderer/world/walk-table.ts`).
- **Let the GPU do what it does for free.** Clipping off-screen triangles
  costs it almost nothing; re-uploading index and instance lists from the
  CPU every frame does not. Emitted particles tick at 20 Hz on the CPU,
  which writes a slot's record only at its tick; a compute pass draws
  every frame from the records (`device/particle-shader.ts`, held to a
  JavaScript reference by `tests/browser/particle-presentation.test.mjs`).
- **Batch queue writes.** Each `writeBuffer` call has a fixed cost: a
  hundred small writes a frame cost more than one large one. The particle
  pass packs every stream's frame data into one arena (`device/particles.ts`).
- **Allocate nothing in the frame.** Reuse arrays and typed-array views;
  avoid closures, spreads and `subarray` in per-item loops.

A profiling hook in the client is an explicit observer
(`src/engine/runtime/frame-probes.ts`), never a rewrite of source text.

## 4. Prove the fix changed nothing visible

A performance change must leave the output identical. For renderer
changes, drive the old and new code through the same camera path in Node
and compare every upload and draw list frame by frame (or, where the change
moves work to the GPU on purpose, what each frame shows inside the
frustum). Add a unit test for each exactness claim and check that it fails
when the claim is broken. Then run the gates listed in
[AGENTS.md](../AGENTS.md#verification) and the browser tests for what you
touched.

## 5. Record the result

Report measured numbers, before and after, from back-to-back runs, with
the command that produced them.
