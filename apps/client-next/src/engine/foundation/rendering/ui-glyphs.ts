/*
===========================================================================

ui-glyphs.ts - the retail bitmap font: decode, measure and lay out

Decodes the published font atlas, lays out single-line and boxed text
(native statics and text boards), and resolves where a label's ink would
enter a neighbouring text box. Text runs are tested glyph by glyph.

===========================================================================
*/

import type { UiQuad, UiRect, UiTextLayout } from "@/engine/contracts/ui";
import { textRunGlyphRect, textRunQuad } from "@/engine/foundation/rendering/text-run";
export interface Glyph {
	x: number;
	y: number;
	width: number;
	height: number;
	originX: number;
	originY: number;
	advanceX: number;
}
export interface FontAtlas {
	image: string;
	atlasWidth: number;
	atlasHeight: number;
	fonts: Record<string, { recordHeight: number; ascent: number; descent: number; glyphs: Record<string, Glyph>; }>;
}
export interface GlyphStyle {
	fontIndex?: number;
	fontStyle?: number;
	hAlign?: number;
	vAlign?: number;
	overflow?: "ellipsis" | "clip" | "avoid-overlap";
}
/*
================
drawableGlyph

C0/C1 controls are layout directives, never drawable glyphs. Native
CTextBoard breaks or skips them; baking the '?' replacement quad for a
newline (as shipped guide START strings contain) invents visible glyphs.
================
*/
export function drawableGlyph( glyphs: Record<string, Glyph>, c: string ): Glyph | null {
	const code = c.codePointAt( 0 )!;
	if ( code < 32 || (code >= 0x7F && code <= 0x9F) ) return null;
	return glyphs[String( code )] ?? glyphs["63"]!;
}
/*
================
decodeUiFont
================
*/
export function decodeUiFont( value: unknown ): FontAtlas {
	const record = ( v: unknown ): v is Record<string, unknown> =>
		typeof v === "object" && v !== null && !Array.isArray( v );
	const integer = ( v: unknown ) => {
		if ( typeof v !== "number" || !Number.isSafeInteger( v ) ) throw Error( "Invalid retail font metric" );
		return v;
	};
	if (
		!record( value ) || typeof value.image !== "string" || !value.image.startsWith( "/assets/fonts/" ) ||
		!record( value.fonts )
	) throw Error( "Invalid retail font atlas" );
	const atlasWidth = integer( value.atlasWidth ),
		atlasHeight = integer( value.atlasHeight ),
		fonts: FontAtlas["fonts"] = {};
	if ( atlasWidth < 1 || atlasHeight < 1 || atlasWidth > 8192 || atlasHeight > 8192 ) {
		throw Error( "Invalid retail font atlas extent" );
	}
	const variants = Object.entries( value.fonts ).flatMap( ( [index, font] ) =>
		[
			[ index, font ],
			...(record( font ) && record( font.styles ) && font.styles["2"] ?
				[ [ index + ":2", font.styles["2"] ] ] :
				[])
		] as [string, unknown][]
	);
	for ( const [index, font] of variants ) {
		if ( !record( font ) || !record( font.glyphs ) ) throw Error( "Invalid retail font" );
		const recordHeight = integer( font.recordHeight ),
			ascent = integer( font.ascent ),
			descent = integer( font.descent ),
			glyphs: Record<string, Glyph> = {};
		if ( recordHeight < 1 || recordHeight > 256 ) throw Error( "Invalid retail font height" );
		for ( const [code, g] of Object.entries( font.glyphs ) ) {
			if ( !record( g ) ) throw Error( "Invalid retail glyph" );
			const glyph = {
				x: integer( g.x ),
				y: integer( g.y ),
				width: integer( g.width ),
				height: integer( g.height ),
				originX: integer( g.originX ),
				originY: integer( g.originY ),
				advanceX: integer( g.advanceX )
			};
			if (
				glyph.x < 0 || glyph.y < 0 || glyph.width < 0 || glyph.height < 0 || glyph.advanceX < 0 ||
				glyph.advanceX > 512 || glyph.x + glyph.width > atlasWidth || glyph.y + glyph.height > atlasHeight
			) throw Error( "Retail glyph outside atlas" );
			glyphs[code] = glyph;
		}
		if ( !glyphs["63"] ) throw Error( "Missing retail replacement glyph" );
		fonts[index] = { recordHeight, ascent, descent, glyphs };
	}
	if ( !fonts["0"] ) throw Error( "Missing retail default font" );
	return { image: value.image, atlasWidth, atlasHeight, fonts };
}
/*
================
titleColoredText

The title countdown uses the native SML2 font-color subset. Keep authored
glyph metrics and color runs; never hand its markup to browser text shaping.
================
*/
export function titleColoredText(
	atlas: FontAtlas,
	value: string,
	rect: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	style: GlyphStyle = {}
): UiQuad[] {
	const font = atlas.fonts[String( style.fontIndex ?? 0 )];
	if ( !font ) throw Error( "Missing retail UI font" );
	const colors: UiQuad["color"][] = [ color ], quads: UiQuad[] = [];
	let x = rect[0], y = rect[1];
	for ( const token of value.split( /(<[^>]+>|\s+)/ ).filter( Boolean ) ) {
		if ( token === "<sml2>" || token === "</sml2>" ) continue;
		if ( token === "</font>" ) {
			if ( colors.length < 2 ) throw Error( "Unbalanced title color" );
			colors.pop();
			continue;
		}
		const tag = /^<font color="(\d+),(\d+),(\d+),(\d+)">$/.exec( token );
		if ( tag ) {
			const [a, r, g, b] = tag.slice( 1 ).map( Number );
			if ( [ a, r, g, b ].some( n => n! > 255 ) ) throw Error( "Invalid title color" );
			colors.push( [ r! / 255, g! / 255, b! / 255, a! / 255 * color[3] ] );
			continue;
		}
		if ( token.startsWith( "<" ) ) throw Error( "Unsupported title markup" );
		const width = Array.from( token ).reduce(
			( sum, c ) => sum + (drawableGlyph( font.glyphs, c )?.advanceX ?? 0),
			0
		);
		if ( x > rect[0] && x + width > rect[0] + rect[2] ) {
			x = rect[0];
			y += font.recordHeight + 5;
		}
		if ( x === rect[0] && !token.trim() ) continue;
		quads.push(
			...titleText( atlas, token, [ x, y, width, font.recordHeight + 5 ], clip, colors.at( -1 )!, {
				...style,
				hAlign: 0,
				vAlign: 0
			} )
		);
		x += width;
	}
	if ( colors.length !== 1 ) throw Error( "Unbalanced title color" );
	return quads;
}
/*
================
titleTextBox
================
*/
export function titleTextBox(
	atlas: FontAtlas,
	value: string,
	rect: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	style: GlyphStyle = {}
): UiQuad[] {
	const font = atlas.fonts[String( style.fontIndex ?? 0 )];
	if ( !font ) throw Error( "Missing retail UI font" );
	const lines: string[] = [];
	let line = "", width = 0;
	for ( const word of value.replace( /\r\n?/g, "\n" ).split( /(\n|[^\S\n]+)/ ) ) {
		if ( word === "\n" ) {
			lines.push( line.trimEnd() );
			line = "";
			width = 0;
			continue;
		}
		const advance = Array.from( word ).reduce(
			( sum, c ) => sum + (drawableGlyph( font.glyphs, c )?.advanceX ?? 0),
			0
		);
		if ( width + advance > rect[2] && line ) {
			lines.push( line.trimEnd() );
			line = "";
			width = 0;
		}
		if ( !line && !word.trim() ) continue;
		line += word;
		width += advance;
	}
	if ( line ) lines.push( line.trimEnd() );
	const lineHeight = font.recordHeight + 5;
	return lines.flatMap( ( text, index ) =>
		titleText( atlas, text, [ rect[0], rect[1] + index * lineHeight, rect[2], lineHeight ], clip, color, {
			...style,
			vAlign: 0
		} )
	);
}
/*
================
titleText

One line of retail text as a text run quad (text-run.ts), or nothing when
no glyph is drawable. Every public producer in this module publishes runs;
the per-glyph layout below is private to it.
================
*/
export function titleText(
	atlas: FontAtlas,
	value: string,
	rect: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	style: GlyphStyle = {}
): UiQuad[] {
	const laid = layoutGlyphs( atlas, value, rect, clip, color, style ), run = textRunQuad( laid.quads );
	if ( !run ) return [];
	const overflow = style.overflow ?? "avoid-overlap";
	if ( overflow === "ellipsis" ) return [ run ];
	// Text wider than its box carries the shortened run that overlap fitting
	// (resolveTextOverlaps) swaps in when its ink would enter a neighbour.
	const fitted = overflow === "avoid-overlap" && laid.width > Math.max( 0, rect[2] ) ?
		textRunQuad( layoutGlyphs( atlas, value, rect, clip, color, { ...style, overflow: "ellipsis" } ).quads ) :
		undefined;
	return [ Object.freeze( { ...run, textLayout: { box: rect, fitted } } ) ];
}

/*
================
layoutGlyphs

Retail CTextBoard: glyph advances, raw font height + 5, integer alignment.
Sample the published GGO_BITMAP masks directly; browser font shaping differs.
One quad per glyph, sharing one color, texture and clip, and the laid-out
advance width; titleText turns them into a run.
================
*/
function layoutGlyphs(
	atlas: FontAtlas,
	value: string,
	rect: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	{ fontIndex = 0, fontStyle = 0, hAlign = 0, vAlign = 1, overflow = "avoid-overlap" }: GlyphStyle = {}
): { quads: UiQuad[]; width: number; } {
	const font = atlas.fonts[String( fontIndex ) + (fontStyle === 2 ? ":2" : "")];
	if ( !font ) throw Error( "Missing retail UI font slot: " + fontIndex );
	let glyphs = Array.from( value, c => drawableGlyph( font.glyphs, c ) ).filter( ( g ): g is Glyph => g !== null );
	// Native 7831D0 uses a glyph-width prefix plus three period glyphs. Plain
	// CIFStatic does not invoke it; applying bounded single-line presentation here
	// is an intentional port overlap repair, shared by authored and manual boxes.
	// Reserve the suffix before fitting; keep the source/semantic text untouched.
	const available = Math.max( 0, rect[2] );
	if ( overflow === "ellipsis" && glyphs.reduce( ( sum, g ) => sum + g.advanceX, 0 ) > available ) {
		const dot = drawableGlyph( font.glyphs, "." )!, suffix: Glyph[] = [];
		let used = 0;
		for ( let i = 0; i < 3 && used + dot.advanceX <= available; i++ ) {
			suffix.push( dot );
			used += dot.advanceX;
		}
		const prefix: Glyph[] = [];
		for ( const glyph of glyphs ) {
			if ( used + glyph.advanceX > available ) break;
			prefix.push( glyph );
			used += glyph.advanceX;
		}
		glyphs = prefix.concat( suffix );
	}
	// Horizontal containment includes glyph bearings; keep native vertical line
	// metrics (short statics can be shorter than the font's line box).
	const left = Math.max( clip[0], rect[0] ), right = Math.min( clip[0] + clip[2], rect[0] + available );
	if ( overflow !== "avoid-overlap" ) clip = [ left, clip[1], Math.max( 0, right - left ), clip[3] ];
	const width = glyphs.reduce( ( sum, g ) => sum + g.advanceX, 0 ), lineHeight = font.recordHeight + 5;
	const offset = ( size: number, content: number, align: number ) =>
		align === 1 ? Math.floor( (size - content) / 2 ) : align === 2 ? Math.floor( size - content ) : 0;
	let x = Math.round( rect[0] ) + offset( rect[2], width, hAlign );
	const y = Math.round( rect[1] ) + offset( rect[3], lineHeight, vAlign ) + font.ascent;
	const quads = glyphs.map( g => {
		const quad: UiQuad = {
			rect: [ x + g.originX, y - g.originY, g.width, g.height ],
			uv: [
				g.x / atlas.atlasWidth,
				g.y / atlas.atlasHeight,
				g.width / atlas.atlasWidth,
				g.height / atlas.atlasHeight
			],
			color,
			texture: atlas.image,
			clip
		};
		x += g.advanceX;
		return quad;
	} );
	return { quads, width };
}

/*
================
inkEnters

A glyph's visible ink (its rect within its clip) enters another text box.
================
*/
function inkEnters( rect: UiRect, clip: UiRect, other: UiRect ): boolean {
	return Math.max( rect[0], clip[0], other[0] ) <
			Math.min( rect[0] + rect[2], clip[0] + clip[2], other[0] + other[2] ) &&
		Math.max( rect[1], clip[1], other[1] ) < Math.min( rect[1] + rect[3], clip[1] + clip[3], other[1] + other[3] );
}

/*
================
runInkEnters

Whether any glyph of a run quad puts ink into another text box.
================
*/
function runInkEnters( quad: UiQuad, other: UiRect ): boolean {
	for ( const glyph of quad.run!.glyphs ) {
		if ( inkEnters( textRunGlyphRect( quad, glyph ), quad.clip, other ) ) return true;
	}
	return false;
}

/*
================
resolveTextOverlaps

Native statics may grow into unused space. A run wider than its layout box
is replaced by its shortened run only when its ink would enter another,
horizontally disjoint text box: overlapping boxes are intentional layers,
not neighbouring columns. Anchored labels neither fit nor block. Resolve
once per complete UI projection; layouts are dropped from the result.
================
*/
export function resolveTextOverlaps( quads: readonly UiQuad[] ): UiQuad[] {
	const boxes: UiQuad[] = [];
	for ( const q of quads ) {
		if ( !q.textLayout ) continue;
		if ( !q.run ) throw Error( "UI text layout without a text run" );
		if ( !q.characterAnchor && !q.worldAnchor ) boxes.push( q );
	}
	const fitted = new Set<UiTextLayout>();
	for ( const q of boxes ) {
		const layout = q.textLayout!;
		if ( layout.fitted === undefined || fitted.has( layout ) ) continue;
		const box = layout.box;
		for ( const neighbor of boxes ) {
			if ( neighbor.textLayout === layout ) continue;
			const other = neighbor.textLayout!.box;
			if ( !(box[0] + box[2] <= other[0] || other[0] + other[2] <= box[0]) ) continue;
			if ( runInkEnters( q, other ) ) {
				fitted.add( layout );
				break;
			}
		}
	}
	const emitted = new Set<UiTextLayout>(), result: UiQuad[] = [];
	for ( const q of quads ) {
		const { textLayout, ...paint } = q;
		if ( !textLayout || !fitted.has( textLayout ) ) {
			result.push( textLayout ? paint : q );
			continue;
		}
		if ( emitted.has( textLayout ) ) continue;
		emitted.add( textLayout );
		if ( textLayout.fitted ) result.push( { ...paint, ...textLayout.fitted, color: paint.color } );
	}
	return result;
}
