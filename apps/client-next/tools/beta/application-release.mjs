/*
===========================================================================

application-release.mjs - compile a browser release over an immutable asset base.

The same compiler and manifest projection serve baseline comparisons and CI
candidates. Application files are materialized here; retained asset files remain
references until a verified local package or the staging host supplies them.

===========================================================================
*/

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { buildApplication } from "./build.mjs";
import { compressRoutes } from "./compression.mjs";
import { files, releaseIdentity, sha } from "./policy.mjs";
import { ASSET_SCHEMA, RELEASE_PROTOCOL } from "../../src/engine/foundation/release/protocol.ts";

/*
================
buildApplicationRelease

Use a fresh destination and retain private maps outside the package. Compress
only new application routes; the inherited data inventory stays byte-identical.
================
*/
export async function buildApplicationRelease( { manifest, source, destination, mode = "beta" } ) {
	if ( manifest.releaseId !== releaseIdentity( manifest ) ) {
		throw Error( "Asset base manifest identity mismatch" );
	}
	if ( manifest.assetSchema !== ASSET_SCHEMA ) {
		throw Error( "Asset base schema differs from the compiled client; build a data release" );
	}
	await mkdir( destination, { recursive: false } );
	const packageRoot = path.join( destination, "package" );
	const privateRoot = path.join( destination, "private" );
	await mkdir( privateRoot );
	await writeFile( path.join( privateRoot, "source.json" ), JSON.stringify( source ), { flag: "wx" } );
	const maps = await buildApplication( { directory: path.join( packageRoot, "application" ), source, mode } );
	for ( const [name, bytes] of maps ) {
		const target = path.join( privateRoot, "maps", name );
		await mkdir( path.dirname( target ), { recursive: true } );
		await writeFile( target, bytes, { flag: "wx" } );
	}

	const application = { files: [], routes: [] };
	for ( const name of await files( path.join( packageRoot, "application" ) ) ) {
		const file = "application/" + name;
		const bytes = await readFile( path.join( packageRoot, file ) );
		const mime = name.endsWith( ".js" ) ? "text/javascript" : name.endsWith( ".css" ) ? "text/css" : "text/html";
		application.files.push( { path: file, length: bytes.length, sha256: sha( bytes ), kind: "application" } );
		application.routes.push( { url: "/" + name, file, offset: 0, length: bytes.length, mime } );
	}
	await compressRoutes( packageRoot, application );
	const result = {
		...manifest,
		// The wire contract belongs to the new application. Only the asset
		// schema belongs to the retained data; a coordinated release can change
		// the protocol without changing those bytes.
		protocol: RELEASE_PROTOCOL,
		sourceHash: sha( JSON.stringify( source ) ),
		privateMaps: maps.size,
		files: [ ...manifest.files.filter( row => row.kind !== "application" ), ...application.files ],
		routes: [ ...manifest.routes.filter( row => !row.file.startsWith( "application/" ) ), ...application.routes ]
	};
	result.files.sort( ( first, second ) => first.path.localeCompare( second.path ) );
	result.routes.sort( ( first, second ) => first.url.localeCompare( second.url ) );
	result.releaseId = releaseIdentity( result );
	await writeFile( path.join( packageRoot, "release.json" ), JSON.stringify( result, null, 2 ), { flag: "wx" } );
	return { packageRoot, privateRoot, manifest: result };
}
