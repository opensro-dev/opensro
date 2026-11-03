# UI gallery

A local developer page that opens any of the browser client's windows
directly, over fake data, with no game server. Use it to look at a window,
click its real controls, and compare it with the original client, without
logging in and setting up the situation in game.

Every entry mounts the production UI owners (`runtime/ui`, the renderer, the
asset loader and the input bridge) inside `frame.html`, seeded with local
snapshots. Outgoing commands go nowhere: the server routes are refused, and
nothing a window sends is answered as a success.

## Run it

Prepare the game assets as in `docs/guides/GETTING_STARTED.md`, then from the
repository root:

```
node node_modules/vite/bin/vite.js --config apps/client-next/tools/ui-gallery/vite.config.mjs
```

Open <http://127.0.0.1:5191/>. Pick a window on the left. Reset reloads it,
and the buttons above the frame run its actions (open, close, advance the
clock). The URL hash names the window, so a link opens the same one.

## Files

- `scenarios.ts`: the window list (`journeys`) and the focused HUD, guild
  and stall journeys.
- `extended.ts`, `recipes-*.ts`, `recipe-input.ts`: the other windows and
  how each is opened.
- `entry-recipes.mjs`, `entry-evidence.mjs`: opening steps, and the controls
  that show a window really opened.
- `fixture-state.ts`, `frontend-fixture.ts`, `frontend-stage-fixture.ts`:
  the fake snapshots and the character-select stage.
- `frame.ts`, `frame.html`: mounts the production UI for one entry.
- `gallery.ts`, `index.html`, `gallery.css`: the page around it.

`tsconfig.json` type-checks the tool apart from the client.

Based on the UI/HUD QA sandbox by Diego Arias (#785), cut down to the
gallery.
