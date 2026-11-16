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
preloadCursors

Fetches and decodes every world cursor at once (retail loads them all
when its cursor manager starts) and calls loaded for each as it is
ready. load is injected for tests; the page uses decodedImage.
================
*/
export function preloadCursors(
	load: ( url: string ) => Promise<unknown>,
	loaded: ( value: WorldCursor ) => void
): Promise<void> {
	return Promise.all(
		worldCursors().map( value =>
			load( cursorAssetUrl( value ) ).then( () => loaded( value ), () => {
				// A cursor whose image failed still has to stand when asked
				// for: let the rule name it, so the browser retries the fetch
				// and draws its fallback meanwhile, as before this preload.
				loaded( value );
			} )
		)
	).then( () => undefined );
}
