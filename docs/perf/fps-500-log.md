# FPS 500 log (#4692)

Laptop: AMD iGPU, 1920x1200, crowd of 16, snow, busy casting. One line per
finding; numbers are measured, never estimated.

## Baseline (0cc0601f5)

- drag: frame 5.13 ms (195 fps), main CPU 4.51, GPU 3.13 (main-pass 2.61).
- still: frame 4.48 ms (223 fps), main CPU 3.99, GPU 3.23 (main-pass 2.76).

## Confirmed findings

- Real content has 0 plain-static world groups: object/animated groups carry
  instanceRadius, terrain/lightmap groups carry ranges.
- Weather on the GPU costs 0.01-0.02 ms GPU and saves 0.2-0.6 ms CPU.
- UI layout runs on ~30% of frames (0.28 ms/frame total); name tags move
  through the renderer's label projection, not a UI re-layout.
- Drag vs still differs mostly by combat (effect actors 3 -> 21), not camera.
- Every admitted emitter in the crowd is a particle graph (18.8 still, 28 drag).
- Drag profile (161 fps, ms/s): characters prepare 255 (planBatches 126 of
  which advanceEmitter 87, transformFor 64; prepareGroup 84 of which
  drawPrimitive 57, writeBatchRows 17), world prepare 135 (self 33, map
  animations 32, follow camera 11), prepareEvents 73, presentActors 52.
- Laptop GPU sweep, drag, 2 runs each (gpu / main-pass ms): full 2.99-3.26 /
  2.46-2.70; 50% render scale 1.41-1.55 / 0.91-0.98 (fill-bound: ~60% of the
  main pass scales with pixels); objects hidden 2.52-2.99 / 1.99-2.32; sky
  hidden 2.81-2.89 / 2.30-2.34; characters hidden 2.44-2.88 / 2.36-2.80 plus
  skeletal 0.42-0.45 gone; ground hidden 1.64 / 2.57 (runs disagree: noise).

## Completed

- 03419e71f GPU object cull: main-pass -0.23 ms still, -0.40 ms drag; CPU level.
- 0cc0601f5 crowd names pass the server pre-check (8% crowds were refused).

## In progress

- Persistent character batches (perf/fps-500): storage kept a band down and
  idle for 2 s, charged after admission; opaque rows keep stable slots;
  instance uploads carry one coalesced dirty range; emitter simulation
  separated from planning (same order, same random sequence).

## Rejected / parked

- Plain-static world path: no content uses it; not extended.
- GPU node globals for sockets: parked on park/gpu-socket-globals, no consumer.
- Skipping the emitter transform: drawn rows and holder follow read it each
  frame; skipping only moves the cost.
- Skeleton dedup: unmeasured. The earlier ~15% compared cumulative counters
  (which include frames outside the timed window) with instantaneous pose
  counts; it needs unique poses and dispatched evaluations in one window.
- Granular instance uploads alone: characters updateInstances is ~8.7 ms/s
  (~0.05 ms/frame); shipped with stable slots, measured in the batch A/B.
