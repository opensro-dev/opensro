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
| `combat` (only with `--at combat`) | `combat`: a repeatable fight against the native GM `/LOADMONSTER` scene |
| `crowd` (only with `--at crowd`) | `still`, `drag` among `--crowd N` real players |

`crowd` stands `--crowd N` peers (32 by default; `core/crowd.mjs`) in a grid
in front of the character: authenticated characters on real GameWorld
sockets, admitted through the local provisioning endpoint (`--provisioning`,
`--token`, the defaults of `crowd-host.mjs`) and disabled again at the end, or
attached from a running `crowd-host.mjs` that holds a matching crowd. Admission
is bounded by the Agent's login budget (a burst of 10, then one every 6 s), so
a freshly admitted crowd has at most 32 peers; hold one crowd with
`crowd-host.mjs` across benches, and hold a larger one from saved sessions
(below). It measures what every drawn character costs (#3563).

### A large crowd held from saved sessions

Every local peer logs in from 127.0.0.1, so 600 logins take about an hour.
An Agent session lasts 12 hours, so provision the crowd once, then hold it
from its saved sessions in about a minute as often as needed.

Run it against a local stack on a scratch world (`SRO_LOCAL_STATE_ROOT` set
to a scratch directory when the stack starts), never the owner's own local
world: the accounts stay in that world's database, disabled, after
`--retire`.

```
node tools/perf/bench/crowd-provision.mjs --fixture FIXTURE.json --count 600 --accounts ACCOUNTS.json
node tools/perf/bench/crowd-host.mjs --accounts ACCOUNTS.json --count 600 --act walk=0.3,chat=0.1 --seed 1
node tools/perf/bench/fps-bench.mjs --at crowd --crowd 600 --crowd-fixture FIXTURE.json --act walk=0.3,chat=0.1 --seed 1
node tools/perf/bench/crowd-provision.mjs --accounts ACCOUNTS.json --retire
```

- **Provision** creates scratch accounts and characters and saves each
  account's password and session to the accounts file (owner-only; default
  `.state/crowd-host/crowd-accounts.json`). It waits out the login budget,
  prints progress with an estimate, and can be stopped at any time: running
  it again with the same file continues where it stopped and never creates an
  account twice. A larger `--count` later adds accounts to the same file.
- **Hold** stands the first `--count` characters of the file on the crowd
  layout (a fixture reset each, so every hold starts the same scene), connects
  them on their saved sessions and logs in again only where a session has
  expired. It prints how many sessions it reused and how many logins it spent,
  and `GET /crowd` carries the same as `source`. Shutdown (`POST /shutdown`,
  Ctrl+C) closes the sockets and keeps the accounts for the next hold.
- **Measure** with the hold's fixture, count, `--act` and `--seed` (they make
  up the crowd's key); add `--crowd-host URL` when the host is not on its
  default port. Each row's `crowd` records the size, the source and the
  activity evidence.
- **Retire** disables every account of the file once the crowd is no longer
  wanted and reads each one back from the Agent.

The layout keeps the whole crowd and every walker's 25-unit reach within one
320-unit interest block of the observer on both axes, so the server sends the
observer every peer: rows 15 units apart starting 35 units ahead, at most 18
deep, then wider (600 peers stand 34 wide, 720 at most). A crowd that would
leave the fixture's region is refused before any account is made. The ground
is the fixture's to get right: in the Jangan square (region 25000 around
x 1404, z 1374) buildings block some of a 600-peer layout. This fixture's
every spot and walker reach, for every crowd up to 720, passes the server's
movement clip (`ClipMovementPath`), and it stands inside Jangan town:

```
{ "id": "held-crowd-jangan-north", "movementMode": 3, "start": { "regionId": 25255, "x": 1050, "y": 0, "z": 910 }, "startYawRadians": 0 }
```

`combat` loads `--combat codename:count:type` at the character's feet with
the native GM commands, `/INVINCIBLE` first unless `--vulnerable`. The current
default `MOB_CH_TIGER:10:GIANT` is diagnostic only: it has not been validated
as a killable fixture for scratch character `asd2`. Select a measured,
killable scene explicitly before running acceptance.

The loader waits for the requested monsters' presentation actors. A window
counts only if the server accepted at least two damaging casts, the local
character stays alive, and every loaded monster remains alive and presented
within 150 units at each turn and the end. `--vulnerable` also requires
incoming damage; invincible runs cannot establish realistic combat acceptance.
The scene's codename and count repeat; positions do not (the monsters run
on their AI). Compare warmed, matched scenes with the same character stats,
graphics settings and instrumentation. Keep rejected runs as evidence.

After the measured window and captures end, the harness spends at most three
minutes attempting ordinary attacks on its recorded monster IDs. It verifies
cleanup against the server's observatory and writes `combat-residue.json`,
including on rejected windows. Positive server HP overrides client death;
missing rows in a truncated snapshot remain unknown unless a server death
was already observed. Partial loads, character death, timeout and read errors
fail cleanup. They never trigger an automatic restart or a revive. Use an
isolated GameWorld, and resolve any remaining residue before another run.
`combat-combat.json` retains the scene and bounded observations even when no
accepted FPS row exists. Deaths during post-window cleanup are not measured
combat frames; a death/drop benchmark needs a separate matched scenario.

Each row gives frames per second, frame-interval percentiles (p99 and max
show spikes), and the main thread's frame time and world-preparation time
per frame. The last line says whether the slowest scenario meets 500.

Useful options:

| Option | Adds |
| --- | --- |
| `--at jangan,hunt` / `--only drag` | Run some locations or scenarios |
| `--seconds N` | Longer spans for steadier numbers (default 3) |
| `--heap` | Allocation rate per scenario (MB/s, KB/frame) and an allocation profile each |
| `--spans` | Runtime-stage and detail-span timings (`@stage`, `stage`, `stage n`). Dense marks add overhead; use them to explain work, not to claim plain-run FPS |
| `--cpu` | A CPU profile per scenario |
| `--counts` | WebGPU commands per frame (draws, bundles, buffer writes, submits); slows the frame, so read only its counts |
| `--trace` | A Chrome trace per location (main thread, workers, GPU process) |
| `--gpu-process` | CPU and wall ms per frame of Chrome's GPU-process threads, from a separate traced window (below) |
| `--paced` | Retain the browser's display pacing |
| `--frame-limit 60` | Exercise the player's 60 FPS limit; also accepts 120, 240 or 0 (the default, uncapped) |
| `--cpu-rate 4` | Apply Chrome CPU throttling after world warm-up |
| `--shadow-detail 2` | Enable detailed character shadows; `1` selects blobs and `0` (the default) disables shadows |

For the default player setting, use `--paced --frame-limit 60`. Frame limits
and CPU rates, along with shadow detail, are recorded in the result JSON. Keep the default uncapped run
for throughput comparisons; a capped acceptance run does not measure the 500 FPS goal.

Captures go to `temp/artifacts/fps-bench/<location>-<scenario>.*`.

The machine is shared with whatever else runs on it: numbers move by up to
a quarter between runs. Compare runs made back to back, never a number
from this morning with one from now, and measure a change more than once.

### The GPU process: `--gpu-process`

Every WebGPU call the page makes (draws, `queue.writeBuffer`, compute
dispatches, submits) is serialized and replayed in Chrome's GPU process,
where Dawn decodes and validates it and hands it to the driver. That is
CPU time the main thread's numbers do not show and the GPU timestamp
queries do not see. `--gpu-process` reports it per frame:

```
SRO_PROBE_CLIENT_NEXT_BASE_URL=http://127.0.0.1:4181 node tools/perf/bench/fps-bench.mjs --only still,drag --gpu-process
```

After each timed window the scenario runs a second window under a
Chrome trace, so the trace never slows the row's own fps. The row gains
`gpuProcess`: the GPU main thread's (`CrGpuMain`) CPU and wall ms per
frame, every GPU-process thread above 2% busy, the renderer main
thread for comparison, and the frames these divide by (the traced
window's own frame-probe count and fps, the same source as a row's fps).

- **What it is:** CPU time in the GPU process. CPU is thread time; wall
  is the time a thread had a task open, which also counts blocking waits
  inside the task (for example a Metal submit waiting on the driver).
  Busy is wall over the window.
- **What it is not:** time on the GPU. GPU execution time is the
  timestamp queries' job; a frame can be bound by either.
- **Where the work is:** on an Apple-silicon Mac (Metal) the WebGPU
  commands run on `CrGpuMain` (`WebGPUDecoderImpl::HandleDawnCommands`,
  with `Queue::Submit` the largest part); `VizCompositorThread` is the
  display compositor, which an uncapped client keeps busy with many tiny
  tasks. Check the thread names on other platforms before comparing.
- **Trace categories:** `toplevel` (each thread's top-level tasks, with
  their thread time) and `blink.user_timing` (the marks that place the
  window), nothing else. Nested slices count once: wall is the union of a
  thread's task intervals, CPU the thread time of top-level tasks.
  `processCosts` in `tools/perf/core/trace.mjs` is the reducer.
- The trace is reduced as it arrives and not saved: an uncapped client's
  GPU process posts hundreds of thousands of tasks a second. Use
  `--trace` for a trace to read (the two do not combine). `cross` and
  `combat` run once per session and get no traced window.

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

### What one WebGPU call costs

```
node tools/perf/constants/serve.mjs [--port 4230]
```

then open `http://127.0.0.1:4230/` in the browser to measure
(`?quick=1` for a smoke run of a few seconds). It is a plain page with no
game assets, build or server: it measures the fixed cost of the calls a
frame is made of (#3563), so results from other machines and GPU
backends (D3D12, Vulkan, Metal) can be compared. Any static file server
works too (`python3 -m http.server` inside `tools/perf/constants`), but
only `serve.mjs` sends the cross-origin-isolation headers that give
`performance.now()` steps of about 5 microseconds instead of 0.1 ms. Keep
the tab in front and the machine quiet; the default run takes under a
minute.

| Case | Each frame (N = 250 to 4000) |
| --- | --- |
| D1 | `drawIndexed` with its own bind group, vertex and index buffer (8 rotating) |
| D2 | one pipeline, bind group and buffers; N `drawIndexed` with different arguments |
| D3 / D4 | as D2 through `drawIndexedIndirect` from one buffer; D4's records draw 0 instances |
| D5 | one draw of N instances, the floor |
| D6 | as D2, but every draw rebinds one bind group with its own dynamic uniform offset (only the offset changes) |
| D7 | as D2, but every draw binds the next vertex and index buffer (8 rotating; only the buffers change) |
| U1 / U2 / U3 | N writes of 256 bytes into N buffers / into one buffer / one write of N x 256 bytes |
| U4 / U5 | one write of N x 256 bytes to a staging buffer, then N `copyBufferToBuffer` of 256 bytes into N buffers / into one buffer (the client's staging path) |
| C1 / C2 | N dispatches with a bind group each / one dispatch of N workgroups |

D1 to D4, D6 and D7 run twice: `pass` encodes the draws every frame, `bundle` records
them once into a render bundle. Frames run back to back, not paced by the
display: encode and submit, then wait for `onSubmittedWorkDone`.

How to read the table: each column is the slope of a least-squares line
through the per-size medians, in microseconds per unit of N, with its R
squared. `main` is the main thread's encode plus submit; `loop` is the
whole frame. `loop - main` estimates what the GPU process and the GPU add
per call. It is only an estimate, because the GPU process works while the
main thread is still encoding. A low R squared on a slope near zero
(D5, U3, C2, `main` for bundles) is timer noise, not a bad fit.

`Copy JSON` copies the result, with the browser, the adapter, the
features and the parameters; paste it into the issue.
`window.__gpuConstants` holds the same result for scripts. `?sizes=`, `?frames=`, `?repeats=`, `?warmup=` and
`?only=D1,U3` change the run; `?nofeature=1` skips
`indirect-first-instance` (D3 then passes `firstInstance` 0, as it must
on an adapter without the feature).

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

A per-frame mean hides work that runs only on some frames. `--frames FN`
says how many frames ran a function and what one of those frames paid:

```
node --max-old-space-size=12000 tools/perf/analyze/trace.mjs TRACE.json --window 4:7.4 --frames "chatLayout "
```

It prints, for example, `104 of 219 frames (47%, 30.6 a second)` and
`a frame that runs it: mean 7.62 ms ...; over every frame 3.620 ms/f`:
a layout that should rebuild on change was rebuilding on half the frames.

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
[AGENTS.md](../../AGENTS.md#verification) and the browser tests for what you
touched.

### Character presentation: replay two trees

`apps/client-next/tools/perf/presentation/` proves that a change to the
character presentation (`src/engine/runtime/characters`) leaves its outputs
identical. It replays a scripted session through the presentation of a
checkout, in `runtime.ts`'s frame order, and writes one line per frame with
everything the presentation hands to the rest of the client: the actors and
portraits given to the renderer (encoded at call time), assemblies, models,
clips and the retained set, footprints, sounds played, asset requests and
cancels, the effective-HP calls, and the runtime's reads after the step
(error, degradations, camera target, damage texts and the others).

From the repository root, with `TOOL=apps/client-next/tools/perf/presentation`:

```
node $TOOL/scenarios.mjs temp/presentation               # one session per scenario
node $TOOL/replay.mjs . temp/presentation/standing.json.gz temp/presentation/base.ndjson.gz
node $TOOL/replay.mjs ../other-tree temp/presentation/standing.json.gz temp/presentation/change.ndjson.gz
node $TOOL/diff.mjs temp/presentation/base.ndjson.gz temp/presentation/change.ndjson.gz
```

`replay.mjs ROOT` loads every module from ROOT (sources, loader, the
`tests/helpers/presentation-fixture.mjs` rig, the encoder), so one copy of
the tool replays any tree that has the rig. `diff.mjs` exits 0 with the
frame count when the two runs are equal, and 1 at the first difference,
naming the frame, the gid, the JSON path and both values. The scenarios
(`scenarios.mjs` without arguments lists them) are synthetic
world-batch streams built from the published catalogs and a seed: a 200
player standing crowd for 60 s, walkers, combat with hits, a knockdown and
a death, sit and stand, a stall, a ride, equipment changes, freeze and slow,
emotes, pickups, spawn churn, a camera path across LOD rescans, ground
items, and `settle`: a still crowd that then meets each kind of input a
skipped decide must notice (presentation-due.ts; the runtime test
`presentation-due.test.mjs` replays it with the production rule and with
an always-due one), and `evict`: a standing crowd while waves of new bodies
outgrow the residency budget, so the cache retires sources in its recorded
order. A replay takes one to seven seconds per scenario and about a minute
for the standing crowd.

What it proves: the two trees present the same session identically. What it
does not: anything outside the presentation (the GPU, world scenery,
terrain, the local mover, dungeon rooms are stubbed), or a session the
scenarios do not script. Quote the diff line and each scenario's result in
the pull request.

## 5. Record the result

Report measured numbers, before and after, from back-to-back runs, with
the command that produced them.
