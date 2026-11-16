/*
===========================================================================

projectionFiles.mjs - the server projection's JSON files and world paths

The server game-data projection reads the client's world tree and writes
JSON documents beside it. Its modules (buildServerGameDataBundle.mjs,
objectNavProjection.mjs) share these three helpers, so the one rule for
which world paths are safe to follow lives in one place.

===========================================================================
*/
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

/*
================
writeJson
================
*/
export async function writeJson( filename, value ) {
	await mkdir( path.dirname( filename ), { recursive: true } );
	await writeFile( filename, `${JSON.stringify( value, null, 2 )}\n` );
}

/*
================
readJson
================
*/
export async function readJson( filename ) {
	return JSON.parse( await readFile( filename, "utf8" ) );
}

/*
================
worldAuthorityPath
================
*/
export function worldAuthorityPath( publicPath ) {
	const prefix = "/assets/world/";
	if ( typeof publicPath !== "string" || !publicPath.startsWith( prefix ) ) {
		throw new Error( `World asset is outside ${prefix}: ${JSON.stringify( publicPath )}` );
	}
	const relativePath = publicPath.slice( prefix.length );
	if ( !relativePath || path.posix.normalize( relativePath ) !== relativePath || relativePath.startsWith( "../" ) ) {
		throw new Error( `Unsafe world asset path: ${JSON.stringify( publicPath )}` );
	}
	return relativePath;
}
