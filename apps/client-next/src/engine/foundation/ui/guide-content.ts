/*
===========================================================================

guide-content.ts - shared native PML parsing and inline layout

Guides, quests, mall descriptions and tooltips consume the same safe token
stream. Markup never enters the DOM; only admitted native attributes reach
layout, and image paths remain within the published interface assets.

===========================================================================
*/
import type { FontAtlas, Glyph } from "@/engine/foundation/rendering/ui-glyphs";
import { titleText, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

const PML_LINE_MARGIN = 5;
const BOARD_EXTRA_HEIGHT = 5;
/*
================
GuideToken
================
*/
export type GuideToken =
	| { kind: "text"; value: string; color: UiQuad["color"] | null; strong: boolean; }
	| { kind: "break"; }
	| { kind: "image"; path: string; }
	| { kind: "paragraph"; margin: number; };
// The shipped event articles use SML2 text, strong, font color, BR and images.
// Unsupported markup is rejected, never rendered as text or browser HTML.
/*
================
guideTokens

84023D..8403DE scopes paragraph line_margin independently of font attributes.
840473 handles combined font color and style; font line_margin is a no-op.
================
*/
export function guideTokens( source: string ): readonly GuideToken[] {
	const out: GuideToken[] = [], colors: (UiQuad["color"] | null)[] = [ null ];
	let strong = 0;
	const fontBold: boolean[] = [];
	const margins = [ PML_LINE_MARGIN ];
	for ( const token of source.split( /(<[^>]*>)/ ).filter( Boolean ) ) {
		if ( /^<\/?sml2>$/i.test( token ) ) continue;
		if ( /^<br\s*\/?\s*>$/i.test( token ) ) {
			out.push( { kind: "break" } );
			continue;
		}
		if ( token === "<strong>" ) {
			strong++;
			continue;
		}
		if ( token === "</strong>" ) {
			if ( --strong < 0 ) throw Error( "Unbalanced guide emphasis" );
			continue;
		}
		const paragraph = /^<p(?: line_margin="(\d+)")?>$/i.exec( token );
		if ( paragraph ) {
			const margin = paragraph[1] === undefined ? margins.at( -1 )! : Number( paragraph[1] );
			if ( !Number.isSafeInteger( margin ) || margin > 4096 ) throw Error( "Invalid paragraph margin" );
			margins.push( margin );
			out.push( { kind: "paragraph", margin } );
			continue;
		}
		if ( token.toLowerCase() === "</p>" ) {
			if ( margins.length === 1 ) throw Error( "Unbalanced guide paragraph" );
			margins.pop();
			out.push( { kind: "paragraph", margin: margins.at( -1 )! } );
			continue;
		}
		if ( token.toLowerCase() === "</font>" ) {
			if ( colors.length === 1 ) throw Error( "Unbalanced guide color" );
			colors.pop();
			if ( fontBold.pop() ) strong--;
			continue;
		}
		const font = /^<font((?: [a-z_]+="[^"<>]*")*)>$/i.exec( token );
		if ( font ) {
			let nextColor = colors.at( -1 )!, bold = false;
			const seen = new Set<string>();
			for ( const attr of font[1]!.matchAll( / ([a-z_]+)="([^"<>]*)"/gi ) ) {
				const name = attr[1]!.toLowerCase(), value = attr[2]!;
				if ( seen.has( name ) ) throw Error( "Repeated guide font attribute" );
				seen.add( name );
				if ( name === "color" ) {
					if ( !/^\d+,\d+,\d+,\d+$/.test( value ) ) throw Error( "Invalid guide color" );
					const [a, r, g, b] = value.split( "," ).map( Number );
					if ( [ a, r, g, b ].some( n => n! > 255 ) ) throw Error( "Invalid guide color" );
					nextColor = [ r! / 255, g! / 255, b! / 255, a! / 255 ];
				} else if ( name === "style" && value.toLowerCase() === "bold" ) bold = true;
				else if ( name !== "line_margin" || !/^\d+$/.test( value ) ) {
					throw Error( "Unsupported guide font attribute" );
				}
			}
			colors.push( nextColor );
			fontBold.push( bold );
			if ( bold ) strong++;
			continue;
		}
		const image = /^<img src="(interface\\[a-z0-9_\\.-]+\.ddj)"\s*>$/i.exec( token );
		if ( image ) {
			if ( image[1]!.includes( ".." ) ) throw Error( "Invalid guide image" );
			out.push( {
				kind: "image",
				path: "/assets/images/Media_extracted/" + image[1]!.replaceAll( "\\", "/" ).replace( /\.ddj$/i, ".png" )
			} );
			continue;
		}
		if ( token.startsWith( "<" ) ) throw Error( "Unsupported guide markup: " + token );
		out.push( {
			kind: "text",
			value: token.replace( /\r?\n/g, " " ),
			color: colors.at( -1 )!,
			strong: strong > 0
		} );
	}
	if ( strong || colors.length !== 1 || margins.length !== 1 ) throw Error( "Unbalanced guide markup" );
	return out;
}
/*
================
fitPmlText

782AC0 fits a CTextBoard to the remaining line width. The first glyph is
always admitted, even when wider than the remaining width (782B51..782B61).

Port-only, not native (owner-approved 2026-10-13): on overflow this rolls
back to the last admitted ASCII space, keeping that space on the old line,
and keeps the fitted prefix only when there is none. Native v1.150 wraps per
character: CEnvironmentConfig's constructor (4BCA61) sets
g_GlobalEnvironmentConfig+0x12C = 1, which 83E9BF passes to 782AC0, and that
skips the rollback (782B82, 782BDC).
================
*/
function fitPmlText( glyphs: Record<string, Glyph>, chars: readonly string[], start: number, available: number ) {
	let end = start, width = 0, spaceEnd = start, spaceWidth = 0;
	while ( end < chars.length ) {
		const c = chars[end]!, advance = drawableGlyph( glyphs, c )?.advanceX ?? 0;
		if ( width + advance > available && width !== 0 ) break;
		width += advance;
		end++;
		if ( c === " " ) {
			spaceEnd = end;
			spaceWidth = width;
		}
	}
	if ( end < chars.length && spaceEnd > start ) {
		end = spaceEnd;
		width = spaceWidth;
	}
	return { end, width };
}
/*
================
guideContent

83E8F0 lays out each PML text resource through the fitted CTextBoard and
continues its returned suffix on the next line. Keep run boundaries and
spaces: fitPmlText applies the owner-approved word-wrap deviation.
================
*/
export function guideContent(
	atlas: FontAtlas,
	tokens: readonly GuideToken[],
	r: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	size: ( path: string ) => readonly [number, number] | undefined
) {
	const quads: UiQuad[] = [], paths: string[] = [];
	let x = r[0], y = r[1], margin = PML_LINE_MARGIN, line = atlas.fonts["0"]!.recordHeight + margin;
	/*
	================
	newline
	================
	*/
	const newline = () => {
		x = r[0];
		y += line;
		line = atlas.fonts["0"]!.recordHeight + margin;
	};
	for ( const token of tokens ) {
		if ( token.kind === "paragraph" ) {
			if ( x > r[0] ) newline();
			margin = token.margin;
			line = atlas.fonts["0"]!.recordHeight + margin;
			continue;
		}
		if ( token.kind === "break" ) {
			newline();
			continue;
		}
		if ( token.kind === "image" ) {
			paths.push( token.path );
			const extent = size( token.path );
			if ( !extent ) continue;
			const [w, h] = extent;
			if ( x > r[0] && x + w > r[0] + r[2] ) newline();
			quads.push( {
				rect: [ x, y, w, h ],
				clip,
				uv: [ 0, 0, 1, 1 ],
				texture: token.path,
				color: [ 1, 1, 1, 1 ]
			} );
			x += w;
			line = Math.max( line, h + margin );
			continue;
		}
		const font = atlas.fonts[token.strong ? "0:2" : "0"];
		if ( !font ) throw Error( "Missing native emphasis font" );
		const chars = Array.from( token.value );
		let start = 0;
		while ( start < chars.length ) {
			const fit = fitPmlText( font.glyphs, chars, start, r[0] + r[2] - x );
			quads.push(
				...titleText(
					atlas,
					chars.slice( start, fit.end ).join( "" ),
					[ x, y, fit.width, font.recordHeight + BOARD_EXTRA_HEIGHT ],
					clip,
					token.color ?? color,
					{
						fontStyle: token.strong ? 2 : 0,
						vAlign: 0
					}
				)
			);
			x += fit.width;
			start = fit.end;
			if ( start < chars.length ) newline();
		}
	}
	return { quads, paths, height: y - r[1] + line };
}
