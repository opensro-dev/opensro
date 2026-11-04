/*
===========================================================================

move-identity.mjs - the identity proof for one ui.ts window move

A move PR takes a block out of ui.ts and lands it in a window module. The
proof this tool runs is the repository's scripted-move rule: lex both
sides, strip comments, collapse whitespace outside string and template
text, and require a character-identical token stream. One mechanical
normalisation is allowed on top of that, and only one: a shared-state
field the census (#2515) lists may be rewritten from the bare closure
name `x` to the window record's `state.x`. The tool that applies the
normalisation is the tool that checks it - normaliseRemoved and
normaliseAdded are the two directions of the same rewrite, and proveMove
compares only their output.

What the proof does not cover is reported as residue: the tokens left
over on each side after the longest common prefix and suffix are
trimmed. Review reads the residue (imports, the dispatch wiring, any
token that changed), never the whole move.

Usage:
  node tools/move-identity.mjs --removed <file> --added <file>
      [--census <census.json>] [--fields a,b,c]

The census file is the #2515 field list (tests/runtime/ui-census.json):
an object with a "fields" array of rows, each with a "name". Without a
census, no rewrite is allowed and `state.x` in the added text is
residue like anything else.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { root, ts } from "./project.mjs";

// The window state record every moved window reads its shared fields
// through. A census field may appear under this name and no other.
export const STATE_RECORD = "state";

// How many residue tokens each side prints. The count is always exact;
// the listing is capped so a failed proof stays readable.
const RESIDUE_PRINT_LIMIT = 40;

/*
================
tokenizeMoveText

The token stream of one side: scanner tokens with trivia and comments
gone, each token its exact source text. String and template text keeps
its characters, so a changed literal is a changed token.
================
*/
export function tokenizeMoveText( text ) {
	const scanner = ts.createScanner( ts.ScriptTarget.Latest, true, ts.LanguageVariant.Standard, text );
	const tokens = [];
	for ( let kind = scanner.scan(); kind !== ts.SyntaxKind.EndOfFileToken; kind = scanner.scan() ) {
		tokens.push( scanner.getTokenText() );
	}
	return tokens;
}

/*
================
isIdentifierToken

Census rewrites apply to identifier tokens only. The scanner reports
keywords as their own kinds, so a field that shares a keyword's spelling
can never be rewritten by accident.
================
*/
function isIdentifierToken( scannerKinds, index ) {
	return scannerKinds[index] === ts.SyntaxKind.Identifier;
}

/*
================
tokenizeWithKinds

tokenizeMoveText plus the scanner kind of each token, for the rewrite.
================
*/
function tokenizeWithKinds( text ) {
	const scanner = ts.createScanner( ts.ScriptTarget.Latest, true, ts.LanguageVariant.Standard, text );
	const tokens = [], kinds = [];
	for ( let kind = scanner.scan(); kind !== ts.SyntaxKind.EndOfFileToken; kind = scanner.scan() ) {
		tokens.push( scanner.getTokenText() );
		kinds.push( kind );
	}
	return { tokens, kinds };
}

/*
================
normaliseRemoved

The ui.ts side of the rewrite: a bare census field `x` becomes the one
token `state.x`. A field read through a member (`holder.x`) is not the
closure field and is left alone, as is a field in property-name
position (`{ x: 1 }` keeps its name; the value position is the read).
================
*/
export function normaliseRemoved( text, fields ) {
	const { tokens, kinds } = tokenizeWithKinds( text );
	const wanted = new Set( fields );
	const out = [];
	for ( let i = 0; i < tokens.length; i++ ) {
		const previous = tokens[i - 1], next = tokens[i + 1];
		if (
			isIdentifierToken( kinds, i ) && wanted.has( tokens[i] ) && previous !== "." &&
			!(next === ":" && (previous === "{" || previous === ","))
		) {
			out.push( STATE_RECORD + "." + tokens[i] );
		} else {
			out.push( tokens[i] );
		}
	}
	return out;
}

/*
================
normaliseAdded

The window-module side of the same rewrite: the three tokens
`state` `.` `x`, with x a census field, collapse to the one token
`state.x` the removed side normalises to. `state.y` for a field the
census does not list stays three tokens and so lands in the residue.
================
*/
export function normaliseAdded( text, fields ) {
	const { tokens, kinds } = tokenizeWithKinds( text );
	const wanted = new Set( fields );
	const out = [];
	for ( let i = 0; i < tokens.length; i++ ) {
		if (
			isIdentifierToken( kinds, i ) && tokens[i] === STATE_RECORD && tokens[i + 1] === "." &&
			kinds[i + 2] === ts.SyntaxKind.Identifier && wanted.has( tokens[i + 2] )
		) {
			out.push( STATE_RECORD + "." + tokens[i + 2] );
			i += 2;
		} else {
			out.push( tokens[i] );
		}
	}
	return out;
}

/*
================
proveMove

Compares the two normalised streams. Equal streams prove the move.
Otherwise the residue is what review must read: after the longest
common prefix and suffix are trimmed, the removed side's leftover
tokens (code lost or changed) and the added side's (code gained or
changed), each in order.
================
*/
export function proveMove( removedText, addedText, fields = [] ) {
	const removed = normaliseRemoved( removedText, fields );
	const added = normaliseAdded( addedText, fields );
	let prefix = 0;
	while ( prefix < removed.length && prefix < added.length && removed[prefix] === added[prefix] ) prefix++;
	let suffix = 0;
	while (
		suffix < removed.length - prefix && suffix < added.length - prefix &&
		removed[removed.length - 1 - suffix] === added[added.length - 1 - suffix]
	) suffix++;
	const removedResidue = removed.slice( prefix, removed.length - suffix );
	const addedResidue = added.slice( prefix, added.length - suffix );
	return {
		identical: removedResidue.length === 0 && addedResidue.length === 0,
		removedTokens: removed.length,
		addedTokens: added.length,
		matchedTokens: prefix + suffix,
		removedResidue,
		addedResidue
	};
}

/*
================
readCensusFields

The census field names from a #2515 census file: an array of rows, or
an object with a "fields" array; each row an object with a "name" or a
bare string. Anything else is a hard error - a census the tool cannot
read must not silently allow no rewrites, or every move would fail for
the wrong reason and the failure would train people to bypass it.
================
*/
export function readCensusFields( censusPath ) {
	const parsed = JSON.parse( fs.readFileSync( censusPath, "utf8" ) );
	const rows = Array.isArray( parsed ) ? parsed : parsed?.fields;
	if ( !Array.isArray( rows ) ) throw new Error( `Census ${censusPath} has no field list` );
	return rows.map( ( row, index ) => {
		const name = typeof row === "string" ? row : row?.name;
		if ( typeof name !== "string" || name === "" ) {
			throw new Error( `Census ${censusPath} row ${index} has no field name` );
		}
		return name;
	} );
}

/*
================
formatResidue

One side's residue as it is printed: the exact token count, then the
tokens in order up to the print limit.
================
*/
function formatResidue( label, residue ) {
	const shown = residue.slice( 0, RESIDUE_PRINT_LIMIT ).join( " " );
	const more = residue.length > RESIDUE_PRINT_LIMIT ? ` ... (${residue.length - RESIDUE_PRINT_LIMIT} more)` : "";
	return `${label} (${residue.length} tokens): ${shown}${more}`;
}

/*
================
main

--removed is the text that left ui.ts, --added the text that landed in
the window module (the mover extracts both, e.g. from the PR diff).
Exit 0: the streams are token-identical apart from the census rewrite.
Exit 1: the residue is printed; it is what review must read.
================
*/
function main( argv ) {
	const args = new Map();
	for ( let i = 0; i < argv.length; i += 2 ) {
		if ( !argv[i]?.startsWith( "--" ) || argv[i + 1] === undefined ) {
			throw new Error(
				"Usage: node tools/move-identity.mjs --removed <file> --added <file> [--census <file>] [--fields a,b,c]"
			);
		}
		args.set( argv[i].slice( 2 ), argv[i + 1] );
	}
	const removedPath = args.get( "removed" ), addedPath = args.get( "added" );
	if ( !removedPath || !addedPath ) {
		throw new Error(
			"Usage: node tools/move-identity.mjs --removed <file> --added <file> [--census <file>] [--fields a,b,c]"
		);
	}
	const censusPath = args.get( "census" ) ??
		path.join( root, "tests/runtime/ui-census.json" );
	const fields = args.has( "fields" ) ?
		args.get( "fields" ).split( "," ).filter( Boolean ) :
		fs.existsSync( censusPath ) ?
		readCensusFields( censusPath ) :
		[];
	const proof = proveMove(
		fs.readFileSync( removedPath, "utf8" ),
		fs.readFileSync( addedPath, "utf8" ),
		fields
	);
	console.log(
		`move identity: ${proof.matchedTokens} tokens matched (${proof.removedTokens} removed, ${proof.addedTokens} added), ${fields.length} census fields`
	);
	if ( proof.identical ) {
		console.log( "PASS: token-identical apart from the census rewrite" );
		return;
	}
	console.error( formatResidue( "removed residue - code lost or changed", proof.removedResidue ) );
	console.error( formatResidue( "added residue - code gained or changed; review reads this", proof.addedResidue ) );
	process.exitCode = 1;
}

if ( process.argv[1] === import.meta.filename ) {
	try {
		main( process.argv.slice( 2 ) );
	} catch ( error ) {
		console.error( error );
		process.exitCode = 1;
	}
}
