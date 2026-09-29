/*
===========================================================================

englishCompletions.mjs - product English for untranslated retail text cells

Joymax shipped some Global v1.150 text cells untranslated: an empty or
literal "0" English column (8) next to authored Korean (2). The retail client
shows those cells empty; for example, all 15 Roc Mountain region banners pop
up blank. This is a documented product localization layer, NOT native parity.

- Data lives in englishCompletions/<textfile>.json as
  { key: { english, basis, source } }. `source` is the exact retail Korean
  the English translates. If Joymax's Korean changes, the build fails as
  stale instead of silently showing an outdated translation.
- A completion applies only while the retail English is missing, so a
  shipped translation wins unless englishCorrections.mjs contains an exact,
  reviewed correction for that symbol and source wording.
- assertEnglishCompletionCoverage makes an untranslated active row a build
  error, so "every empty English cell is filled" stays true as data changes.
- The same completion feeds the client catalogs, the quest asset, and the
  server's textdata projection (one policy, like itemTextCompletions).

===========================================================================
*/

import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { correctedEnglish } from "./englishCorrections.mjs";

export const ENGLISH_COMPLETION_FILES = Object.freeze( [
	"textuisystem.txt",
	"textzonename.txt",
	"textdataname.txt",
	"texthelp.txt",
	"textquest.txt"
] );

const CATALOG_DIR = path.join( import.meta.dirname, "englishCompletions" );
const PLACEHOLDER = /^(?:-|xxx|null|0)?$/i;

/*
================
isPlaceholderText

A cell that carries no text (empty, "0", "xxx", "-", "null").
================
*/
export function isPlaceholderText( value ) {
	return typeof value !== "string" || PLACEHOLDER.test( value.trim() );
}

/*
================
needsEnglish

Active retail row whose Korean is authored text but whose English is missing.
================
*/
export function needsEnglish( columns ) {
	return columns[0]?.trim() === "1" && !isPlaceholderText( columns[2] ) && isPlaceholderText( columns[8] );
}

/*
================
markupSignature

The formatting skeleton a translation must preserve verbatim and in order:
ASCII markup tags (<br>, <font color="...">, <sml2>, ...) and printf specs.
Korean pseudo-tags such as <아이템 구성> are prose and are translated.
================
*/
export function markupSignature( text ) {
	// Real renderer tags only (a prose "<Item Contents>" is not markup), and
	// printf conversions as the client formats them ("%d", "%s", "%%"); a
	// literal "10% every" is prose.
	const pattern =
		/<\/?(?:br|font|strong|sml2|img|center|left|right|b|i|u|p|table|tr|td)\b[^>]*>|%%|%[-+#0]?\d*(?:\.\d+)?[dsuifxXc]|\\n/gi;
	return [ ...String( text ).matchAll( pattern ) ].map( ( match ) => match[0].replace( /\s+/g, " " ).toLowerCase() );
}

const catalogs = new Map();

/*
================
loadEnglishCompletions

The completion catalog for one textdata file (empty when none is authored).
================
*/
export function loadEnglishCompletions( fileName ) {
	const name = fileName.toLowerCase();
	if ( !ENGLISH_COMPLETION_FILES.includes( name ) ) throw new Error( `No English completion policy for ${fileName}` );
	if ( !catalogs.has( name ) ) {
		const file = path.join( CATALOG_DIR, name.replace( /\.txt$/, ".json" ) );
		let catalog = {};
		try {
			catalog = JSON.parse( readFileSync( file, "utf8" ) );
		} catch ( error ) {
			if ( error?.code !== "ENOENT" ) throw error;
		}
		catalogs.set( name, Object.freeze( catalog ) );
	}
	return catalogs.get( name );
}

/*
================
listEnglishCompletionCatalogs
================
*/
export function listEnglishCompletionCatalogs() {
	return readdirSync( CATALOG_DIR ).filter( ( name ) => name.endsWith( ".json" ) );
}

/*
================
completedEnglish

English for one retail row: reviewed shipped English, or the authored completion
when the shipped cell is missing. Throws when a completion's recorded Korean
source no longer matches the row (stale translation).
================
*/
export function completedEnglish( fileName, columns ) {
	const english = columns[8]?.trim() ?? "";
	const key = columns[1]?.trim();
	if ( !isPlaceholderText( english ) ) return correctedEnglish( fileName, key, english );
	const entry = key ? loadEnglishCompletions( fileName )[key] : undefined;
	if ( !entry ) return english;
	const korean = columns[2]?.trim() ?? "";
	if ( entry.source.trim() !== korean ) {
		throw new Error(
			`Stale English completion ${fileName}:${key}: retail Korean changed from ${
				JSON.stringify( entry.source )
			} to ${JSON.stringify( korean )}`
		);
	}
	return entry.english;
}

/*
================
validateEnglishCompletionCatalog

Validates every entry: known key, matching source, preserved markup, real
English. Returns the entry count.
================
*/
export function validateEnglishCompletionCatalog( fileName, rows ) {
	const catalog = loadEnglishCompletions( fileName );
	const byKey = new Map();
	for ( const columns of rows ) {
		const key = columns[1]?.trim();
		if ( key && !byKey.has( key ) ) byKey.set( key, columns );
	}
	const problems = [];
	for ( const [key, entry] of Object.entries( catalog ) ) {
		const columns = byKey.get( key );
		if ( !columns ) {
			problems.push( `${key}: not a ${fileName} key` );
			continue;
		}
		// A line with words needs English words. A line that is only punctuation
		// (a trailing-off "…" in dialogue) translates to punctuation.
		const needsWords = /[\p{L}\p{N}]/u.test( entry.source );
		if (
			typeof entry.english !== "string" || isPlaceholderText( entry.english ) ||
			(needsWords && !/[A-Za-z]/.test( entry.english ))
		) {
			problems.push( `${key}: english is not text` );
		}
		if ( /[ᄀ-ᇿ㄰-㆏가-힯]/.test( entry.english ) ) problems.push( `${key}: english still contains Hangul` );
		if ( entry.source.trim() !== (columns[2]?.trim() ?? "") ) problems.push( `${key}: source Korean is stale` );
		const want = markupSignature( entry.source ).join( "\u0000" );
		const got = markupSignature( entry.english ).join( "\u0000" );
		if ( want !== got ) problems.push( `${key}: markup/placeholders differ from the Korean` );
	}
	if ( problems.length ) {
		throw new Error(
			`Invalid English completions for ${fileName} (${problems.length}):\n${problems.slice( 0, 40 ).join( "\n" )}`
		);
	}
	return Object.keys( catalog ).length;
}

/*
================
assertEnglishCompletionCoverage

Coverage gate: every active row with Korean text must end with English, from
retail, this catalog, or a caller-supplied completion layer (`covered(key)`,
e.g. the item-name catalog or the guide-title map).
================
*/
export function assertEnglishCompletionCoverage( fileName, rows, covered = ( _key ) => false ) {
	validateEnglishCompletionCatalog( fileName, rows );
	const catalog = loadEnglishCompletions( fileName );
	const missing = [];
	const seen = new Set();
	for ( const columns of rows ) {
		const key = columns[1]?.trim();
		if ( !key || seen.has( key ) ) continue;
		seen.add( key );
		if ( needsEnglish( columns ) && !Object.hasOwn( catalog, key ) && !covered( key ) ) missing.push( key );
	}
	if ( missing.length ) {
		throw new Error(
			`Untranslated ${fileName} rows (${missing.length}); add them to scripts/build/shared/englishCompletions/` +
				`${fileName.replace( /\.txt$/, ".json" )} (scripts/tools/localization/englishGaps.mjs lists them):\n` +
				missing.slice( 0, 40 ).join( "\n" )
		);
	}
}

/*
================
completeEnglishTextProjection

Server projection: completes missing English and applies reviewed corrections, keeping
the BOM, record separators and every other column byte-for-byte.
================
*/
export function completeEnglishTextProjection( fileName, bytes ) {
	const utf16 = bytes[0] === 0xff && bytes[1] === 0xfe;
	const encoding = utf16 ? "utf16le" : "utf8";
	// Records end in \r\n; cells may contain bare LF (same split as the item projection).
	const text = bytes.toString( encoding ).replace( /[^\r]+/g, ( record ) => {
		if ( record.trimStart().startsWith( "//" ) ) return record;
		const columns = record.split( "\t" );
		if ( columns.length < 9 ) return record;
		const english = completedEnglish( fileName, columns );
		if ( isPlaceholderText( english ) || english === columns[8]?.trim() ) return record;
		columns[8] = english;
		return columns.join( "\t" );
	} );
	return Buffer.from( text, encoding );
}
