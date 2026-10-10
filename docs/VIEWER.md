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
