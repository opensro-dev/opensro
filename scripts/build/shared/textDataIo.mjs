import { readFileSync, readdirSync } from "node:fs";
import { readdir } from "node:fs/promises";

/**
 * Iterate decoded textdata framing without imposing a cell policy. Whole-line
 * comments and blank rows are optional; callers choose whether yielded rows
 * retain authored whitespace for position-sensitive tab formats.
 */
export function* iterateTextDataLines(
	rawText,
	{ trim = false, skipBlank = true, skipComments = true } = {}
) {
	for ( const rawLine of String( rawText ).split( /\r?\n/ ) ) {
		const framed = rawLine.trim();
		if ( skipBlank && !framed ) continue;
		if ( skipComments && framed.startsWith( "//" ) ) continue;
		yield trim ? framed : rawLine;
	}
}

export function stripFullLineComment( rawLine ) {
	return rawLine.trim().startsWith( "//" ) ? "" : rawLine;
}

/**
 * Read the native UTF-16LE textdata framing used by the synchronous data
 * builders. Content columns remain byte-for-byte equivalent after decoding:
 * only the BOM, blank rows, and whole-line comments are removed.
 */
export function readTextDataLinesSync( sourcePath ) {
	const text = readFileSync( sourcePath, "utf16le" ).replace( /^\ufeff/, "" );
	return [ ...iterateTextDataLines( text ) ];
}

/** Split a column-indexed textdata row without trimming or collapsing empty cells. */
export function splitTextDataRow( line ) {
	return line.split( "\t" );
}

/** Read framed textdata lines and split each into position-stable raw cells. */
export function readTextDataRowsSync( sourcePath ) {
	return readTextDataLinesSync( sourcePath ).map( splitTextDataRow );
}

/** 797240 localized-text framing: CR ends records; bare LF belongs to a
 * language cell. Splitting on every LF truncates multiline quest articles. */
export function readLocalizedTextDataRowsSync( sourcePath ) {
	const text = readFileSync( sourcePath, "utf16le" ).replace( /^\ufeff/, "" );
	return text.split( /\r\n?/ ).filter( line => line.trim() && !line.trimStart().startsWith( "//" ) ).map(
		splitTextDataRow
	);
}

/** Preserve the directory's native order; several table folds are last-row-wins. */
export function listTextDataShardNamesSync( textDataDirectory, fileNamePattern ) {
	return readdirSync( textDataDirectory ).filter( ( fileName ) => matches( fileNamePattern, fileName ) );
}

/** Async sibling for resource builders that use the BOM/codepage-aware readText helper. */
export async function listTextDataShardNames( textDataDirectory, fileNamePattern ) {
	return (await readdir( textDataDirectory )).filter( ( fileName ) => matches( fileNamePattern, fileName ) );
}

function matches( pattern, value ) {
	pattern.lastIndex = 0;
	return pattern.test( value );
}
