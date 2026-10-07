# Character crowd recordings

This tool reuses actual recorded actor inputs to exercise the shipping character
renderer. It does not replay network traffic, terrain, the runtime character
owner, or the complete game. Its frame durations are **not live-world FPS**.
The renderer starts fresh, so live cloth, particles, and presentation RNG state
are not restored. Use the same replay seed, camera, capture and asset hashes for
paired experiments. A live server/client settlement check remains necessary.

Run captures, image checks and timedemos through the repository's
`scripts/coordination/locked-run.mjs`, with `SRO_COORDINATION_DIR` pointing at
the shared coordination directory. Capturing perturbs the running client;
keep its window separate from FPS measurements.

After the existing benchmark `instrument()` and authenticated `openClient()`:

```js
await page.evaluate( async metadata => {
    const { installCrowdCapture } = await import( "/tools/perf/core/crowd-capture.mjs" );
    installCrowdCapture( metadata, { maxFrames: 1200 } );
}, {
    sourceCommit, manifestHash,
    replaySeed: 42,
    viewport: { width: 1600, height: 900 },
    videoOptions
} );
// Drive the desired scenario here. Capture ends at 1200 frames or 64 MiB.
const capture = await page.evaluate( () => globalThis.__crowdCapture.finish() );
await writeFile( outputPath, JSON.stringify( capture ) );
```

Use the actual source revision, generated manifest hash and video options from
the capture harness. `replaySeed` is an explicitly chosen seed for the fresh
renderer, not the unknown RNG state of the running client. The recorder copies
actors at frame end and retains the runtime frame clock from the movement
observer. It skips frames without that clock. It preserves optional undefined
fields, Float32 attachment rotations, and negative zero through JSON. Failures
appear in `capture.failure`; they must not be treated as a successful recording.

From `apps/client-next`, replay against a Vite instance serving this checkout:

```text
node tools/perf/bench/crowd-timedemo.mjs URL CAPTURE.json CAMERA.json OUTPUT_DIR
```

`CAMERA.json` is an explicit `WorldCamera`, with `originRegion`, `eye`, `target`,
`fov`, `near` and `far`. Orbit input is recorded as evidence but does not restore
the terrain-collided, zoom-eased live camera. Choose the camera for the captured
coordinates and inspect `last-frame.png`. Admitted actor counts alone do not
prove that the crowd was visible.

The first 200 recorded frames are discarded as warmup; all frames advance once
in their original order. Assets are loaded before measurement with the shipping
GLB, native BAN, texture, effect and assembly owners. Streamed motions use the
published body/animation manifests and retain their recorded clip identities.
Unsupported model identities fail explicitly.
No material simplification is performed. `timedemo.json` records per-frame
renderer call duration and draw statistics, asset SHA-256 digests, camera and
metadata. Model admission and capture decoding are outside timed spans.

Image fixtures can reuse `loadCrowdModels(renderer, actors)` from
`core/crowd-models.mjs`. Its result contains asset hashes and a `close()` method
for retained bitmaps; dispose the renderer before calling `close()`.
