/*
===========================================================================

text-lines.ts - line breaking for the HUD's text controls

The client fits text through 782AC0, whose second argument picks the wrap:
0 rolls an overflow back to the last ASCII space (word wrap), anything else
breaks at the glyph that did not fit. v1.150 always passes
g_GlobalEnvironmentConfig+0x12C, which CEnvironmentConfig's constructor sets
to 1 (4BCA61) and nothing changes, so native text wraps per character in
every language. Mid-word breaks read as broken in English: the owner approved
word wrap as a deliberate deviation (2026-10-13), with no flag. textLines and
textBoxLines carry it; textBoardLines keeps the native per-glyph break.

===========================================================================
*/
import { NARROW_FONT_INDEX } from "@/engine/foundation/rendering/ui-glyphs";
/*
================
textLines

Port-only, not native (owner-approved 2026-10-13): word wrap for chat lines
(CIFChatLine_LayoutText 5394A0 reads the same 4BCA61 flag at 53960C and
natively wraps per character), tooltips and prose. Measure with the same
bitmap glyph advances used for rendering. Long tokens still break at a glyph
(chat links and names, translated tips need not contain spaces).

The wrap walks glyph advances, never string prefixes (#3176): the renderer's
measure is a sum of per-glyph advances, so a line's width accumulates as
its glyphs land and each glyph is measured once. Re-measuring the growing
line per character re-walked every prefix - and thrashed the whole-string
width memo - which made one chat layout rebuild cost milliseconds.
================
*/
export function textLines(
	value: string,
	width: number,
	measure: ( value: string ) => number,
	preserveLeadingSpaces = false
): string[] {
	const lines: string[] = [];
	for ( const paragraph of value.split( "\n" ) ) {
		const leading = preserveLeadingSpaces ? /^[ \t]*/.exec( paragraph )![0] : "";
		let line = leading, advance = 0;
		for ( const char of leading ) advance += measure( char );
		for ( const token of paragraph.slice( leading.length ).split( /(\s+)/ ) ) {
			if ( !token ) continue;
			const glyphs = [ ...token ].map( char => ({ char, advance: measure( char ) }) );
			let tokenAdvance = 0;
			for ( const glyph of glyphs ) tokenAdvance += glyph.advance;
			if ( line.trim() && advance + tokenAdvance > width ) {
				lines.push( line.trimEnd() );
				line = "";
				advance = 0;
			}
			if ( !line && !token.trim() ) continue;
			for ( const glyph of glyphs ) {
				if ( line && advance + glyph.advance > width ) {
					lines.push( line );
					line = "";
					advance = 0;
				}
				line += glyph.char;
				advance += glyph.advance;
			}
		}
		lines.push( line.trimEnd() );
	}
	return lines;
}

// A fitted button caption keeps this far inside each side of its plate.
export const CAPTION_MARGIN = 4;

/*
================
narrowFace

Port-only, not native (Experimental "Fit window labels"): the face a
one-line text wider than width in face 0 is drawn in, so a column head
or a button caption on a fixed plate ("Experience" on the job ranking's
56-pixel head, "Dismiss" on the academy's 44-pixel button) reads whole
instead of running into its neighbour or past its plate. Face 0 falls
back to NARROW_FONT_INDEX (ui-glyphs.ts) when that is narrower; any
other face, or a text that fits, keeps its face. For a caller that clips
a caption to its plate, where the overlap resolver never sees it run
over (the academy matching's heads).
================
*/
export function narrowFace(
	value: string,
	width: number,
	fontIndex: number,
	measure: ( value: string, fontIndex: number ) => number
): number {
	if ( fontIndex !== 0 ) return fontIndex;
	const wide = measure( value, fontIndex );
	if ( wide <= width ) return fontIndex;
	return measure( value, NARROW_FONT_INDEX ) < wide ? NARROW_FONT_INDEX : fontIndex;
}

/*
================
fitLines

Port-only, not native (Experimental "Fit window labels"): a label's
lines and face inside width, the first that fits of: one line in its
face, two in its face, one in NARROW_FONT_INDEX, two in it (the academy
matching's "Recruitment Condition" in the 62 pixels between its combo
boxes reads "Recruitment" over "Condition" in the small face). Null
when none fits.
================
*/
export function fitLines(
	value: string,
	width: number,
	fontIndex: number,
	measure: ( value: string, fontIndex: number ) => number
): { lines: string[]; fontIndex: number; } | null {
	const faces = fontIndex === 0 ? [ 0, NARROW_FONT_INDEX ] : [ fontIndex ];
	for ( const face of faces ) {
		const measureFace = ( line: string ) => measure( line, face );
		if ( measureFace( value ) <= width ) return { lines: [ value ], fontIndex: face };
		// textLines breaks a word wider than the line at a glyph; a label
		// never breaks inside a word.
		if ( value.split( " " ).some( word => measureFace( word ) > width ) ) continue;
		const lines = textLines( value, width, measureFace );
		if ( lines.length === 2 && lines.every( line => measureFace( line ) <= width ) ) {
			return { lines, fontIndex: face };
		}
	}
	return null;
}

// Words a cut caption never ends on: they only lead into what was cut.
// Space-delimited, so a lookup is " " + word + " ".
const CAPTION_TRAILING_WORDS = " a an the for of to in on with and or ";

/*
================
captionWords

Port-only, not native (Experimental "Fit window labels"): the leading
words of a one-line caption that fit width, without a dangling function
word at the cut ("Check for duplicate entries" reads "Check", not "Check
for"). The whole caption comes back when it fits, or when not even its
first word does.
================
*/
export function captionWords( value: string, width: number, measure: ( value: string ) => number ): string {
	if ( measure( value ) <= width ) return value;
	const words = value.split( " " );
	for ( let count = words.length - 1; count > 0; count-- ) {
		if ( measure( words.slice( 0, count ).join( " " ) ) > width ) continue;
		let keep = count;
		while ( keep > 1 && CAPTION_TRAILING_WORDS.includes( " " + words[keep - 1]!.toLowerCase() + " " ) ) keep--;
		return words.slice( 0, keep ).join( " " );
	}
	return value;
}

/*
================
textBoxParagraphs

CIFTextBox 5395A0..5395DD: raw LF and backslash+'n' end a logical segment;
backslash+raw LF is a swallowed source continuation. Do not normalize these
escapes in the localization owner: other controls have different grammars
and must retain their original authored text.
================
*/
export function textBoxParagraphs( value: string ): string[] {
	const paragraphs: string[] = [];
	let paragraph = "";
	for ( let i = 0; i < value.length; i++ ) {
		const char = value[i]!;
		if ( char === "\\" && value[i + 1] === "\n" ) {
			i++;
			continue;
		}
		if ( char === "\n" || char === "\\" && value[i + 1] === "n" ) {
			paragraphs.push( paragraph );
			paragraph = "";
			if ( char === "\\" ) i++;
		} else paragraph += char;
	}
	paragraphs.push( paragraph );
	return paragraphs;
}

/*
================
textBoxLines

CIFTextBox 5394A0 fits each row through 782AC0. Port-only, not native
(owner-approved 2026-10-13): an overflow rolls back to the last space (word
wrap); native v1.150 passes 782AC0 the 4BCA61 flag of 1 and breaks at the
glyph that did not fit. Wrapping preserves whitespace, and continuation
spaces are inserted after splitting, as native does.
================
*/
export function textBoxLines(
	value: string,
	width: number,
	measure: ( value: string ) => number,
	indent = false
): string[] {
	const rows: string[] = [],
		space = measure( " " ),
		prefix = indent ? " ".repeat( Math.max( 1, space > 0 ? Math.trunc( 20 / space ) : 1 ) ) : "";
	for ( const paragraph of textBoxParagraphs( value ) ) {
		const glyphs = Array.from( paragraph );
		let start = 0, continuation = false;
		do {
			let end = start, advance = 0, breakAt = -1;
			while ( end < glyphs.length ) {
				const next = measure( glyphs[end]! );
				if ( end > start && advance + next > width ) break;
				advance += next;
				if ( glyphs[end] === " " ) breakAt = end + 1;
				end++;
			}
			if ( end < glyphs.length && breakAt > start ) end = breakAt;
			rows.push( (continuation ? prefix : "") + glyphs.slice( start, end ).join( "" ) );
			start = end;
			continuation = true;
		} while ( start < glyphs.length );
	}
	return rows;
}

/*
================
textBoardLines

7824F0 / 7832C0: text boards wrap at glyph boundaries, preserving spaces,
which is the native per-glyph break. This differs from the word wrap the
port keeps for tooltips and guide prose (textLines).
================
*/
export function textBoardLines( value: string, width: number, measure: ( value: string ) => number ): string[] {
	const lines: string[] = [];
	let line = "", advance = 0;
	for ( const glyph of value ) {
		if ( glyph === "\n" ) {
			lines.push( line );
			line = "";
			advance = 0;
			continue;
		}
		const next = measure( glyph );
		if ( advance + next > width ) {
			lines.push( line );
			line = "";
			advance = 0;
		}
		line += glyph;
		advance += next;
	}
	lines.push( line );
	return lines;
}
