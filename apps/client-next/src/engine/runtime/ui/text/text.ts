/*
===========================================================================

text.ts - the UI's bitmap font: measurement and glyph layout

Owns font admission and two bounded memos, run widths and glyph layouts,
so labels that repeat frame to frame are not laid out again.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import { decodeUiFont, titleGlyphs, titleTextBox, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
import type { FontAtlas, GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { guideContent, type GuideToken } from "@/engine/foundation/ui/guide-content";

// Bitmap metrics own measurement and drawing. No browser font or second
// raster atlas can diverge from selection/caret geometry.
export function createUiText( assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">, base: string ) {
	// Owned by this font admission; finite FIFO storage survives idle HUD frames.
	const widths = new Map<string, number>(), layouts = new Map<string, readonly UiQuad[]>();
	let state: { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; font: FontAtlas; } | {
		kind: "failed";
		message: string;
	} | { kind: "disposed"; } = { kind: "idle" };
	return {
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
					id: assets.request( new URL( "/assets/fonts/native-ui-font-atlas.json", base ).href, 4 << 20 )
				};
			}
			return false;
		},
		height: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight : 0,
		boardHeight: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight + 5 : 0,
		extentHeight: ( index = 0, style = 0 ) => {
			if ( state.kind !== "ready" ) return 0;
			const font = state.font.fonts[String( index ) + (style === 2 ? ":2" : "")]!;
			return font.ascent + font.descent;
		},
		path: () => state.kind === "ready" ? state.font.image : null,
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
		error: () => state.kind === "failed" ? state.message : null,
		run( value: string, fontStyle = 0, fontIndex = 0 ) {
			if ( state.kind !== "ready" ) return { width: 0 };
			const text = value, key = fontIndex + ":" + fontStyle + ":" + text, cached = widths.get( key );
			if ( cached !== undefined ) return { width: cached };
			const font = state.font.fonts[String( fontIndex ) + (fontStyle === 2 ? ":2" : "")]!;
			let width = 0;
			for ( const c of text ) width += drawableGlyph( font.glyphs, c )?.advanceX ?? 0;
			if ( widths.size >= 2048 ) widths.delete( widths.keys().next().value! );
			widths.set( key, width );
			return { width };
		},
		// Labels repeat frame to frame; glyph layout walks every character. The
		// memo keys every input and hands out a copy of frozen quads, so no caller
		// can change another's result.
		quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			if ( state.kind !== "ready" ) return [];
			const key = [
					value,
					rect,
					clip,
					color,
					style.fontIndex,
					style.fontStyle,
					style.hAlign,
					style.vAlign,
					style.overflow
				].join( "|" ),
				cached = layouts.get( key );
			if ( cached ) {
				layouts.delete( key );
				layouts.set( key, cached );
				return cached.slice();
			}
			const quads = titleGlyphs( state.font, value, rect, clip, color, style ).map( quad =>
				Object.freeze( quad )
			);
			if ( layouts.size >= 4096 ) layouts.delete( layouts.keys().next().value! );
			layouts.set( key, quads );
			return quads.slice();
		},
		box( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			return state.kind === "ready" ? titleTextBox( state.font, value, rect, clip, color, style ) : [];
		},
		dispose() {
			widths.clear();
			layouts.clear();
			if ( state.kind === "loading" ) assets.cancel( state.id );
			state = { kind: "disposed" };
		}
	};
}
