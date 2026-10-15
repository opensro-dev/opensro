/*
===========================================================================

guide-content.ts - shared native PML parsing and inline layout

Guides, quests, mall descriptions and tooltips consume the same safe token
stream. Markup never enters the DOM; only admitted native attributes reach
layout, and image paths remain within the published interface assets.

===========================================================================
*/
import type { FontAtlas } from "@/engine/foundation/rendering/ui-glyphs";
import { titleText, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
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
	| { kind: "paragraph"; margin: number; }
	| { kind: "align"; align: GuideAlign; };

// A line's horizontal alignment (the text board's <left>, <center> and
// <right> scopes).
export type GuideAlign = "left" | "center" | "right";
// Text_HasSml2Markup (843100): longer than the 13-character wrapper and
// opening "<sml2>" (case-sensitive). Its "</sml2>" search is a no-op natively
// (neg/sbb/neg on the find position is true for npos too), so it is not tested.
const SML2_MIN_LENGTH = 13;

/*
================
hasSml2Markup

Whether a string is native SML2 markup, as 843100 decides before a text
board or message box renders it as markup rather than plain lines.
================
*/
export function hasSml2Markup( value: string ): boolean {
	return value.length > SML2_MIN_LENGTH && value.startsWith( "<sml2>" );
}

/*
================
markupArg

A value filled into SML2 text (a name or a typed alias) with its angle
brackets removed, so typed text can never open or close a markup scope.
Natively 6461C0 refuses such a name before the confirm exists.
================
*/
export function markupArg( value: string ): string {
	return value.replace( /[<>]/g, "" );
}

/*
================
fillMarkup

SML2 text with each %s replaced, in order, by its argument with angle
brackets removed (markupArg). A replacer function, so "$" patterns in a
player's text are literal and never re-insert the template.
================
*/
export function fillMarkup( template: string, args: readonly string[] ): string {
	return args.reduce( ( value, arg ) => value.replace( "%s", () => markupArg( arg ) ), template );
}

/*
================
groupedNumber

A count with comma thousands separators, as the native std_wstring_format
digit grouping (53E2A0) writes gold.
================
*/
export function groupedNumber( value: number ): string {
	return String( Math.trunc( value ) ).replace( /\B(?=(\d{3})+(?!\d))/g, "," );
}

// The shipped event articles use SML2 text, strong, font color, BR and images;
// message boxes add the <left>, <center> and <right> line scopes.
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
	const aligns: GuideAlign[] = [ "left" ];
	for ( const token of source.split( /(<[^>]*>)/ ).filter( Boolean ) ) {
		if ( /^<sml2>$/i.test( token ) ) continue;
		if ( /^<\/sml2>$/i.test( token ) ) {
			// The document's end closes any font scope still open: the shipped
			// UIIT_STT_ALIAS_MODIFY_WINDOW leaves its gold line's <font> open,
			// and the native text board (52C340) still renders it. Other scopes
			// must balance.
			while ( colors.length > 1 ) {
				colors.pop();
				if ( fontBold.pop() ) strong--;
			}
			continue;
		}
		const align = /^<(\/?)(left|center|right)>$/i.exec( token );
		if ( align ) {
			const name = align[2]!.toLowerCase() as GuideAlign;
			if ( align[1] ) {
				if ( aligns.length === 1 || aligns.at( -1 ) !== name ) throw Error( "Unbalanced guide alignment" );
				aligns.pop();
			} else aligns.push( name );
			out.push( { kind: "align", align: aligns.at( -1 )! } );
			continue;
		}
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
	if ( strong || colors.length !== 1 || margins.length !== 1 || aligns.length !== 1 ) {
		throw Error( "Unbalanced guide markup" );
	}
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
	// The current alignment, and the first quad of the line being laid out.
	let align: GuideAlign = "left", lineStart = 0;
	/*
	================
	finishLine

	Shifts the finished line's quads to its alignment within r.
	================
	*/
	const finishLine = () => {
		const spare = r[2] - (x - r[0]);
		if ( align !== "left" && spare > 0 ) {
			const dx = align === "center" ? Math.floor( spare / 2 ) : spare;
			for ( let i = lineStart; i < quads.length; i++ ) {
				const q = quads[i]!;
				quads[i] = { ...q, rect: [ q.rect[0] + dx, q.rect[1], q.rect[2], q.rect[3] ] };
			}
		}
		lineStart = quads.length;
	};
	/*
	================
	newline
	================
	*/
	const newline = () => {
		finishLine();
		x = r[0];
		y += line;
		line = atlas.fonts["0"]!.recordHeight + margin;
	};
	for ( const token of tokens ) {
		if ( token.kind === "align" ) {
			// An alignment scope starts on its own line.
			if ( x > r[0] ) newline();
			align = token.align;
			continue;
		}
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
				...titleText( atlas, word, [ x, y, width, font.recordHeight + 5 ], clip, token.color ?? color, {
					fontStyle: token.strong ? 2 : 0,
					vAlign: 0
				} )
			);
			x += width;
		}
	}
	finishLine();
	return { quads, paths, height: y - r[1] + line };
}
