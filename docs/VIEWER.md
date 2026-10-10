# The embeddable 3D viewer (V1)

Port-only, not native: the original has no web viewer. This page draws one
monster or one character with the client's own renderer, alone in an iframe,
with no game session and no login. It serves the community website
(opensro-web `docs/COMMUNITY.md`, V1): profile pages, unique cards and the
catalogue's monster portraits.

Status: in progress on `feat/viewer-v1`. Done: the request parser and fixed
reasons (`foundation/ui/viewer-request.ts`), and the framing camera
(`foundation/rendering/viewer-camera.ts`), both tested. Next: the runtime
owner and the renderer output. The order of work is monster + still first
(the catalogue's portraits), then `look` characters, then drag and zoom.

## Contract

`viewer.html?monster=<refObjId>` or `viewer.html?look=<base64url JSON>` (the
P6 `look` object: `bodyRefObjId`, `worn[]`, `avatar[]`).

- `&still=1` draws one frame and stops. `&size=` sets the square still in
  pixels: 64..1024, default 384.
- The background is always transparent.
- Camera: a fixed three-quarter front view (35 degrees off the model's front,
  a slight downward pitch), framed to the model's bind-pose bounds with a
  margin. The model is in its idle pose at time 0 for a still.
- When the model is drawn, the page posts `{ type: "viewer-ready" }` to its
  parent or opener. A still also posts `{ type: "viewer-still", png }` (a PNG
  data URL) and resolves `window.__viewerStill` with the same data URL.
- On failure the page posts `{ type: "viewer-error", reason }` and rejects
  `window.__viewerStill` with `Error(reason)`. `reason` is one of a fixed
  set, exported from `viewer-request.ts`:
  `no-webgpu`, `bad-request`, `unknown-monster`, `asset-load`, `render`,
  `timeout` (not drawn within 30 s).

## Design

The client has one page entry (`index.html` → `src/bootstrap.ts` → the sealed
`runtime.ts`), and drawing goes through the world/session pipeline. The
viewer is a second entry that shares the renderer and the asset owners but
none of the session:

- `viewer.html` → `src/viewer.ts` (entry; side effects live here, as
  `bootstrap.ts` holds the game's). Listed beside `index.html` in the vite
  build inputs.
- `foundation/ui/viewer-request.ts` (pure): parses and validates the query
  into a request, and owns the reason enum. Tested without a browser.
- `foundation/rendering/viewer-camera.ts` (pure): the fixed three-quarter
  camera from a model's bounds and the output aspect.
- `runtime/viewer/viewer.ts` (owner): creates the asset owner and the
  renderer, resolves the request to a presentation resource (the same
  presentation catalogue the game uses:
  `characters/presentation-catalog.ts`, keyed by `refObjId`), installs that
  one model and its idle clip (`setCharacterModel`, `setCharacterAnimation`),
  publishes one actor (`setCharacterActors`) and the camera
  (`setCharacterPreview`), and drives frames until the model is drawn.
- **Renderer option, transparent output:** `createRenderer` gains an explicit
  `{ transparent: true }` option. It configures the canvas `alphaMode:
  "premultiplied"`, clears the scene target to alpha 0, and skips the sky,
  terrain, fog and post passes that would write alpha 1. The game never sets
  it, so its frame is unchanged; a test pins that.
- **Still readback:** after the first frame with the model drawn, the viewer
  copies the canvas texture to a buffer (no `preserveDrawingBuffer` in
  WebGPU) and encodes the PNG.

### Next step: the portrait path (found 2026-10-11)

`renderer/frame/frame.ts` already has a portrait branch: an offscreen
character render with a transparent clear (`clearValue` alpha 0), which the
HUD portraits use (`renderer/characters/portrait.ts`, `createPortrait`).
Try that first for the still, since it is transparent by construction and
needs no new main-pass mode. Check two things:
- whether its output texture can be copied to a buffer for the PNG;
- whether it takes an arbitrary camera (portrait.ts fixes yaw, pitch and
  distance for the HUD).

If both hold, the `{ transparent: true }` canvas option above is needed only
for the interactive iframe, not for stills.

Checked 2026-10-11:
- The portrait target is a fixed 128-pixel texture ("portrait-128" in
  frame.ts), and the equipment doll is its own target.
- The portrait camera is hard-coded to the head: `PORTRAIT_DISTANCE` 6.5,
  `PORTRAIT_PITCH` 0.2, field of view pi/6.
- The doll's camera takes only `{ yaw, seconds, aspect }` (portrait.ts
  `prepare`, line ~200).

Route found (2026-10-11): the inventory doll is the right path, not the HUD
portrait. The UI scene names it (`UiScene.doll = { gid, yaw }` plus a quad
with `doll: true`). The renderer then prepares `characters.portraitSource(gid)`
through the doll's `createPortrait` into a `__doll` texture of the quad's size,
with a transparent clear (renderer.ts ~605..625, frame.ts "equipment-doll").
The monster still needs four additive changes:
1. `UiScene.doll.camera?: WorldCamera`, passed through to `prepare`'s doll
   branch in place of its fixed `eye [0, 9, -40]` camera. Models face -z at
   the doll's yaw, so `viewerCamera(bounds, [0, -1])`.
2. A renderer method that copies the `__doll` texture to a buffer and
   resolves RGBA. This is the still's readback, transparent by construction.
3. The viewer feeds one monster entity at the origin through the runtime
   character owner (runtime/characters/characters.ts), which already
   resolves the presentation resource, loads the model into the renderer
   and produces `portraitSource(gid)`. No new model loader.
4. A UI scene holding one full-size doll quad, no other UI.
Progress: step 1 is done (c9cfc496): the doll input's `camera`, with no change for the inventory.
Step 2 details:
- The portrait and doll targets come from `device/ui.ts` `portraitTarget(id, width, height)`, with usage `TEXTURE_BINDING | RENDER_ATTACHMENT`. The readback needs `COPY_SRC` added, harmless for the HUD.
- The format is the scene format (`sceneFormat()`), which is a float format with the HDR stage on, so the readback converts to 8-bit RGBA, keeping alpha.
- Copy with `copyTextureToBuffer` (rows padded to 256 bytes) and `mapAsync`, as `device/timing.ts` does for its queries.

The main canvas pass stays opaque. The PNG comes from the doll texture, so
stills need no transparent canvas mode at all.

Earlier plan, superseded by the route above: give `prepare` an optional explicit camera (`viewer-camera.ts`
output) and the renderer an optional still target of the request's size,
leaving the HUD calls' arguments and results unchanged (pin that with a
test). Then read the still target back into the PNG.

## Serving

The vite dev server already serves the generated asset tree, so the viewer
works there first. A static build (`vite preview` of `dist`) is confirmed
before the PR merges and written here.

## Tests

- `viewer-request` parsing: every reason, size bounds, base64url `look`.
- `viewer-camera`: framing for a tall, a wide and a tiny model.
- The renderer option: off is byte-identical to today's frame setup.
- One browser test (run alone, never with the full browser suite) drawing
  one shipped monster in still mode.
