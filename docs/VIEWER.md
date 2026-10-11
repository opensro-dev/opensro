# The embeddable 3D viewer (V1)

Port-only, not native: the original has no web viewer. This page draws one
monster or one character with the client's own renderer, alone in an iframe,
with no game session and no login. It serves the community website
(opensro-web `docs/COMMUNITY.md`, V1): profile pages, unique cards and the
catalogue's monster portraits.

Status: in progress on `feat/viewer-v1`. Monster stills work: `viewer.html?monster=<refObjId>&still=1`
draws the model through the game's renderer into a transparent PNG. Still to do: `look` characters,
then the interactive iframe (drag and zoom), then a static-build check.

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

The client has one ownership root (`src/engine/ownership.json`): one page entry,
`src/bootstrap.ts`, owns `runtime.ts`, and every runtime owner (assets, renderer,
characters) has exactly one owner. A second page entry could not reach those owners,
so the viewer is a second start-up path inside the same tree:

- `viewer.html` loads the same `src/bootstrap.ts` and marks its root
  `data-page="viewer"`. Bootstrap then calls `runtime.ts` `startViewerRuntime`
  instead of `startRuntime`. `viewer.html` is a vite build input beside `index.html`.
- `runtime.ts` `startViewerRuntime` creates only the owners one model needs (assets,
  renderer, character presentation, built as `startRuntime` builds them), runs the
  animation-frame loop (only `runtime.ts` may call `requestAnimationFrame`), and disposes
  the owners when the viewer is done. It is appended after `startRuntime`; the sealed frame
  is byte-identical, and the frame barrier is re-sealed with that proof.
- `runtime/viewer/viewer.ts` (owned by `runtime.ts`) decides each frame. It feeds one
  monster entity through the character presentation, so the model resolves and loads
  exactly as in the world. It frames the model, draws it as the inventory doll, reads the
  doll back, encodes the PNG and reports to the embedding page. It is a declared async and
  `OffscreenCanvas` owner (`tools/verify-capabilities.mjs`), and its calls into the
  renderer and the presentation are declared in `execution-contract.json`.
- Pure helpers: `foundation/ui/viewer-request.ts` (query and reasons),
  `foundation/rendering/viewer-camera.ts` (the camera that fits a box's eight corners to
  the square view from the fixed angle), `foundation/rendering/texel-readback.ts`
  (unpacking padded GPU rows).

### The doll route

- The UI scene's doll quad carries an optional `camera` (contracts `UiQuad.doll`). The
  sanitizer (`foundation/ui/ui.ts`) and the renderer pass it to `portrait.ts`, in place of
  the inventory's fixed camera. The inventory never sets it.
- The doll draws the model at scale 1, turned by pi (`previewYaw(0)`), in its `stand`
  clip's first frame. `Renderer.characterBounds(gid)` answers that pose's bounds
  (`characterBindBounds(model, clip)`), in model space. The viewer flips x and z into the
  doll's space before framing.
- `Renderer.readDoll()` → `device/ui.ts` `readPortrait` → `device/portrait-readback.ts`:
  one `copyTextureToBuffer` and `mapAsync`. The staging buffer retires through the
  device. Portrait targets carry `COPY_SRC`. The still is transparent by construction,
  and the main canvas stays opaque.
- Readiness: the character owner's `ready(gid)` says the model is displayed, and its
  `knows(entity)` says whether the admitted catalogue has the model at all. That is how
  an unknown id fails at once with `unknown-monster`, not after the timeout.
- A hidden page gets no animation frames, so the viewer draws only when visible. An
  embedding iframe must be on screen, and a headless capture runs with background
  throttling off (`launchProbeBrowser`).

## Serving

The vite dev server already serves the generated asset tree, so the viewer
works there first. A static build (`vite preview` of `dist`) is confirmed
before the PR merges and written here.

## Tests

- `tests/runtime/viewer-request.test.mjs`: every reason, size bounds, base64url `look`.
- `tests/runtime/viewer-camera.test.mjs`: a tall, a wide and a tiny box fill the view
  without leaving it, from the fixed angle.
- `tests/runtime/texel-readback.test.mjs`: padded rows in the three portrait formats.
- `tests/browser/viewer-still.test.mjs`, run alone against a dev server, never with the
  whole browser suite: Tiger Girl (1954) as a 256-pixel transparent still that spans
  most of the frame, and an unknown id failing `unknown-monster`.
