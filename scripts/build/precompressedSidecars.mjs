/*
===========================================================================

precompressedSidecars.mjs - retire precompressed sidecars nothing produces

A .br/.gz/.zst file beside a public asset is served by content negotiation
in place of the asset itself. Outside the packs root, the JSON compressor
(jsonAssetCompression.mjs) is the only producer, so a sidecar is live only
when its base is an existing .json file. Anything else - GLB sidecars left
by an earlier pipeline, or a sidecar whose asset was retired - would shadow
a rebuilt asset with stale bytes, so the full build soft-archives it
through archiveGeneratedArtifact (temp/archives/generated-artifacts/).

The packs root has its own rule and collector (assetPackGarbage.mjs).

===========================================================================
*/

import { existsSync } from "node:fs";
import { stat } from "node:fs/promises";
import path from "node:path";

import { archiveGeneratedArtifact } from "./artifacts/generatedArtifactArchive.mjs";
import { listFiles } from "./shared/fsUtils.mjs";
import { PRECOMPRESSED_ASSET_SUFFIXES } from "./shared/compressionUtils.mjs";

/*
================
sidecarBase

The asset a sidecar shadows, or null when the file is not a sidecar.
================
*/
function sidecarBase( file ) {
	const suffix = PRECOMPRESSED_ASSET_SUFFIXES.find( ( candidate ) => file.endsWith( candidate ) );
	return suffix ? file.slice( 0, -suffix.length ) : null;
}

/*
================
retireUnownedSidecars

Returns { retired: [{ file, bytes }], retiredBytes }. With apply, each
retired sidecar is soft-archived.
================
*/
/**
 * @param {{ publicRoot: string, apply?: boolean }} options
 */
export async function retireUnownedSidecars( { publicRoot, apply = false } ) {
	const assetsRoot = path.join( publicRoot, "assets" );
	const packsRoot = path.join( assetsRoot, "packs" ) + path.sep;
	const retired = [];
	for ( const file of await listFiles( assetsRoot ) ) {
		if ( file.startsWith( packsRoot ) ) continue;
		const base = sidecarBase( file );
		if ( !base ) continue;
		if ( base.toLowerCase().endsWith( ".json" ) && existsSync( base ) ) continue;
		retired.push( { file, bytes: (await stat( file )).size } );
	}
	if ( apply ) {
		for ( const { file } of retired ) {
			await archiveGeneratedArtifact( file, { scopeRoot: publicRoot, reason: "unowned-precompressed-sidecar" } );
		}
	}
	return { retired, retiredBytes: retired.reduce( ( sum, entry ) => sum + entry.bytes, 0 ) };
}
