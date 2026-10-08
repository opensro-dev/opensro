# Optional environment graphics stages

These four stages are **port-only, not native**. Each is off by default in
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
x height fog, y sun direction, z terrain relief, w textured horizon.

## Terrain relief

The World tab switch shades terrain slopes using heightfield normals.
`terrainBlockNormals` computes normals from the 17-by-17 height grid with
20-unit spacing, using one-sided differences at block edges. Asset loading
requests these normals when relief is enabled; native decoding retains flat
normals. Toggling invalidates pending requests, cached terrain parts, and
frontend preloads; the displayed scene remains until replacement geometry
is ready.

The shader divides slope lighting by the response on level ground, using
`TERRAIN_RELIEF` (0.45) as its strength. Channels with effectively zero base
lighting retain a multiplier of one instead of darkening otherwise flat
ground. The existing lightmap still multiplies the result.

## Textured horizon

The World tab switch keeps terrain textures and their fog blend beyond the
native detail band instead of the flat fog return. Texture mip levels remain
exactly those authored in the source assets. Single-level textures can alias
at grazing angles; this feature does not generate extra levels.

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
