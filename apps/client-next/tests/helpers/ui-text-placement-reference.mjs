/*
===========================================================================

ui-text-placement-reference.mjs - ungated absolute placement oracle

Keeps the pre-cache translation expressions and local-layout LRU. The test
injects this constructor at the UI's text dependency without rewriting UI code.

===========================================================================
*/
import "./native-source-loader.mjs";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
const { createUiText } = await import( "../../src/engine/runtime/ui/text/text.ts" );
const { decodeUiFont, titleText } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const font = decodeUiFont( JSON.parse(
	readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json", "utf8" )
) );
const MAX_LAYOUTS = 4096;

/*
================
createReferenceUiText
================
*/
export function createReferenceUiText( assets, base ) {
	const owner = createUiText( assets, base ), layouts = new Map();
	return {
		...owner,
		/*
		================
		quads
		================
		*/
		quads( value, rect, clip, color, style = {} ) {
			if ( !owner.path() ) return [];
			const nx = Math.floor( rect[0] ), ny = Math.floor( rect[1] );
			const localRect = /** @type {import("../../src/engine/contracts/ui").UiRect} */ (
					[ rect[0] - nx, rect[1] - ny, rect[2], rect[3] ]
				),
				localClip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ (
					[ clip[0] - nx, clip[1] - ny, clip[2], clip[3] ]
				);
			const key = [
				value,
				localRect,
				localClip,
				color,
				style.fontIndex,
				style.fontStyle,
				style.hAlign,
				style.vAlign,
				style.overflow
			].join( "|" );
			let cached = layouts.get( key );
			if ( cached !== undefined ) {
				layouts.delete( key );
				layouts.set( key, cached );
			} else {
				cached = titleText( font, value, localRect, localClip, color, style )[0] ?? null;
				if ( layouts.size >= MAX_LAYOUTS ) layouts.delete( layouts.keys().next().value );
				layouts.set( key, cached );
			}
			return cached ? [ referenceShift( cached, nx, ny, rect, clip, style.overflow ?? "avoid-overlap" ) ] : [];
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			layouts.clear();
			owner.dispose();
		}
	};
}

/*
================
referenceShift

Keep the original arithmetic order, including absolute clip intersection.
================
*/
function referenceShift( run, dx, dy, box, clip, overflow ) {
	const available = Math.max( 0, box[2] ),
		left = Math.max( clip[0], box[0] ),
		right = Math.min( clip[0] + clip[2], box[0] + available );
	const bounded = [ left, clip[1], Math.max( 0, right - left ), clip[3] ];
	/*
	================
	moved
	================
	*/
	const moved = ( quad, glyphClip ) => ({
		...quad,
		rect: [ quad.rect[0] + dx, quad.rect[1] + dy, quad.rect[2], quad.rect[3] ],
		clip: glyphClip
	});
	const shifted = moved( run, overflow === "avoid-overlap" ? clip : bounded );
	if ( !run.textLayout ) return Object.freeze( shifted );
	const fitted = run.textLayout.fitted;
	return Object.freeze( {
		...shifted,
		textLayout: {
			box,
			fitted: fitted ? Object.freeze( moved( fitted, bounded ) ) : fitted
		}
	} );
}

export { createReferenceUiText as createUiText };
