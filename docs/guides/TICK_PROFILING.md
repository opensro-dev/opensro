# Tick profiling: capture and reconcile a live tick budget

Why the live tick costs what it costs, in exact milliseconds per tick,
with no assumptions and no blind spots. This is the read the 10 ms
budget (#2150) is planned and accepted against; the epic carries the
current targets table. It replaces the hand analysis of 2026-11-07,
which assumed its tick counts, lost time to pprof's node dropping,
and misread a scheduler profile as 27 ms of runnable wait per tick
(the raw trace held 17 ms over all 51 ticks).

Three sources, reconciled, never merged silently:

- **Exact zone totals.** `/transport/metrics` carries `tick_zones`:
  every tick zone's cumulative count and milliseconds since boot
  (phases, divisions and their sub-steps, hooks and their action
  steps). Two snapshots diffed give exact ms/tick - no sampling, no
  histogram bucket midpoints.
- **The CPU profile**, reduced with no node dropping: every sample
  counts, and a function's cumulative time credits each sample once
  per distinct function on its stack.
- **The execution trace**, integrated for the tick goroutines (stacks
  through `Ticker.runScheduledTick` / `Ticker.runDivision`): running,
  runnable and blocked time per tick, with the top stacks for the
  runnable and blocked waits.

The tool is `sro-tickbudget` in `apps/server/tools/tickbudget` (its
own Go module, so the server module gains no dependencies):

```
cd apps/server/tools/tickbudget
go build -o sro-tickbudget .
```

## 1. Capture

```
./sro-tickbudget capture --ssh USER@HOST --out capture-2026-11-08
```

This runs the whole sequence on the host in a private `/tmp`
directory that is deleted when it ends (the files hold player names
in stack samples - never copy a capture into this repository):

1. metrics, a 30 s CPU profile, metrics;
2. metrics, a 5 s execution trace, metrics;
3. a heap pull, 60 s, a second heap pull and metrics.

The bracketing metrics snapshots are what make the windows exact:
each profile is scaled by the tick count measured around it, not by
a duration someone typed. `--pprof` (default `127.0.0.1:8792`) and
`--metrics` (default `127.0.0.1:8788`) name the host-local service
addresses. Both must be loopback - the pprof listener exposes the
whole process - and the tool refuses anything else before it fetches
a byte. The ssh target exists only on your command line; no host name
or address belongs in the repo.

A capture takes about 95 seconds. Take it while the world is under
the load you want to explain, and note the live session count the
budget prints - a budget without its load is not comparable.

## 2. Analyze

```
./sro-tickbudget analyze capture-2026-11-08
./sro-tickbudget analyze --targets targets.json capture-2026-11-08
```

It prints the budget, writes `budget.json` next to the capture (the
machine-readable form of everything below), and with `--targets` - a
JSON object of zone key to ms/tick, the #2150 table's shape - adds a
now / target / gap read per zone.

A directory assembled by hand analyzes too: any two metrics snapshots
(`metrics-*.json`, or `m0.json`/`m1.json`), a `cpu.pb.gz` and a
`trace.out` are picked up by name. Snapshots from before the zone
ledger (no `tick_zones`) still read: the zone rows skip and the
profile/trace checks that can run, run.

## 3. Reading the table

```
window: 609 ticks, 38152.4 ms of tick wall (62.648 ms/tick mean), ...
zones (ms/tick, exact from tick_zones):
  phase:total                     56.120
    phase:before_hooks             5.9..
    phase:divisions               28.4..
      division:global-official    28.1..
      unattributed (divisions)     0.3..
      sub:monsterLeg              13.5..
      ...
      unattributed (sub-steps)     2.1..
    phase:hooks                   19.8..
      hook:action                 18.9..
      unattributed (hooks)         0.2..
      step:advancePets             4.2..
      ...
      unattributed (steps)         1.0..
    phase:commit                   1.9..
    unattributed (phases)          0.0..
```

- The phases nest under `phase:total`; divisions and their sub-steps
  under `phase:divisions`; hooks and the action steps under
  `phase:hooks`. Sub-step and step keys are global (they name the
  sub-step, not its division or hook), so they read as a group inside
  their container.
- Every container prints an **unattributed** row: parent minus the
  sum of its children. That is real time no child zone claims -
  dispatch, waiting on shard workers, work between zones. A budget
  that does not print it is hiding it.
- Under the table, the CPU profile's top cumulative functions, then
  the top frames under each zone (samples whose stack names the
  zone), then the trace's running / runnable / blocked per tick with
  the top blocking and runnable stacks.

## 4. Reconciliation: the BLIND SPOT lines

Five checks print PASS, FAIL or SKIP with their numbers:

- (a) the phases sum to `phase:total` within 2%;
- (b) the trace's running time per tick for the tick goroutines
  agrees with the zones' sum within 15%;
- (c) CPU per tick from the profile agrees with the trace's running
  time within 15%;
- (d) runnable and blocked time per tick are reported, with the top
  stacks (this is the check that would have caught the 27 ms misread:
  scheduler latency is not runnable wait);
- (e) children never exceed their parent.

Every FAIL prints as a **BLIND SPOT** line naming the two disagreeing
sources and both numbers. The tool never picks a winner: a
disagreement means one source is being misread, and naming it is the
fix's starting point. A missing source is a SKIP, never a silent
pass - a capture without a trace cannot fail (b), and says so.

Two reading notes:

- The tick goroutines are the coordinator plus the shard workers
  that actually ran a division in the window. An idle shard never
  enters `runDivision`; its channel wait is not tick time and is not
  counted as blocked.
- Blocked time for the coordinator is mostly waiting for shard
  workers to finish a tick's divisions. Read it beside the divisions
  rows, not as waste by itself.

## 5. Feeding the budget

`budget.json` carries the windows (exact ticks, wall, allocation per
tick, live sessions), every zone's ms/tick, the check verdicts, the
target gaps and the profile/trace summaries. Diff two captures'
`budget.json` files to show what a change bought; cite the capture's
session count and tick mean whenever a number leaves this page.
