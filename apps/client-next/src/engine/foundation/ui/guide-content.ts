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

// CIFPML_RebuildLayout (535B8C) seeds the first line state with margin 5.
const PML_LINE_MARGIN = 5;
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
	const margins = [ PML_LINE_MARGIN ];
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
GuideBlock

A CIFPML control's authored frame for its text block: the res HAlign that
seeds the first line's alignment and the res VAlign that shifts the whole
block inside its rect. CIFPML_RebuildLayout (535B84) maps HAlign 0/1/2 to
the line alignments left/center/right, and its tail (535C21..535C85)
shifts the laid-out layer down by the client height minus the content
height - halved for VAlign 1, full for VAlign 2, never when the content
is taller.
================
*/
export interface GuideBlock {
	align: GuideAlign;
	vAlign: number;
}

/*
================
styledHeight

The styled height of a font face: CTextBlock_GetStyledLine (781C40)
returns the TEXTMETRIC tmHeight that CGfxFont_InitializeGdiStyleSlots
(A17690) stores per style slot with GetTextMetricsA. The atlas records
that same metric as ascent + descent for every face and style.
================
*/
function styledHeight( font: { ascent: number; descent: number; } ): number {
	return font.ascent + font.descent;
}

/*
================
shiftQuad

A laid-out quad moved by (dx, dy); a text run's layout box moves with its
glyphs so overlap fitting keeps seeing the placed cell.
================
*/
function shiftQuad( q: UiQuad, dx: number, dy: number ): UiQuad {
	if ( !dx && !dy ) return q;
	const box = q.textLayout?.box;
	const moved: UiQuad = {
		...q,
		rect: [ q.rect[0] + dx, q.rect[1] + dy, q.rect[2], q.rect[3] ],
		...(box ?
			{ textLayout: { ...q.textLayout, box: [ box[0] + dx, box[1] + dy, box[2], box[3] ] as UiRect } } :
			{})
	};
	return moved;
}

/*
================
guideContent

CPMLLayer_LayoutContent (840840) lays a PML document out as CTextBlock
lines (83E8F0): each line carries its own height, margin and alignment,
a finished line advances the cursor by its height plus its margin
(83E6B0), and every run or image sits on the line's bottom edge
(83D000). A line's height is the tallest styled height or image on it.
Keep run boundaries and spaces: fitPmlText applies the owner-approved
word-wrap deviation.
================
*/
export function guideContent(
	atlas: FontAtlas,
	tokens: readonly GuideToken[],
	r: UiRect,
	clip: UiRect,
	color: UiQuad["color"],
	size: ( path: string ) => readonly [number, number] | undefined,
	block?: GuideBlock
) {
	const quads: UiQuad[] = [], paths: string[] = [];
	// The line being filled: its origin, the margin and alignment it will
	// advance by, its height so far, and its quads awaiting the bottom edge.
	let x = r[0], y = r[1], margin = PML_LINE_MARGIN, align: GuideAlign = block?.align ?? "left";
	let lineHeight = 0, height = 0;
	let pending: { index: number; cell: number; }[] = [];
	/*
	================
	finishLine

	Aligns the finished line within r, drops its quads onto the line's
	bottom edge, and advances by the line's height plus its margin.
	================
	*/
	const finishLine = () => {
		const spare = r[2] - (x - r[0]),
			dx = spare > 0 && align === "center" ? Math.floor( spare / 2 ) : spare > 0 && align === "right" ? spare : 0,
			bottom = y + lineHeight;
		for ( const item of pending ) {
			const q = quads[item.index]!;
			quads[item.index] = shiftQuad(
				q,
				dx,
				bottom - item.cell - (q.textLayout ? q.textLayout.box[1] : q.rect[1])
			);
		}
		pending = [];
		y = bottom + margin;
		height = y - r[1];
		x = r[0];
		lineHeight = 0;
	};
	for ( const token of tokens ) {
		if ( token.kind === "align" ) {
			// An alignment scope starts on its own line.
			if ( x > r[0] ) finishLine();
			align = token.align;
			continue;
		}
		if ( token.kind === "paragraph" ) {
			if ( x > r[0] ) finishLine();
			margin = token.margin;
			continue;
		}
		if ( token.kind === "break" ) {
			finishLine();
			continue;
		}
		if ( token.kind === "image" ) {
			paths.push( token.path );
			const extent = size( token.path );
			if ( !extent ) continue;
			const [w, h] = extent;
			if ( x > r[0] && x + w > r[0] + r[2] ) finishLine();
			pending.push( { index: quads.length, cell: h } );
			quads.push( {
				rect: [ x, y, w, h ],
				clip,
				uv: [ 0, 0, 1, 1 ],
				texture: token.path,
				color: [ 1, 1, 1, 1 ]
			} );
			lineHeight = Math.max( lineHeight, h );
			x += w;
			continue;
		}
		const font = atlas.fonts[token.strong ? "0:2" : "0"];
		if ( !font ) throw Error( "Missing native emphasis font" );
		const cell = styledHeight( font ), chars = Array.from( token.value );
		let start = 0;
		while ( start < chars.length ) {
			const fit = fitPmlText( font.glyphs, chars, start, r[0] + r[2] - x );
			const placed = titleText(
				atlas,
				chars.slice( start, fit.end ).join( "" ),
				[ x, y, fit.width, cell ],
				clip,
				token.color ?? color,
				{
					fontStyle: token.strong ? 2 : 0,
					vAlign: 0
				}
			);
			if ( placed.length ) pending.push( { index: quads.length, cell } );
			quads.push( ...placed );
			lineHeight = Math.max( lineHeight, cell );
			x += fit.width;
			start = fit.end;
			if ( start < chars.length ) finishLine();
		}
	}
	finishLine();
	// CIFPML_RebuildLayout shifts the whole block for the authored VAlign.
	const shiftY = block?.vAlign === 1 ?
		Math.floor( Math.max( 0, r[3] - height ) / 2 ) :
		block?.vAlign === 2 ?
		Math.max( 0, r[3] - height ) :
		0;
	if ( shiftY ) {
		for ( let i = 0; i < quads.length; i++ ) quads[i] = shiftQuad( quads[i]!, 0, shiftY );
	}
	return { quads, paths, height };
}
