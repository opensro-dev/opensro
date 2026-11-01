# Experimental graphics

The renderer reconstructs the 2005 D3D9 look. Every stage here is a deliberate, port-only deviation behind an Experimental option, so the defaults stay native unless a stage says otherwise.

## Anisotropy, presentation, fog, water and sheen

Five shipped stages: anisotropic filtering, the presentation pass (FXAA
with a typography guard, adaptive sharpen, vibrance/contrast grade, output
dither), exp2 height fog, water fresnel, garment sheen. A sixth change on
the asset side ships the world's block textures as authored. The renderer is
a faithful 2005 D3D9 reconstruction, so every stage here is a deliberate,
documented deviation - and each one is tuned through the named constants in
its owning file. Nothing here changes a data format, a pipeline variant count
or the render-bundle structure.

**Every renderer stage is an opt-in under Experimental > Video, off by
default.** Off is the native 2005 frame: the plain swapchain copy, retail
samplers (`maxAnisotropy: 1`) and D3DFOG_LINEAR. The
preferences are saved with the other Experimental options; the renderer
switches the presentation pass and the samplers at run time, and the shader
stages read `env.stages` (x height fog, y sun direction; z and w are retired).
Water reflection and equipment shine are native Video options, not stages here.
The asset-side block textures (section 6) keep the authored pixels and are
not a toggle.

### 1. Anisotropic filtering x16

`apps/client-next/src/engine/runtime/renderer/device/pipelines.ts`

Retail 87cbc0 sets MIN/MAG/MIP to LINEAR with no anisotropy, so ground
and wall texels collapse at grazing angles. The world, lightmap and
user-adjustable samplers request `maxAnisotropy: 16` (the user-adjustable
path only while the Filtering option is ON, because WebGPU requires
all-linear filters for anisotropy). The retail look is one sampler
constant away (`maxAnisotropy: 1`). No measurable frame cost.

### 2. Presentation pass: FXAA, adaptive sharpen, grade, dither

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

#### The typography guard

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
Off by default: Experimental > Video > Anti-aliasing and color grade turns it
on. The page URL no longer carries a switch. Cost when on: one fullscreen
triangle plus the offscreen frame texture the deferred-particle path already
allocates.

Regression cover: `apps/client-next/tests/browser/finish.test.mjs` -
flat fixed points byte-exact, the near-binary boundary keeps its
sharpen-only value, the soft boundary still takes the sub-texel term,
disabled device takes the plain copy, resize rebuilds the binding.

### 3. Fog: exp2 falloff, height term, horizon tint

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

### 4. Water: fresnel + sky reflection

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

### 5. Garment sheen from authored DXT3 alpha

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

### 6. World object textures shipped as authored GPU blocks

`scripts/build/world/objects/buildTitleSectorObjectResources.mjs`

The world's DDJ material textures are already authored in DXT (BC) block
form - the 2005 client decompressed them on load and uploaded expanded
RGBA. The publisher now probes each DDJ's DDS header and ships every
power-of-two fourCC source (DXT1/DXT3/DXT5) verbatim as an NTX1
`.texture` container - authored blocks byte-for-byte plus a box-filtered
mip suffix (`scripts/build/native_texture_mips.py -Manifest`, the same
encoder the character pipeline already uses). Non-block and non-PoT
sources keep the converted PNG path unchanged.

For the outdoor title sector: 2,671 of 2,689 material textures qualify
(1,944 DXT1 + 727 DXT3), 18 stay PNG. The published tree drops from
219 MB to 135 MB (-38%), the client stops decoding 2,671 images on the
CPU during world load (the NTX1 route uploads authored blocks directly),
and VRAM for those textures drops from ~664 MB expanded RGBA8 to
~129 MB block-compressed. No renderer change: the client's existing
`.texture` decode route (`world.ts`) admits the containers; materials
reference the `.texture` public path instead of the `.png`.

The retail look is byte-identical where it matters - the authored blocks
are the same pixels the 2005 client uploaded - and the mip suffix only
adds the levels the native minification filter wanted. Revert: the probe
(`probeBlockTexture`) returning null restores the PNG path for every
texture.

### Verification

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
- World block textures: one-region rebuild then full outdoor republish,
  then a four-way audit - every material's `texturePublicPath` exists on
  disk, every published `.texture` matches the manifest hash, the packs
  serve the admitted bytes, and
  `tests/runtime/world-stream.test.mjs` proves the published container
  decodes to a native `bc1-rgba-unorm` GPU resource with its mip chain
  through the stream (0 textures left pending).

## Sunlight and bloom

These stages are **port-only, not native**. Each is off by default in
Experimental. Turning them off preserves the existing palette, fixed light,
terrain shading, distant fog band, and native bloom chain.

### Moving sunlight

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

### Smooth bloom

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

### Verification scope

The environment unit tests cover the appended direction, its unit length,
and continuity across both horizon crossings. Terrain normal tests cover
flat ground, ramps, and border differences. Browser verification must cover
each switch separately, restoration after disabling, float bloom resize and
native handback, and exact default pixels against the reviewed baseline.
Check the PR validation record for commands and results actually run; this
document does not assert a pass from an earlier contributor session.

## HDR, shadows and per-pixel light

Three stages of the renderer roadmap's third track (issue #273): the HDR
intermediate with a filmic tone map, one sun shadow cascade, and per-pixel
character and object lighting. The renderer stays a faithful 2005 D3D9
reconstruction; every stage here is a deliberate, documented deviation,
tuned through the named constants in its owning file.

**Every stage is an opt-in under Experimental, off by default.** Off is
the native 2005 frame, byte for byte: the same pipelines (no rgba16float
sibling ever compiles), the same pass order, and `env.stages2` zeroed in
the shader. The window's Image tab gains its fourth row (HDR tone map) and
a new Lighting tab holds the two direct-light stages; World keeps its four
rows, Chat and Developer shift one tab later.

The environment block's port-only tail grows by the second stages vec4
(byte 368: x HDR, y sun shadow, z per-pixel lighting) and the cascade
matrix (byte 384); the layout table lives in
`apps/client-next/src/engine/runtime/renderer/device/environment-block.ts`.

### 1. HDR intermediate and filmic tone map

`apps/client-next/src/engine/runtime/renderer/device/hdr.ts`,
`pipelines.ts`, `bloom.ts`, `geometry.ts`, `device.ts`, `frame.ts`

Experimental > Image > HDR tone map renders the scene into an
`rgba16float` intermediate instead of the 8-bit frame, and one fullscreen
pass resolves it through Narkowicz's fitted ACES curve (2015) into the
presented frame. The 2005 frame is authored display-referred LDR, so the
only values above 1.0 are the stages that lift them there - here the sun
disc gain (`SUN_HDR_GAIN`, pipelines.ts; the retail clamp stands when the
stage is off) - and there is no linear-to-sRGB re-encode on output; the
curve's job is the highlight roll-off and the filmic mid lift, and
`TONEMAP_EXPOSURE` is the whole compensation surface.

The structural cost is honest: the scene's geometry pipelines need
rgba16float siblings, and they compile lazily on the stage's first enabled
use (the float bloom rule - a native-only device never compiles them).
`geometry.sceneBindings` walks every live draw to its float pipeline
sibling and rebinds it (the `textureOptions` walk's pattern), the water
mirror and the portrait targets reformat to follow, and the silhouette
receivers build their float variant on demand. The UI never touches the
intermediate: with bloom on, the chain's copy-back step becomes the tone
map (`bloom.ts`'s lazily compiled `original` sibling, so the glow still
composites after the roll-off); without bloom, `hdr.ts`'s own pass runs
between the scene and the lens flares. The character preview uses a
transparent float intermediate after background UI, then tone-maps and
composites over it before foreground UI. Scene depth remains available to
world labels and flare visibility before preview clears its own depth.
`geometry.hdrPreview` derives coverage-alpha siblings only for that fullscreen
compositor. HUD portraits and inventory dolls retain authored alpha because
their textures are consumed by the ordinary UI shader, not the HDR resolve.

### 2. One sun shadow cascade

`apps/client-next/src/engine/runtime/renderer/device/sun-shadow.ts`,
`sun-shadow-math.ts` (foundation), `pipelines.ts`, `geometry.ts`,
`renderer.ts`

Experimental > Lighting > Sun shadows renders one depth-only pass of the
frame's opaque casters - world objects and characters; terrain, water and
everything blended stays out - from the shading light's position, and the
world shader resolves receivers with a 3x3 kernel of hardware comparison
taps. The cascade follows the same light the shader shades with (the
pinned retail diagonal, or the packed arc direction under Moving
sunlight), covers 96 metres around the eye (`SHADOW_EXTENT` is 960 native
world units, ten units per metre) at `SHADOW_SIZE`
taps, and its centre snaps to whole cascade texels (`sun-shadow-math.ts`)
so a walking camera does not shimmer the edges. The caster pass carries
the world shader's D3D9 alpha test (12-bit truncation, compare byte), so
leaf cards cast leaves, with `SUN_SHADOW_BIAS` slope-scaled bias against
acne.

Receivers keep their ambient: the cascade shades the diffuse term of the
world path, the object path and the lightmap terrain alike (the baked sum
is terrain's diffuse), fading to unshadowed at the cascade border
(`SUN_SHADOW_FADE`). Water and sky never receive. The native per-character
silhouette projections stand untouched beside it - this complements, it
does not replace. Every world draw binds the cascade at bindings 13/14;
while the stage is off those name a 1x1 dummy depth view no gated branch
ever samples, and the whole owner (shader, pipelines, cascade texture)
builds only on first enabled use. The selected view is bound independently
of the last prepared frame, including when an enabled preference is restored
during device startup or recovery. Local portrait and preview geometry does
not receive the world cascade.

Known follow-up (this wave's scope): the caster list is the frame's opaque
draws without distance culling; a city scene pays for every object again
in the depth pass.

### 3. Per-pixel character and object lighting

`apps/client-next/src/engine/runtime/renderer/device/pipelines.ts`

Experimental > Lighting > Per-pixel character light. The retail vs_1_1
path saturates each vertex's lighting before interpolation (`oD0`,
A5D450's diagonal in object space), so a 2005 character shows its
triangles under strong light. The stage re-evaluates the same terms - the
same diffuse and ambient factors, the same light - per pixel from the
interpolated, skinned world normal the shader already carries. Nothing
else changes: the vertex point-light path
(`NATIVE_CHARACTER_LIGHTING`, a compatibility mode of its own, off) stays
per-vertex.

### Regression coverage

`tests/browser/hdr-stages.test.mjs` drives a synthetic ground, smooth-normal
dome and casting box through the production renderer. It cycles each stage
and their combination with float bloom, checks exact restoration when
disabled, and requires both lit and shaded ground. It also destroys the GPU
device with HDR enabled and compares the recovered frame. The reusable
fixture lives in `tests/helpers/hdr-gpu.mjs` so the same capture can run
against a main checkout for native-off comparison.

Runtime regressions cover retained startup flags, binding changes before
the first cascade preparation, preview receiver suppression, absent and
live water targets, maximum camera zoom, wrapped alpha-test sampling,
render-target compatibility, deferred particles, retained label depth and
pass ordering. The PR review records the exact tested head and results;
historical results from an older base are not evidence for the current head.
