/*
===========================================================================

check_native_claims.mjs - a pull request that cites the client lists its claims

#2219: CONTRIBUTING.md section 6 asks a pull request resting on native
behaviour to fill the native-claims table. "Rests on native behaviour" is a
judgement, but one mechanical signal is not: a new source comment citing a
client address. Such a comment makes a claim, so the pull request's body must
hold at least one table row. A pull request with no citation is not asked for
anything, because a claim written only in prose cannot be told apart from a
description.

The gate reads the diff and the pull request's body, so it runs from its own
workflow, not from `pnpm check source`. The body is data: it is only parsed.

===========================================================================
*/

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );

// The product sources that carry citations. Tests quote numbers freely.
const CITING_SOURCE = /^apps\/(server\/.*(?<!_test)\.go|client-next\/src\/.*\.tsx?)$/;

// The client's code begins at this address; a lower number in a comment is a
// quantity, not a citation.
const CLIENT_TEXT_BASE = 0x401000;
// Zero padding is absorbed: 0x0085EA70 is the same citation as 85EA70.
const ADDRESS_TOKEN = /\b(0x|sub_)?0{0,2}([0-9a-fA-F]{6})\b/g;

const CLAIMS_HEADING = /^##\s+Native claims\s*$/im;

/*
================
commentTextByLine

The comment text of a Go or TypeScript source, by 1-based line. Strings are
skipped so a URL's slashes do not open a comment.
================
*/
export function commentTextByLine( source ) {
	const lines = new Map();
	let line = 1, state = "code", quote = "";
	const keep = ch => lines.set( line, (lines.get( line ) ?? "") + ch );
	for ( let i = 0; i < source.length; i++ ) {
		const ch = source[i], next = source[i + 1];
		if ( ch === "\n" ) {
			line += 1;
			if ( state === "line" ) state = "code";
			if ( state === "string" && quote !== "`" ) state = "code";
			continue;
		}
		if ( state === "line" ) {
			keep( ch );
		} else if ( state === "block" ) {
			if ( ch === "*" && next === "/" ) {
				state = "code";
				i += 1;
			} else {
				keep( ch );
			}
		} else if ( state === "string" ) {
			if ( ch === "\\" && quote !== "`" ) i += 1;
			else if ( ch === quote ) state = "code";
		} else if ( ch === "/" && next === "/" ) {
			state = "line";
			i += 1;
		} else if ( ch === "/" && next === "*" ) {
			state = "block";
			i += 1;
		} else if ( ch === '"' || ch === "'" || ch === "`" ) {
			state = "string";
			quote = ch;
		}
	}
	return lines;
}

/*
================
addedLinesByFile

The line numbers each file gains, from a `git diff --unified=0`.
================
*/
export function addedLinesByFile( diff ) {
	const added = new Map();
	let file = null;
	for ( const row of diff.split( "\n" ) ) {
		if ( row.startsWith( "+++ " ) ) {
			file = row === "+++ /dev/null" ? null : row.slice( "+++ b/".length );
			continue;
		}
		const hunk = /^@@ -\S+ \+(\d+)(?:,(\d+))? @@/.exec( row );
		if ( !hunk || file === null ) continue;
		const start = Number( hunk[1] ), count = hunk[2] === undefined ? 1 : Number( hunk[2] );
		if ( !added.has( file ) ) added.set( file, new Set() );
		for ( let line = start; line < start + count; line++ ) added.get( file ).add( line );
	}
	return added;
}

/*
================
citedAddresses

The client addresses a piece of comment text names. A round thousand is a
quantity far more often than a function start, and an unprefixed token with
no digit is a word such as "facade": write 0x on a digit-less address.
================
*/
export function citedAddresses( text ) {
	const cited = [];
	for ( const match of text.matchAll( ADDRESS_TOKEN ) ) {
		const prefix = match[1], digits = match[2];
		if ( !prefix && !/[0-9]/.test( digits ) ) continue;
		if ( Number.parseInt( digits, 16 ) >= CLIENT_TEXT_BASE && !digits.endsWith( "000" ) ) cited.push( digits );
	}
	return cited;
}

/*
================
withoutHtmlComments

The text outside HTML comments. An unclosed comment hides the rest, as it
does when GitHub renders the body. A scan, not a replace, so text that a
removal joins up cannot form a new comment.
================
*/
function withoutHtmlComments( text ) {
	let visible = "", at = 0;
	while ( at < text.length ) {
		const open = text.indexOf( "<!--", at );
		if ( open < 0 ) return visible + text.slice( at );
		visible += text.slice( at, open );
		const close = text.indexOf( "-->", open + 4 );
		if ( close < 0 ) return visible;
		at = close + 3;
	}
	return visible;
}

/*
================
claimRowCount

The filled rows of the body's native-claims table. A template's empty header
counts as none, and so does a table quoted inside an HTML comment.
================
*/
export function claimRowCount( body ) {
	const visible = withoutHtmlComments( body ), heading = CLAIMS_HEADING.exec( visible );
	if ( !heading ) return 0;
	const rest = visible.slice( heading.index + heading[0].length ), end = rest.search( /^##\s/m );
	const rows = (end < 0 ? rest : rest.slice( 0, end )).split( /\r?\n/ ).filter( row => row.trim().startsWith( "|" ) );
	// The first two rows are the header and its rule.
	return rows.slice( 2 ).filter( row => row.replace( /[|\s-]/g, "" ) !== "" ).length;
}

/*
================
evaluateClaims

Plain data in, plain verdict out: `files` maps a path to its source at the
head, so a test needs no git and no pull request.
================
*/
export function evaluateClaims( { diff, body, files } ) {
	const citations = [];
	for ( const [file, added] of addedLinesByFile( diff ) ) {
		if ( !CITING_SOURCE.test( file ) ) continue;
		const comments = commentTextByLine( files( file ) );
		for ( const line of added ) {
			for ( const address of citedAddresses( comments.get( line ) ?? "" ) ) {
				citations.push( `${file}:${line} ${address}` );
			}
		}
	}
	return { citations, rows: claimRowCount( body ), ok: citations.length === 0 || claimRowCount( body ) > 0 };
}

/*
================
main
================
*/
function main() {
	const base = process.env.PR_BASE_SHA, body = process.env.PR_BODY ?? "";
	if ( !base ) throw new Error( "PR_BASE_SHA is required" );
	const diff = execFileSync( "git", [ "diff", "--unified=0", "--no-ext-diff", `${base}...HEAD` ], {
		cwd: rebuildRoot,
		encoding: "utf8",
		maxBuffer: 1 << 30
	} );
	const verdict = evaluateClaims( {
		diff,
		body,
		files: file => readFileSync( path.join( rebuildRoot, file ), "utf8" )
	} );
	if ( verdict.ok ) {
		console.log( `native claims: ${verdict.citations.length} new citations, ${verdict.rows} table rows` );
		return;
	}
	console.error( "New client citations, but the Native claims table has no rows:" );
	for ( const citation of verdict.citations ) console.error( `  ${citation}` );
	console.error( "Fill the table in the pull request's body (.github/CONTRIBUTING.md section 6)." );
	process.exitCode = 1;
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
