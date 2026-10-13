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
/*
================
textLines

Port-only, not native (owner-approved 2026-10-13): word wrap for chat lines
(CIFChatLine_LayoutText 5394A0 reads the same 4BCA61 flag at 53960C and
natively wraps per character), tooltips and prose. Measure with the same
bitmap glyph advances used for rendering. Long tokens still break at a glyph
(chat links and names, translated tips need not contain spaces).
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
		let line = leading;
		for ( const token of paragraph.slice( leading.length ).split( /(\s+)/ ) ) {
			if ( !token ) continue;
			if ( line.trim() && measure( line + token ) > width ) {
				lines.push( line.trimEnd() );
				line = "";
			}
			if ( !line && !token.trim() ) continue;
			for ( const char of token ) {
				if ( line && measure( line + char ) > width ) {
					lines.push( line );
					line = "";
				}
				line += char;
			}
		}
		lines.push( line.trimEnd() );
	}
	return lines;
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
