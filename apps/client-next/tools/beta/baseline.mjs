/*
===========================================================================

baseline.mjs - compare a beta source snapshot with normal production defaults.

This validation artifact uses the shared application release builder and the
exact asset bytes from a verified generation. It does not publish production.

===========================================================================
*/

import { link, mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { buildApplicationRelease } from "./application-release.mjs";
import { verifyDirectory } from "./policy.mjs";

/*
================
main

Hard-link verified assets only into the fresh destination. The compiler owns
application files, so comparison cannot accidentally overwrite the beta input.
================
*/
async function main() {
	const [generation, destination] = process.argv.slice( 2 );
	if ( !generation || !destination ) {
		throw Error( "Usage: baseline.mjs BETA_GENERATION FRESH_DESTINATION" );
	}
	const original = path.resolve( generation, "package" );
	const manifest = await verifyDirectory( original );
	const source = JSON.parse( await readFile( path.join( generation, "private/source.json" ), "utf8" ) );
	const result = await buildApplicationRelease( { manifest, source, destination, mode: "production" } );
	for ( const entry of manifest.files ) {
		if ( entry.kind === "application" ) continue;
		const target = path.join( result.packageRoot, entry.path );
		await mkdir( path.dirname( target ), { recursive: true } );
		await link( path.join( original, entry.path ), target );
	}
	await verifyDirectory( result.packageRoot );
	const identical = result.manifest.files.filter( entry =>
		entry.kind === "application" && manifest.files.some( previous => previous.sha256 === entry.sha256 )
	);
	console.log( JSON.stringify(
		{
			baseline: result.packageRoot,
			sourceHash: result.manifest.sourceHash,
			releaseId: result.manifest.releaseId,
			identicalApplicationChunks: identical.map( entry => entry.path )
		},
		null,
		2
	) );
}

await main();
