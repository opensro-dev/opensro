# Graphics modernization - 2026-10-05

Five shipped stages: anisotropic filtering, the presentation pass (FXAA
with a typography guard, adaptive sharpen, vibrance/contrast grade, output
dither), exp2 height fog, water fresnel, garment sheen. The renderer is a
faithful 2005 D3D9 reconstruction, so every stage here is a deliberate,
documented deviation - and each one is tuned, or reverted, through the
named constants in its owning file. Nothing here changes a data format,
a pipeline variant count or the render-bundle structure.

## 1. Anisotropic filtering x16

`apps/client-next/src/engine/runtime/renderer/device/pipelines.ts`

Retail 87cbc0 sets MIN/MAG/MIP to LINEAR with no anisotropy, so ground
and wall texels collapse at grazing angles. The world, lightmap and
user-adjustable samplers request `maxAnisotropy: 16` (the user-adjustable
path only while the Filtering option is ON, because WebGPU requires
all-linear filters for anisotropy). The retail look is one sampler
constant away (`maxAnisotropy: 1`). No measurable frame cost.

## 2. Presentation pass: FXAA, adaptive sharpen, grade, dither

`apps/client-next/src/engine/runtime/renderer/device/finish.ts`

The native client blits the composed offscreen frame to the swapchain
untouched. When enabled, that copy becomes one fullscreen triangle:

- FXAA 3.11 (Timothy Lottes; preset-12 thresholds, 12-step quality walk)
  resolves the aliasing the 2005 rasterizer baked into geometry edges
  without touching the 46-pipeline matrix,
- a gradient-limited unsharp mask sharpens the soft 2005 texture look
  without ringing halos on hard edges (the halo guard reads the raw
  neighbourhood, so it stays off exactly the edges FXAA smoothed),
- a vibrance lift and a smoothstep S-curve grade whose fixed points are
  black, mid gray and white - flat colours and UI panels survive
  byte-for-byte,
- a triangular white-noise output dither, gated on the grade's local
  delta, kills the banding the S-curve would otherwise introduce on
  stretched sky gradients while the fixed points stay byte-exact.

Anti-alias first, sharpen second: the two stages do not fight on edges.

### The typography guard

The UI is composed into the offscreen frame before this pass runs, so
FXAA sees typography - and a one-texel glyph stem does not survive
FXAA's resample (the sharpen's halo guard then leaves the softened edge
alone; net blur). Near-binary edges - a 3x3 sqrt-luma range above
`FXAA_HARD_EDGE` - are typography and UI borders; geometry edges never
reach that range. They pass through FXAA untouched and land exactly
where the sharpen and grade alone put them. The sub-texel blend also
ships at the quality preset (0.25), not the console 0.75, which read as
globally smoothed. The thorough fix - composing the UI after the finish
pass instead of under it - is a frame-order change through
`execution-contract.json`, kept as future work.

Wiring: `device.ts` owns the pass and calls it from
`ColorTarget.present()`; the renderer retains the offscreen frame every
frame it is enabled (`renderer.ts`, `postProcessing || deferred query`).
On by default (`?post-processing=0` on the page URL disables it);
`createDevice()`'s own default stays disabled so device-level tests
keep exercising the byte-exact copy path. Cost: one fullscreen triangle
plus the offscreen frame texture the deferred-particle path already
allocates.

Regression cover: `apps/client-next/tests/browser/finish.test.mjs` -
flat fixed points byte-exact, the near-binary boundary keeps its
sharpen-only value, the soft boundary still takes the sub-texel term,
disabled device takes the plain copy, resize rebuilds the binding.

## 3. Fog: exp2 falloff, height term, horizon tint

`pipelines.ts` fog block + `world-environment.ts` (the environment's
settings.w packs the camera eye height)

The authored start/end uniforms now drive an exp2 falloff with a height
term and a horizon tint instead of the retail D3DFOG_LINEAR ramp
(native sub_4dc920): mid-range stays clear, the horizon softens, peaks
rise out of the haze. The fog factor reaches 1-1/255 at the authored
end, so the distant terrain band's colour match keeps its seam
behaviour (both fog targets take the same tint). A `worldY` varying
(location 11) carries world height; the density e-folds 250 m above the
eye (`FOG_HEIGHT_FALLOFF`). Local fog volumes (dungeon
`material.localFog`) keep their authored colour - only the global fog
takes the tint. The retail ramp is one formula away in the same block.

## 4. Water: fresnel + sky reflection

`pipelines.ts` animated-water block

The 30 authored wave frames (`water1XX`, x/y slope offsets around 0.5)
ship and animate as texture-array layers, but the flat unlit water path
never sampled them. The wave frame now perturbs a fresnel-weighted
reflection between the horizon and zenith colours (the reflected ray
sees zenith looking down, horizon when grazing). The surface is
horizontal, so `(eyeY - worldY) / viewZ` carries the view angle - no
new camera uniform. `WATER_FRESNEL` is F0 (0.02), `WATER_WAVE_SLOPE`
the slope weight. Deleting the `if(animated)` block restores the flat
authored look.

## 5. Garment sheen from authored DXT3 alpha

`loader.ts` decoration, `device/geometry.ts` packing (`reflection.z`),
the geometry uber-shader, the `WorldMaterial` contract

jmxAssetIO documents it: CPrimMtrl bit 0x200 off means the DXT3 alpha
is sheen, not coverage. The NTX decode knows the container format, so
the loader stamps opaque bc2 (DXT3) character primitives with
`material.sheenAlpha` after decode; the shader reads tex.a as a
Blinn-Phong gloss (`SHEEN_POWER`, `SHEEN_STRENGTH`) against the fixed
45-degree sun the diffuse term already uses. The view vector is the
camera forward - exact at the frame centre. DXT1 parts are excluded by
the format gate, hair cutouts by the opacity gate.

## Verification

- `pnpm --filter @sro/client-next verify:quick` (typecheck, ownership,
  capabilities, execution map), `verify:test-types`, `pnpm task
  check:format`: all PASS.
- Browser tests against a local dev server (`SRO_PROBE_CLIENT_NEXT_BASE_URL`):
  `tests/browser/finish.test.mjs` and `tests/browser/bloom.test.mjs`
  PASS. The device compiles the modified geometry uber-shader before its
  running phase, so each pass also proved the WGSL valid.
- Runtime tests: `tests/runtime/world-environment.test.mjs`,
  `tests/runtime/world-surfaces.test.mjs` PASS - the fog uniform packing
  change broke none of the pinned behaviour.
