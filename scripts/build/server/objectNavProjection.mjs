/*
===========================================================================

objectNavProjection.mjs - the object nav meshes GameWorld walks on

A region's placed objects (bridges, ferries, fortress walls) carry their
own navigation mesh: the BSR names it and the BMS holds the native
payload. This module projects, for one region bundle, the meshes its
placed objects use into object-nav files plus one index, either from the
bundle's inline resources or from the shared resource index it names.
buildServerGameDataBundle.mjs owns the bundle; this module owns only the
object nav part of it.

===========================================================================
*/
import { createHash } from "node:crypto";
import path from "node:path";
import { compareText } from "../shared/textOrder.mjs";
import { readJson, worldAuthorityPath, writeJson } from "./projectionFiles.mjs";

/*
================
buildObjectNavProjection
================
*/
export async function buildObjectNavProjection(
	authorityRoot,
	bundlePath,
	sourceBundle,
	worldRoot,
	referencedObjectNav
) {
	const resources = sourceBundle.objects?.resources;
	if ( !resources || !Array.isArray( resources.bsr ) || !Array.isArray( resources.meshes ) ) {
		const referencedIndex = sourceBundle.objects?.resourceIndexPublicPath;
		if ( !referencedIndex ) {
			return "";
		}
		return buildReferencedObjectNavProjection(
			authorityRoot,
			worldRoot,
			referencedIndex,
			referencedObjectNav
		);
	}
	const usedObjectIds = new Set();
	for ( const region of sourceBundle.navmesh?.regions ?? [] ) {
		for ( const object of region.objects ?? [] ) {
			// Object 0 is a real object (4413B0 bounds only the top): the
			// Jangan ferry's own pieces are object 0 in their bundles.
			if ( Number.isInteger( object.assetId ) && object.assetId >= 0 ) {
				usedObjectIds.add( object.assetId );
			}
		}
	}
	const meshBySource = new Map(
		resources.meshes
			.filter( hasObjectNavPayload )
			.map( ( mesh ) => [ normalizeGamePath( mesh.sourcePath ), mesh ] )
	);
	const areaRoot = path.posix.dirname( bundlePath );
	const bundleStem = path.posix.basename( bundlePath, ".json" );
	const meshFiles = [];
	const emittedMeshes = new Map();
	const bsr = [];

	for ( const row of [ ...resources.bsr ].sort( ( left, right ) => left.objectId - right.objectId ) ) {
		if ( !usedObjectIds.has( row.objectId ) ) {
			continue;
		}
		const resolved = navMeshSourcePaths( row )
			.filter( ( sourcePath ) => meshBySource.has( sourcePath ) );
		if ( resolved.length === 0 ) {
			continue;
		}
		bsr.push( {
			objectId: row.objectId,
			renderMeshSection: { paths: resolved },
			meshPaths: resolved
		} );
		for ( const sourcePath of resolved ) {
			if ( emittedMeshes.has( sourcePath ) ) {
				continue;
			}
			const mesh = meshBySource.get( sourcePath );
			const meshPath = path.posix.join(
				areaRoot,
				"object-nav",
				`${bundleStem}-${digestText( sourcePath ).slice( 0, 16 )}.json`
			);
			emittedMeshes.set( sourcePath, meshPath );
			meshFiles.push( { sourcePath, path: meshPath } );
			await writeJson( path.join( authorityRoot, meshPath ), {
				format: "sro-server-object-nav-mesh",
				version: 1,
				mesh: {
					byteLength: mesh.byteLength,
					headerOffsets: mesh.headerOffsets,
					nativePayloads: mesh.nativePayloads
				}
			} );
		}
	}
	if ( bsr.length === 0 ) {
		return "";
	}
	meshFiles.sort( ( left, right ) => compareText( left.sourcePath, right.sourcePath ) );
	const indexPath = path.posix.join( areaRoot, `${bundleStem}-object-nav.json` );
	await writeJson( path.join( authorityRoot, indexPath ), {
		format: "sro-server-object-nav-index",
		version: 1,
		bsr,
		meshFiles
	} );
	return indexPath;
}

/*
================
buildReferencedObjectNavProjection
================
*/
async function buildReferencedObjectNavProjection(
	authorityRoot,
	worldRoot,
	publicIndexPath,
	cache
) {
	const relativeIndexPath = worldAuthorityPath( publicIndexPath );
	if ( cache.has( relativeIndexPath ) ) {
		return cache.get( relativeIndexPath );
	}

	const sourceIndex = await readJson( path.join( worldRoot, relativeIndexPath ) );
	const referencedMeshSources = new Set();
	for ( const row of sourceIndex.bsr ?? [] ) {
		for ( const sourcePath of navMeshSourcePaths( row ) ) referencedMeshSources.add( sourcePath );
	}

	const meshPathBySource = new Map();
	const meshFiles = [];
	for ( const descriptor of sourceIndex.meshFiles ?? [] ) {
		const sourcePath = normalizeGamePath( descriptor.sourcePath );
		if ( !referencedMeshSources.has( sourcePath ) ) {
			continue;
		}
		const relativeMeshPath = worldAuthorityPath( descriptor.publicPath );
		const document = await readJson( path.join( worldRoot, relativeMeshPath ) );
		const mesh = document.mesh ?? document;
		if ( !hasObjectNavPayload( mesh ) ) {
			continue;
		}
		const projectedPath = path.posix.join(
			path.posix.dirname( relativeIndexPath ),
			"object-nav",
			`${digestText( sourcePath ).slice( 0, 16 )}.json`
		);
		await writeJson( path.join( authorityRoot, projectedPath ), {
			format: "sro-server-object-nav-mesh",
			version: 1,
			mesh: {
				byteLength: mesh.byteLength,
				headerOffsets: mesh.headerOffsets,
				nativePayloads: mesh.nativePayloads
			}
		} );
		meshPathBySource.set( sourcePath, projectedPath );
		meshFiles.push( { sourcePath, path: projectedPath } );
	}

	const bsr = [];
	for ( const row of sourceIndex.bsr ?? [] ) {
		const resolved = navMeshSourcePaths( row ).filter( ( sourcePath ) => meshPathBySource.has( sourcePath ) );
		if ( resolved.length === 0 ) {
			continue;
		}
		bsr.push( {
			objectId: row.objectId,
			renderMeshSection: { paths: resolved },
			meshPaths: resolved
		} );
	}
	meshFiles.sort( ( left, right ) => compareText( left.sourcePath, right.sourcePath ) );
	bsr.sort( ( left, right ) => left.objectId - right.objectId );

	const projectedIndexPath = path.posix.join(
		path.posix.dirname( relativeIndexPath ),
		`${path.posix.basename( relativeIndexPath, ".json" )}-object-nav.json`
	);
	await writeJson( path.join( authorityRoot, projectedIndexPath ), {
		format: "sro-server-object-nav-index",
		version: 1,
		bsr,
		meshFiles
	} );
	cache.set( relativeIndexPath, projectedIndexPath );
	return projectedIndexPath;
}

/*
================
navMeshSourcePaths

The mesh an object's navigation comes from: 441E40 loads it from the one
path the BSR header names at offset slot 7 (primaryMeshPath); the render
meshes carry none, and a BSR without that path has no object navigation.
The projection keeps its renderMeshSection/meshPaths fields, now holding
that one path, so every server reading this index still decodes it.
================
*/
function navMeshSourcePaths( row ) {
	return row.primaryMeshPath ? [ normalizeGamePath( row.primaryMeshPath ) ] : [];
}

/*
================
hasObjectNavPayload
================
*/
export function hasObjectNavPayload( mesh ) {
	return Array.isArray( mesh?.nativePayloads ) &&
		mesh.nativePayloads.some( ( payload ) => payload.kind === "bms-offset7-post-payload-tail" );
}

/*
================
digestText
================
*/
function digestText( value ) {
	return createHash( "sha256" ).update( value ).digest( "hex" );
}

/*
================
normalizeGamePath
================
*/
function normalizeGamePath( value ) {
	return String( value ?? "" ).trim().replaceAll( "\\", "/" ).replace( /^\/+/, "" ).toLowerCase();
}
