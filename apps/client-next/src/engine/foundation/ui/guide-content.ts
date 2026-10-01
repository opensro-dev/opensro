/*
===========================================================================

guide-content.ts - shared native PML parsing and inline layout

Guides, quests, mall descriptions and tooltips consume the same safe token
stream. Markup never enters the DOM; only admitted native attributes reach
layout, and image paths remain within the published interface assets.

===========================================================================
*/
import type { FontAtlas } from "@/engine/foundation/rendering/ui-glyphs";
import { titleGlyphs, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
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
	const margins = [ 5 ];
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
guideContent
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
	let x = r[0], y = r[1], margin = 5, line = atlas.fonts["0"]!.recordHeight + margin;
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
		for ( const word of token.value.split( /(\s+)/ ).filter( Boolean ) ) {
			const width = Array.from( word ).reduce(
				( n, c ) => n + (drawableGlyph( font.glyphs, c )?.advanceX ?? 0),
				0
			);
			if ( x > r[0] && x + width > r[0] + r[2] ) newline();
			if ( x === r[0] && !word.trim() ) continue;
			quads.push(
				...titleGlyphs( atlas, word, [ x, y, width, font.recordHeight + 5 ], clip, token.color ?? color, {
					fontStyle: token.strong ? 2 : 0,
					vAlign: 0
				} )
			);
			x += width;
		}
	}
	return { quads, paths, height: y - r[1] + line };
}
