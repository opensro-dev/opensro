# Optional environment graphics stages

These stages are **port-only, not native**. Each is off by default in
Experimental. Turning them off preserves the existing palette, fixed light,
terrain shading, distant fog band, and native bloom chain.

## Moving sunlight

The World tab switch replaces the fixed diagonal light with a direction
following the sky sun's X-Y arc. At night it uses the anti-solar direction;
this is an artistic choice, not the native moon's separately scaled arc.
Within `SUN_TWILIGHT_HEIGHT` (0.2) of the horizon, the direction blends toward
zenith so the day/night transition remains continuous. Vertex and fragment
lighting use the same `lightDirection()` helper in `pipelines.ts`.

`world-environment.ts` preserves the original 336-byte prefix and appends
`sunDirection` at byte 336. The resulting environment block is 352 bytes.
The device appends `stages` at byte 352, for a 368-byte uniform:
x height fog, y sun direction; z and w are retired and always 0.

## Terrain relief and textured horizon (retired)

Both World tab switches were removed at the owner's request on 2026-10-11:
the change was too small to see (#764). `terrainBlockNormals` and the
`TERRAIN_RELIEF` shading are gone, terrain always carries the retail flat
(0,1,0) normals, and the retail distant-cell rule (8ABFD0: untextured
linear fog outside the detailed band) always applies. The `stages` z and w
lanes stay in the uniform layout and are written as 0.

## Smooth bloom

The Image tab switch modifies the bloom chain when Bloom effect in Video options
is enabled. It retains the capture scale, threshold, kernel and composite
constants but removes per-tap 8-bit rounding and adds a second blur level.
This changes both glow spread and intensity; it is not an HDR scene renderer.

The capture target keeps the canvas format for compatibility with scene
pipelines. Only the 512- and 256-pixel blur targets use `rgba16float`.
Switching back rebuilds the native targets and uses the original native
shader and pass sequence. Float shader and pipelines are created only on
first enabled use. Validation failures use the existing terminal device
error path; they are not silently ignored or recovered by disabling bloom.

## Verification scope

The environment unit tests cover the appended direction, its unit length,
and continuity across both horizon crossings. Terrain normal tests cover
flat ground, ramps, and border differences. Browser verification must cover
each switch separately, restoration after disabling, float bloom resize and
native handback, and exact default pixels against the reviewed baseline.
Check the PR validation record for commands and results actually run; this
document does not assert a pass from an earlier contributor session.
