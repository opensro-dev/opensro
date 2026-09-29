/*
===========================================================================

candidate.mjs - prepare the application artifact consumed by the client workflow.

The downloaded base manifest is untrusted until its identity is checked by the
shared builder; the host additionally binds it to its own verified live assets.
Private source snapshots and maps are never included in the candidate archive.

===========================================================================
*/

import { readFile } from "node:fs/promises";
import path from "node:path";
import { buildApplicationRelease } from "./application-release.mjs";
import { freezeSource } from "./build.mjs";
import { sha } from "./policy.mjs";

/*
================
main

Freeze once and reject any source drift during compilation. The final artifact
is packaged separately with the generation observed before this build began.
================
*/
async function main() {
	const [basePath, destination] = process.argv.slice( 2 );
	if ( !basePath || !destination ) {
		throw Error( "Usage: candidate.mjs BASE_MANIFEST FRESH_DESTINATION" );
	}
	const manifest = JSON.parse( await readFile( basePath, "utf8" ) );
	const source = await freezeSource();
	const result = await buildApplicationRelease( { manifest, source, destination: path.resolve( destination ) } );
	if ( sha( JSON.stringify( await freezeSource() ) ) !== result.manifest.sourceHash ) {
		throw Error( "Application source changed during build" );
	}
	console.log( JSON.stringify( { release: result.manifest.releaseId, package: result.packageRoot } ) );
}

await main();
