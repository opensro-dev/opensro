# The embeddable 3D viewer (V1)

Port-only, not native: the original has no web viewer. This page draws one
monster or one character with the client's own renderer, alone in an iframe,
with no game session and no login. It serves the community website
(opensro-web `docs/COMMUNITY.md`, V1): profile pages, unique cards and the
catalogue's monster portraits.

Status: monster stills, `look` characters and the interactive view (drag to turn, wheel to
zoom) work in the dev server and in a production build.

## Contract

`viewer.html?monster=<refObjId>` or `viewer.html?look=<base64url JSON>` (the
P6 `look` object: `bodyRefObjId`, `worn[]` of `{slot, refItemId, plus}`,
`avatar[]` of `{slot, refItemId}`; sockets as the server's `inventory/slots.go`).

- `&still=1` draws one picture and stops. `&size=` sets the square still in
  pixels: 64..1024, default 384. A still fits the whole model (92% of its
  longer axis).
- `&batch=1` (a monster still only, for the catalogue's portraits) keeps the
  page after its still, or after its failure. `window.__viewerNext(refObjId)`
  then draws the next monster in its place and returns that monster's still
  promise, so the renderer and shared assets load once for a whole batch. Each
  monster gets a new entity gid, since the character owner keeps per-gid state.
  A batch page that fails to render or answers nothing is reloaded by its caller.
- Without `still`, the view is interactive. The picture takes the iframe's own box
  at device pixels (longer side at most 1024) and fills 88% of its height,
  centred. A long weapon may leave the sides. Drag turns the model, and the wheel
  zooms (0.4..2.5x from that fit). The page draws only while something changes:
  an idle view asks for no animation frames, so several embeds cost nothing at rest.
- The background is always transparent. The embedding iframe must set
  `color-scheme: normal` (or the same scheme as the viewer page, which declares
  none). Otherwise Chrome paints an opaque backdrop behind a frame whose scheme
  differs from its parent's.
- Camera: a fixed three-quarter front view (35 degrees off the model's front,
  a 12-degree downward pitch). It is framed to the clip pose's box, then fitted
  to the drawn pictures' opaque pixels (`nextFraming`, viewer-camera.ts): a
  picture that reaches the border is drawn again from farther away, and one off
  the fill is refit and measured again, until it is framed (a few passes at most;
  a ride or a wing far off the target's depth needs more than one). The model
  stands in its idle pose (a dressed look holds its weapon's armed idle).
- Light: the game's noon light (`VIEWER_LIGHT`, viewer.ts): the shipped
  environment's midday diffuse (0.285) and ambient (0.78), as the inventory
  doll is lit at noon, with the key turned to come from in front of the model
  and above (the viewer's only liberty). Without it the page, which draws no
  world, would light the model with the empty environment's white ambient. A
  brighter invented key once turned pale armour white through the equipment
  reflection stage. The renderer applies it only for the viewer page
  (`setViewerLight`); the game's inventory and party portraits keep the native
  light.
- When the model is drawn, the page posts `{ type: "viewer-ready" }` to its
  parent or opener. A still also posts `{ type: "viewer-still", png }` (a PNG
  data URL). `window.__viewerStill` resolves with the PNG of the first picture,
  in both modes.
- On failure the page posts `{ type: "viewer-error", reason, stage }` and rejects
  `window.__viewerStill` with `Error(reason)`, whose `cause.stage` is the same stage.
  `reason` is one of a fixed set, exported from `viewer-request.ts`:
  `no-webgpu`, `bad-request`, `unknown-monster`, `asset-load`, `render`, `timeout`.
  A look whose body or any item the game does not have fails `bad-request`.
  `stage` says how far the draw got: `request`, `catalogue` (the presentation
  catalogue loading), `model` (the model loading), `first-frame` (framed, not yet
  read), or `readback`.
- `timeout` means not drawn within 30 s of drawing time. A hidden page gets no
  animation frames, so its time does not count, and each frame counts at most 100 ms.
  A page that never becomes visible never finishes: draw it on screen, or headless
  with background throttling off.

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
- A look is a `player` entity with the look's body. It spawns bare, which starts
  the catalogue loading. Once the catalogue is in, it is dressed with each item
  typed by its packed type word, published per item as `typeFlags` in
  `missionPresentation.json` by `buildMissionPresentationAsset.mjs` (packed as the
  server's `characterTidWord`). The weapon's type picks the armed idle, as in the
  world.
- `characters/viewer-queries.ts` is the viewer's read-only window on the
  presentation: `knows` (the catalogue has the model), `itemType`, and `dressed`
  (every worn part is resident and worn, so the picture is never taken half-dressed).
- The interactive view paints each readback into a visible 2D picture canvas. The
  WebGPU canvas stays hidden, so both modes show the same transparent doll pixels,
  and the game's opaque main pass is untouched. `runtime.ts` schedules frames only
  while `frame()` answers `draw`. Input calls the runtime's `wake`.
- A doll quad's camera is part of the UI quad's identity
  (`foundation/ui/ui-equality.ts`); otherwise a zoom alone would reuse the
  previous quad.
- A hidden page gets no animation frames, so the viewer draws only when visible. An
  embedding iframe must be on screen, and a headless capture runs with background
  throttling off (`launchProbeBrowser`).

## Serving

`viewer.html` is a build input beside `index.html`. Both load the one `bootstrap`
bundle, which branches on `data-page`. Checked 2026-10-11: `vite build` emits
`dist/viewer.html`, and `vite preview` of that build draws the same look still as
the dev server. The beta package builder (`pnpm build:beta`) does not list the page
yet. Adding it there is the step that puts the viewer on the live site.

## Tests

- `tests/runtime/viewer-request.test.mjs`: every reason, size bounds, base64url `look`.
- `tests/runtime/viewer-camera.test.mjs`: a tall, a wide and a tiny box fill the view
  without leaving it, from the fixed angle.
- `tests/runtime/texel-readback.test.mjs`: padded rows in the three portrait formats.
- `tests/browser/viewer-still.test.mjs`, run alone against a dev server, never with the
  whole browser suite:
  - Tiger Girl (1954) as a 256-pixel transparent still that spans most of the frame;
  - an unknown id failing `unknown-monster`;
  - a dressed look as a still, and an unknown item failing `bad-request`;
  - the interactive view asking for no frames when idle and redrawing on a drag.
- `tests/runtime/ui-equality.test.mjs`: a doll quad whose camera moved is a new quad.
