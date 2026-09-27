/*
===========================================================================

assetPackGarbage.mjs - retire pack outputs the published index no longer uses

Incremental publishers write rebuilt and appended packs into fresh slot
folders (packs/incremental/<group>/slots/...) and then publish a new index.
The superseded slots are never read again, but nothing retired them, so every
refresh left dead packs behind. collectPackGarbage finds every file under the
packs root that the live index does not use (assetPackLiveSet.mjs) and, when
applied, soft-archives it through archiveGeneratedArtifact into
temp/archives/generated-artifacts/, keeping its path and provenance.

A slot folder that still holds a live pack keeps its local manifest and
delivery files: the slot's own build reads them. Emptied folders are removed.

===========================================================================
*/

import { readdir, readFile, rmdir, stat } from "node:fs/promises";
import path from "node:path";

import { archiveGeneratedArtifact } from "./artifacts/generatedArtifactArchive.mjs";
import { livePackFiles } from "./assetPackLiveSet.mjs";
import { listFiles } from "./shared/fsUtils.mjs";

const PACK_ARTIFACT = /\.bin(?:\.zst)?$/iu;

/*
================
collectPackGarbage

Returns { live, garbage: [{ file, bytes }], garbageBytes }. With apply, the
garbage is archived and emptied folders are removed. `index` is the already
validated published index, when the caller has it.
================
*/
/**
 * @param {{ publicRoot: string, apply?: boolean, index?: any }} options
 */
export async function collectPackGarbage( { publicRoot, apply = false, index: publishedIndex } ) {
	const packsRoot = path.join( publicRoot, "assets", "packs" );
	const indexPath = path.join( packsRoot, "manifest.json" );
	// A publisher passes the index it just validated and published; the CLI
	// reads the live one and checks its shape before trusting it.
	const index = publishedIndex ?? JSON.parse( await readFile( indexPath, "utf8" ) );
	if ( !publishedIndex && (index.format !== "sro-asset-pack-index" || !Array.isArray( index.groups )) ) {
		throw new Error( `refusing to collect garbage against an invalid pack index: ${indexPath}` );
	}

	const live = livePackFiles( publicRoot, indexPath, index );
	const liveDirectories = new Set(
		[ ...live ].filter( ( file ) => PACK_ARTIFACT.test( file ) ).map( ( file ) => path.dirname( file ) )
	);

	const garbage = [];
	for ( const file of await listFiles( packsRoot ) ) {
		const key = path.resolve( file ).toLowerCase();
		if ( live.has( key ) ) {
			continue;
		}
		if ( !PACK_ARTIFACT.test( file ) && liveDirectories.has( path.dirname( key ) ) ) {
			continue;
		}
		garbage.push( { file, bytes: (await stat( file )).size } );
	}

	if ( apply ) {
		for ( const { file } of garbage ) {
			await archiveGeneratedArtifact( file, { scopeRoot: publicRoot, reason: "unreferenced-asset-pack-output" } );
		}
		await removeEmptyDirectories( packsRoot );
	}
	return { live: live.size, garbage, garbageBytes: garbage.reduce( ( sum, entry ) => sum + entry.bytes, 0 ) };
}

/*
================
removeEmptyDirectories

Removes empty folders below root, deepest first. root itself is kept.
================
*/
async function removeEmptyDirectories( root ) {
	for ( const entry of await readdir( root, { withFileTypes: true } ) ) {
		if ( !entry.isDirectory() ) {
			continue;
		}
		const directory = path.join( root, entry.name );
		await removeEmptyDirectories( directory );
		if ( (await readdir( directory )).length === 0 ) {
			await rmdir( directory );
		}
	}
}
