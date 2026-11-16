/*
===========================================================================

diff.mjs - the first difference between two presentation replays

Usage: node diff.mjs A B

A and B are replay.mjs outputs (NDJSON, gzipped when they end in .gz).
Each line is rebuilt into its whole frame (frame-delta.mjs) and frames are
compared in order; object keys compare as sets. On the first difference the tool prints the
frame, the gid of the nearest enclosing record that has one (an actor, a
sound, a pose), the JSON path and both values, and exits 1. When every
frame is equal it prints one line with the frame count and exits 0.

===========================================================================
*/
import { createReadStream } from "node:fs";
import path from "node:path";
import { createInterface } from "node:readline";
import { createGunzip } from "node:zlib";
import { applyDelta } from "./frame-delta.mjs";

const USAGE = "usage: node diff.mjs A B";

/*
================
lines

An async iterator over one output's lines.
================
*/
function lines( file ) {
	const source = createReadStream( file );
	const input = file.endsWith( ".gz" ) ? source.pipe( createGunzip() ) : source;
	return createInterface( { input, crlfDelay: Infinity } )[Symbol.asyncIterator]();
}

/*
================
firstDifference

The path to the first differing leaf of two decoded values, with the
gid of the innermost object on the way that carries one, or null when the
values are equal. Keys are compared in A's order, then B's extra keys.
================
*/
function firstDifference( a, b, at = [], gid = undefined ) {
	if ( a !== null && typeof a === "object" && typeof a.gid === "number" ) gid = a.gid;
	else if ( b !== null && typeof b === "object" && typeof b.gid === "number" ) gid = b.gid;
	if ( Object.is( a, b ) ) return null;
	const objects = a !== null && b !== null && typeof a === "object" && typeof b === "object";
	if ( !objects || Array.isArray( a ) !== Array.isArray( b ) ) return { at, gid, a, b };
	if ( Array.isArray( a ) ) {
		for ( let i = 0; i < Math.max( a.length, b.length ); i++ ) {
			if ( i >= a.length || i >= b.length ) {
				return { at: [ ...at, i ], gid, a: a[i] ?? "<absent>", b: b[i] ?? "<absent>" };
			}
			const found = firstDifference( a[i], b[i], [ ...at, i ], gid );
			if ( found ) return found;
		}
		return null;
	}
	for ( const key of new Set( [ ...Object.keys( a ), ...Object.keys( b ) ] ) ) {
		if ( !Object.hasOwn( a, key ) || !Object.hasOwn( b, key ) ) {
			return {
				at: [ ...at, key ],
				gid,
				a: Object.hasOwn( a, key ) ? a[key] : "<absent>",
				b: Object.hasOwn( b, key ) ? b[key] : "<absent>"
			};
		}
		const found = firstDifference( a[key], b[key], [ ...at, key ], gid );
		if ( found ) return found;
	}
	return null;
}

/*
================
jsonPath
================
*/
function jsonPath( at ) {
	return at.map( part => typeof part === "number" ? `[${part}]` : `.${part}` ).join( "" ).replace( /^\./, "" );
}

/*
================
show

A value for the report, cut to a readable length.
================
*/
function show( value ) {
	const text = JSON.stringify( value ) ?? String( value );
	return text.length > 400 ? text.slice( 0, 400 ) + "..." : text;
}

/*
================
main
================
*/
async function main( argv ) {
	if ( argv.length !== 2 ) {
		console.error( USAGE );
		process.exit( 2 );
	}
	const [fileA, fileB] = argv.map( value => path.resolve( value ) );
	const a = lines( fileA ), b = lines( fileB );
	let frameA = null, frameB = null;
	for ( let frame = 0;; frame++ ) {
		const [left, right] = await Promise.all( [ a.next(), b.next() ] );
		if ( left.done && right.done ) {
			console.log( `equal: ${frame} frames` );
			return 0;
		}
		if ( left.done || right.done ) {
			console.log( `frame ${frame}: ${left.done ? "A" : "B"} ends first` );
			return 1;
		}
		const same = left.value === right.value, patchA = JSON.parse( left.value );
		frameA = applyDelta( frameA, patchA );
		frameB = same ? applyDelta( frameB, patchA ) : applyDelta( frameB, JSON.parse( right.value ) );
		if ( same ) continue;
		const found = firstDifference( frameA, frameB );
		if ( !found ) continue;
		console.log( `frame ${frame}` );
		console.log( `gid   ${found.gid ?? "-"}` );
		console.log( `path  ${jsonPath( found.at )}` );
		console.log( `A     ${show( found.a )}` );
		console.log( `B     ${show( found.b )}` );
		return 1;
	}
}

process.exitCode = await main( process.argv.slice( 2 ) );
