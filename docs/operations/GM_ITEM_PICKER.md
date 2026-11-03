# GM console F3 item picker

Planned, not implemented. The VSRO GM console opens an item list with F3
(codename + display name); the port has the command lane but not the picker UI.

## Already built

- The GM console opens with Shift+Backquote and Escape (ui.ts consolePhase), is
  GM-gated on `gameplay.eligibility.gm`, and posts
  `{ kind: "gm-command", line }`.
- `engine/foundation/gameplay/gm-command.ts` composes the 0x75B6 frame
  (`/MAKEITEM <codename> <n>`, `/LOADMONSTER`, `/warp`, `/SILK`, ...) and parses
  the 0xB5B6 reply; replies are appended to the console.
- The worker loads the item catalog into `gmItems`
  (`gmItemReferences`, from the world references' `itemCommandReferences`).
- Server lane and `MakeGMItem` are implemented and tested.

## Missing (the F3 picker)

1. **Catalog transfer to the UI.** The catalog lives in the worker. Add a
   one-shot request (a `gm-item-list` command kind, answered once rather than in
   the per-frame presentation) and cache the `{ code, id }[]` in the UI.
2. **F3 handler** in `apps/client-next/src/engine/runtime/ui/ui.ts`, beside the
   console key handling (~line 5945): open the list only when
   `consolePhase !== 0 && eligibility.gm`.
3. **List UI**: scrollable, type-to-filter, joined to display names from the
   commerce item references (`CommerceItemReference.name`) with the codename as
   fallback; reuse the console row drawing (~line 23427).
4. **Enter** sends `{ kind: "gm-command", line: "/MAKEITEM <codename> 1" }`
   (owner decision: spawn one; Esc closes the list, console stays).
5. Tests for filter, name join and selection; then rebuild the client and
   hard-reload (Ctrl+Shift+R) to verify in-game.

## Verification

Open the console as `[GM]100`, press F3, type part of a codename, Enter: the
item should appear on the ground; the console should show the reply.

## Scope note

This is a UI-only addition over an existing, tested command. It does not touch
the wire format or the server. Native provenance: the VSRO GM console's F3 list;
confirm the v1.150 window before treating it as native rather than port-only.
