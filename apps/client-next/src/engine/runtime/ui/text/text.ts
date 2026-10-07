/*
===========================================================================

text.ts - the UI's bitmap font: measurement and glyph layout

Owns font admission and two bounded memos, run widths and glyph layouts,
so labels that repeat frame to frame are not laid out again. Each local
layout retains one immutable absolute placement; callers own their lists.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import { decodeUiFont, titleText, titleTextBox, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
import type { FontAtlas, GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { guideContent, type GuideToken } from "@/engine/foundation/ui/guide-content";

const FONT_BYTE_LIMIT = 4 << 20;
const BOARD_EXTRA_HEIGHT = 5;
const MAX_WIDTHS = 2048;
const MAX_LAYOUTS = 4096;

/*
================
UiTextPlacement

One placement per local layout. Input vectors are owned snapshots, so edits
to a caller's arrays cannot turn an old placement into a cache hit.
================
*/
interface UiTextPlacement {
	x: number;
	y: number;
	box: UiRect;
	clip: UiRect;
	quad: UiQuad;
}

/*
================
createUiText

Bitmap metrics own measurement and drawing. No browser font or second
raster atlas can diverge from selection/caret geometry.
================
*/
export function createUiText( assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">, base: string ) {
	// Owned by this font admission; finite FIFO storage survives idle HUD frames.
	const widths = new Map<string, number>(), layouts = new Map<string, UiQuad | null>();
	const placements = new WeakMap<UiQuad, UiTextPlacement>();
	let state: { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; font: FontAtlas; } | {
		kind: "failed";
		message: string;
	} | { kind: "disposed"; } = { kind: "idle" };
	return {
		/*
		================
		step
		================
		*/
		step() {
			if ( state.kind === "loading" ) {
				const result = assets.take( state.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) throw Error( "Retail font resource unavailable" );
						layouts.clear();
						state = {
							kind: "ready",
							font: decodeUiFont(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch ( error ) {
						state = { kind: "failed", message: String( error ) };
					}
					return true;
				}
			}
			if ( state.kind === "idle" && assets.available() > 0 ) {
				state = {
					kind: "loading",
					id: assets.request(
						new URL( "/assets/fonts/native-ui-font-atlas.json", base ).href,
						FONT_BYTE_LIMIT
					)
				};
			}
			return false;
		},
		/*
		================
		height
		================
		*/
		height: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight : 0,
		/*
		================
		boardHeight
		================
		*/
		boardHeight: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight + BOARD_EXTRA_HEIGHT : 0,
		/*
		================
		extentHeight
		================
		*/
		extentHeight: ( index = 0, style = 0 ) => {
			if ( state.kind !== "ready" ) return 0;
			const font = state.font.fonts[String( index ) + (style === 2 ? ":2" : "")]!;
			return font.ascent + font.descent;
		},
		/*
		================
		path
		================
		*/
		path: () => state.kind === "ready" ? state.font.image : null,
		/*
		================
		guide
		================
		*/
		guide(
			tokens: readonly GuideToken[],
			rect: UiRect,
			clip: UiRect,
			color: UiQuad["color"],
			size: ( path: string ) => readonly [number, number] | undefined
		) {
			return state.kind === "ready" ?
				guideContent( state.font, tokens, rect, clip, color, size ) :
				{ quads: [], paths: [], height: 0 };
		},
		/*
		================
		error
		================
		*/
		error: () => state.kind === "failed" ? state.message : null,
		/*
		================
		run
		================
		*/
		run( value: string, fontStyle = 0, fontIndex = 0 ) {
			if ( state.kind !== "ready" ) return { width: 0 };
			const text = value, key = fontIndex + ":" + fontStyle + ":" + text, cached = widths.get( key );
			if ( cached !== undefined ) return { width: cached };
			const font = state.font.fonts[String( fontIndex ) + (fontStyle === 2 ? ":2" : "")]!;
			let width = 0;
			for ( const c of text ) width += drawableGlyph( font.glyphs, c )?.advanceX ?? 0;
			if ( widths.size >= MAX_WIDTHS ) widths.delete( widths.keys().next().value! );
			widths.set( key, width );
			return { width };
		},
		/*
		================
		quads

		Labels repeat frame to frame; glyph layout walks every character. The
		memo is keyed by position modulo whole pixels: layout is exactly
		translation-invariant for an integer shift (Math.round( n + f ) is
		n + Math.round( f ); widths, offsets and clip sizes are unchanged), so a
		label that moves with the minimap or a character is laid out once and
		shifted after. Placed quads own frozen vectors; every caller gets its own list.
		================
		*/
		quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			if ( state.kind !== "ready" ) return [];
			const nx = Math.floor( rect[0] ), ny = Math.floor( rect[1] );
			const localRect: UiRect = [ rect[0] - nx, rect[1] - ny, rect[2], rect[3] ],
				localClip: UiRect = [ clip[0] - nx, clip[1] - ny, clip[2], clip[3] ];
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
				cached = titleText( state.font, value, localRect, localClip, color, style )[0] ?? null;
				if ( layouts.size >= MAX_LAYOUTS ) layouts.delete( layouts.keys().next().value! );
				layouts.set( key, cached );
			}
			if ( !cached ) return [];
			const placed = placements.get( cached );
			if (
				placed && Object.is( placed.x, nx ) && Object.is( placed.y, ny ) &&
				sameRect( placed.box, rect ) && sameRect( placed.clip, clip ) &&
				sameRect( placed.quad.color, cached.color )
			) return emitPlacement( placed.quad );
			const quad = freezePlacement( shiftRun( cached, nx, ny, rect, clip, style.overflow ?? "avoid-overlap" ) );
			placements.set( cached, {
				x: nx,
				y: ny,
				box: Object.freeze( [ ...rect ] ) as UiRect,
				clip: Object.freeze( [ ...clip ] ) as UiRect,
				quad
			} );
			return emitPlacement( quad );
		},
		/*
		================
		box
		================
		*/
		box( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			return state.kind === "ready" ? titleTextBox( state.font, value, rect, clip, color, style ) : [];
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			widths.clear();
			layouts.clear();
			if ( state.kind === "loading" ) assets.cancel( state.id );
			state = { kind: "disposed" };
		}
	};
}

/*
================
shiftRun

A cached run quad moved by a whole-pixel offset, frozen (the text owner's
contract: no caller can change a quad another holds). Its clip is formed
again from the caller's own rectangle with layoutGlyphs' expression (a
difference taken in local coordinates rounds its last bit differently):
the caller's clip itself when the text avoids overlap, else its
intersection with the box. The shortened variant is always clipped to the
intersection. The layout box is the caller's own rectangle.
================
*/
function shiftRun(
	run: UiQuad,
	dx: number,
	dy: number,
	box: UiRect,
	clip: UiRect,
	overflow: NonNullable<GlyphStyle["overflow"]>
): UiQuad {
	const available = Math.max( 0, box[2] ),
		left = Math.max( clip[0], box[0] ),
		right = Math.min( clip[0] + clip[2], box[0] + available );
	const bounded: UiRect = [ left, clip[1], Math.max( 0, right - left ), clip[3] ];
	/*
	================
	moved
	================
	*/
	const moved = ( quad: UiQuad, glyphClip: UiRect ): UiQuad => ({
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

/*
================
sameRect

Object.is preserves signed zero and compares scalar snapshots rather than
caller vector identity.
================
*/
function sameRect( a: UiRect, b: UiRect ): boolean {
	return Object.is( a[0], b[0] ) && Object.is( a[1], b[1] ) &&
		Object.is( a[2], b[2] ) && Object.is( a[3], b[3] );
}

/*
================
freezePlacement

A shared placement owns every mutable vector it exposes. The glyph run is
already deeply frozen by textRunQuad; no caller storage is frozen here.
================
*/
function freezePlacement( quad: UiQuad ): UiQuad {
	const paint = {
		...quad,
		rect: Object.freeze( [ ...quad.rect ] ) as UiRect,
		clip: Object.freeze( [ ...quad.clip ] ) as UiRect,
		color: Object.freeze( [ ...quad.color ] ) as UiRect,
		uv: Object.freeze( [ ...quad.uv ] ) as UiRect
	};
	if ( !quad.textLayout ) return Object.freeze( paint );
	const layout = quad.textLayout;
	return Object.freeze( {
		...paint,
		textLayout: Object.freeze( {
			...layout,
			box: Object.freeze( [ ...layout.box ] ) as UiRect,
			fitted: layout.fitted ? freezePlacement( layout.fitted ) : layout.fitted
		} )
	} );
}

/*
================
emitPlacement

Overlap resolution groups by sidecar identity. Two identical labels are
still two independent emissions, even when their placement geometry is shared.
================
*/
function emitPlacement( quad: UiQuad ): UiQuad[] {
	if ( !quad.textLayout ) return [ quad ];
	return [ Object.freeze( {
		...quad,
		textLayout: Object.freeze( { ...quad.textLayout } )
	} ) ];
}
