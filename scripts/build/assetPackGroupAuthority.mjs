/*
===========================================================================

assetPackGroupAuthority.mjs - converge one asset-pack group on its loose files

A focused publisher refreshes one pack group. This module makes that refresh
convergent: before the group is published, every member that still has a
loose file is re-hashed, so an older packed member can never hide a newer
loose source.

===========================================================================
*/

import { stat } from "node:fs/promises";
import path from "node:path";

import { refreshPrecompressedSidecars } from "./generatedManifestSidecars.mjs";
import { containedPublicFile, normalizePublicAssetPath } from "./shared/assetPaths.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./sparseAssetPackGroupRefresh.mjs";

const DISCOVERY_CONCURRENCY = 16;
const PACKED_JSON_SUFFIX = ".json.gz";

/*
================
reconcileAssetPackGroupFromLooseAuthority

Reconciles every loose authority that still exists for one asset-pack group.

A sparse refresh may preserve compacted members that no longer have loose
files. That is safe, but a caller-provided delta is not proof that the rest
of the group's loose projection is current. Before publishing, this:

 1. refreshes precompressed sidecars for every packed JSON authority;
 2. hashes every group member that still has a loose projection; and
 3. rebuilds only the pack slots whose bytes actually changed.

The group is therefore convergent: publishing it cannot hide an unrelated
newer loose source behind an older packed member.
================
*/
export async function reconcileAssetPackGroupFromLooseAuthority( options ) {
	const publicRoot = path.resolve( options.publicRoot );
	const groupName = String( options.groupName );
	const previousAssets = options.previousIndex.assets?.filter( ( asset ) => asset.group === groupName ) ?? [];
	if ( !options.previousIndex.groups?.some( ( group ) => group.name === groupName ) ) {
		throw new Error( `Cannot reconcile absent asset-pack group ${groupName}.` );
	}

	const jsonAuthorities = await existingJsonAuthorities( publicRoot, previousAssets );
	const sidecarResults = await refreshPrecompressedSidecars( jsonAuthorities, {
		onlyWhenStale: true,
		...options.sidecarOptions
	} );
	const looseFiles = await existingLooseMembers( publicRoot, previousAssets );
	const refreshed = await patchAssetPackGroupFromLooseFiles( {
		...options,
		publicRoot,
		groupName,
		looseFiles
	} );

	return {
		...refreshed,
		authorityFileCount: looseFiles.length,
		jsonAuthorityCount: jsonAuthorities.length,
		refreshedJsonSidecarCount: sidecarResults.filter( ( result ) => (result.written?.length ?? 0) > 0 ).length
	};
}

/*
================
existingJsonAuthorities

Loose .json files that back a packed .json.gz member of the group.
================
*/
async function existingJsonAuthorities( publicRoot, assets ) {
	const candidates = uniquePaths(
		assets
			.map( ( asset ) => normalizePublicAssetPath( asset.path ) )
			.filter( ( publicPath ) => publicPath.toLowerCase().endsWith( PACKED_JSON_SUFFIX ) )
			.map( ( publicPath ) => publicPath.slice( 0, -3 ) )
	);
	const rows = await mapWithConcurrency( candidates, DISCOVERY_CONCURRENCY, async ( publicPath ) => {
		const absolutePath = containedPublicFile( publicRoot, publicPath );
		return (await isFile( absolutePath )) ? absolutePath : undefined;
	} );
	return rows.filter( Boolean );
}

/*
================
existingLooseMembers

Group members that still have a loose file on disk.
================
*/
async function existingLooseMembers( publicRoot, assets ) {
	const candidates = uniquePaths( assets.map( ( asset ) => asset.path ) );
	const rows = await mapWithConcurrency( candidates, DISCOVERY_CONCURRENCY, async ( publicPath ) => {
		return (await isFile( containedPublicFile( publicRoot, publicPath ) )) ? publicPath : undefined;
	} );
	return rows.filter( Boolean );
}

/*
================
isFile

True for an existing regular file; false only for ENOENT, so permission
and I/O errors still surface.
================
*/
async function isFile( filePath ) {
	try {
		return (await stat( filePath )).isFile();
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return false;
		throw error;
	}
}

/*
================
uniquePaths

Normalized public paths, deduplicated case-insensitively (the last spelling
wins) and sorted, so the result is stable across runs.
================
*/
function uniquePaths( values ) {
	const paths = new Map();
	for ( const value of values ) {
		const normalized = normalizePublicAssetPath( value );
		paths.set( normalized.toLowerCase(), normalized );
	}
	return [ ...paths.values() ].sort( ( left, right ) => left.localeCompare( right ) );
}
