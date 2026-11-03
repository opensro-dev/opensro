# Tick tracing: one slow tick as a timeline

The tick counters say a tick was slow; they cannot say what ran
inside it. With tick tracing armed, every phase, division, hook,
action step and the monster leg records a zone, and a tick that
reaches 100 ms is written as a Perfetto trace: a nested timeline
that opens in an existing viewer (`#2301`). No viewer is built.

Tracing is off by default and costs nothing measurable while off
(one nil check per zone point). It is port-only tooling, not a
native behaviour.

## Capture one

1. Arm it on the GameWorld under test: set `SRO_TICK_TRACE=on` in
   its environment and restart it. Leave it unset everywhere else.
2. Make a slow tick. Under `sro-loadtest` the crowded-field scene
   at 200-500 sessions does it; in play, the first ticks after a
   teleport wave usually qualify.
3. Take the newest `tick-trace-*.json` from the GameWorld's state
   root (the directory that also holds `tick-spikes.jsonl`). Four
   slots rotate, newest overwriting oldest, so copy the file
   before the next slow ticks land. `/transport/metrics` counts
   them: `tick_traces_written`, `tick_trace_dropped`,
   `tick_trace_write_errors`.

## Open and read it

Open [Perfetto](https://ui.perfetto.dev) and drop the file in (or
"Open trace file"); chrome://tracing accepts the same file.

- One track per goroutine: the tick coordinator, one per shard
  worker. Division zones sit on their worker's track, nested in
  time under the coordinator's `phase:divisions`.
- Zones nest by call: `phase:*` around `hook:*`, `division:<id>`
  around its sub-steps (`peerVisibility`, `sessionLegs`,
  `monsterLeg`, ...), and the action hook's steps (the StepTimer
  names) inside `hook:action`.
- Read it the way the counters cannot be read: the wide zone is
  where the tick went; a wide gap with no zone open is time the
  tick spent outside every named span, which is itself the next
  instrumentation question.

## Cost

Arming is a diagnostic act, not a production posture: each zone
reads the clock twice and appends under a short lock. The armed
per-zone cost and the disarmed overhead against
`BenchmarkTickSyntheticShard` are measured in the #2301 PR before
anyone enables this on a live shard; disarm by unsetting the
variable and restarting.
