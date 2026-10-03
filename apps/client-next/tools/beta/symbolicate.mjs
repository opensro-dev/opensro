/*
===========================================================================

symbolicate.mjs - one generated location of a release back to its source

Reads the release's private source map for a script, after checking it
belongs to that release and is the map the release recorded, and resolves
a one-based generated line and column through the shared symbolizer
(tools/perf/core/symbols.mjs).

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createSourceMap } from "../perf/core/symbols.mjs";
import { sha, safeName } from "./policy.mjs";

/*
================
symbolicate
================
*/
export async function symbolicate( privateRoot, releaseId, script, line, column ) {
	const descriptor = JSON.parse( await readFile( path.join( privateRoot, "debug.json" ), "utf8" ) );
	if ( descriptor.releaseId !== releaseId ) throw Error( "Debug artifacts belong to another release" );
	const name = safeName( script.replace( /^\//, "" ) + ".map" ), expected = descriptor.maps[name];
	if ( !expected ) throw Error( "No private map for this chunk" );
	const bytes = await readFile( path.join( privateRoot, "maps", name ) );
	if ( sha( bytes ) !== expected ) throw Error( "Private map integrity mismatch" );
	if ( !Number.isInteger( line ) || !Number.isInteger( column ) || line < 1 || column < 1 ) {
		throw Error( "Expected 1-based generated line and column" );
	}
	return createSourceMap( JSON.parse( bytes ) ).lookup( line - 1, column - 1 );
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	const [directory, id, script, line, column] = process.argv.slice( 2 );
	console.log( await symbolicate( directory, id, script, Number( line ), Number( column ) ) );
}
