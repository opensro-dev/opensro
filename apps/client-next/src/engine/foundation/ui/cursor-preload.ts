/*
===========================================================================

cursor-preload.ts - every world cursor ready before it is shown

Retail loads all its cursor resources when the cursor manager starts, so
a glyph change is immediate. The browser fetches a CSS cursor image only
when a rule first names it and draws its own arrow until it arrives. The
cursor owner (runtime/platform/ui/cursor.ts) preloads through these and
never names a glyph that is not ready.

===========================================================================
*/
import { cursorAssetUrl, worldCursors } from "../assets/native-assets";
import type { WorldCursor } from "./world-cursor";

/*
================
presentableCursor

The glyph to stand for a wanted one: the wanted glyph once its image is
decoded, else the one already shown, so the browser never falls back to
its own arrow while a cursor image is still on its way.
================
*/
export function presentableCursor(
	wanted: WorldCursor,
	shown: WorldCursor,
	ready: ReadonlySet<WorldCursor>
): WorldCursor {
	return ready.has( wanted ) ? wanted : shown;
}

/*
================
cursorPreloadUrls

Every world cursor with its image URL, in worldCursors order: the
set the cursor owner fetches and decodes when it starts (retail loads
them all when its cursor manager starts).
================
*/
export function cursorPreloadUrls(): readonly (readonly [WorldCursor, string])[] {
	return worldCursors().map( value => [ value, cursorAssetUrl( value ) ] as const );
}
