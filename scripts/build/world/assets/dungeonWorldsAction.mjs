/*
===========================================================================

dungeonWorldsAction.mjs - the dungeon world bundles as one cached action

The dungeon-worlds family (families/looseFamilies.mjs) builds the dungeon
resource manifest from dungeoninfo.txt and the DOF files, then one world
bundle per dungeon region from it. Both read thousands of extracted BSR,
BMS and BMT files and converted images, and both are pure: the same
sources make the same bytes. They run here as one action
(graph/action.mjs) whose reads are discovered as it runs (the readers
note them: world/io.mjs notedExists and notedReadFile, dungeonInfo.mjs,
blockTextures.mjs), so a warm build lays the stored files down instead of
reading the sources again.

What the action does not do is publish the textures the bundles name: a
block texture is an NTX1 encode with its own content cache
(blockTextures.mjs) and a converted PNG is a copy, and both write files
whose list is only known once the bundles are built. The action returns
the texture records, and publishDungeonWorlds publishes them, and the
water images, on a hit and a miss alike.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runGraph } from "../../graph/graph.mjs";
import { copyIntoPublicTree } from "../../shared/publicWrite.mjs";
import { publishBytesAtomically } from "../../shared/atomicPublish.mjs";
import { objectTextureSource } from "../objects/buildTitleSectorObjectResources.mjs";
import { extractedRoot, imageSourceRoot, publicRoot } from "../paths.mjs";
import { publishBlockTextureFile } from "./blockTextures.mjs";
import { buildDungeonResourceManifest, DUNGEON_RESOURCE_PUBLIC_PATH } from "./buildDungeonResources.mjs";
import { buildDungeonWorlds } from "./buildDungeonWorlds.mjs";
import { copyReferencedWaterImages, resolveWaterTextures } from "./copyWaterImages.mjs";
import { readDungeonInfoRows } from "./dungeonInfo.mjs";

const MODULE_FILE = fileURLToPath( import.meta.url );

// The trees the build reads: the extracted game data (dungeoninfo, DOF,
// BSR, BMS, BMT, block DDJ sources) and the converted images.
const DUNGEON_READ_ROOTS = { data: extractedRoot, images: imageSourceRoot };

/*
================
dungeonRegionPublicPath

The world bundle of one dungeon sector (buildDungeonWorlds' file name).
================
*/
export function dungeonRegionPublicPath( sectorId ) {
	return `/assets/world/dungeon/regions/0x${sectorId.toString( 16 ).padStart( 4, "0" )}.json`;
}

/*
================
dungeonSectors

The sectors dungeoninfo.txt admits, read ahead of the action: it declares
its outputs before it runs, so the region list is a parameter of its key,
and the run refuses a manifest that admits any other set.
================
*/
export async function dungeonSectors( root = extractedRoot ) {
	const rows = await readDungeonInfoRows( path.join( root, "Data_extracted", "dungeon", "dungeoninfo.txt" ) );
	return [ ...new Set( rows.map( row => row.regionId | 0x8000 ) ) ].sort( ( a, b ) => a - b );
}

/*
================
relativePublic

A public path as the action's output name under publicRoot.
================
*/
function relativePublic( publicPath ) {
	return publicPath.replace( /^\/+/, "" );
}

/*
================
dungeonWorldsAction

The manifest and the region bundles as one action. Its key is this
module's code closure and the sector list; the files the build reads are
discovered as it runs and checked on reuse. The result is the texture
records the bundles name, for publishDungeonWorlds.
================
*/
export function dungeonWorldsAction( sectors, root = publicRoot ) {
	const manifestPath = path.join( root, relativePublic( DUNGEON_RESOURCE_PUBLIC_PATH ) );
	return {
		name: "family:dungeon-worlds",
		outputRoot: root,
		outputs: [
			relativePublic( DUNGEON_RESOURCE_PUBLIC_PATH ),
			...sectors.map( sector => relativePublic( dungeonRegionPublicPath( sector ) ) )
		],
		code: [ MODULE_FILE ],
		params: { sectors },
		discoverRoots: DUNGEON_READ_ROOTS,
		run: async () => {
			// The sidecars are the family's (onlyWhenStale after the action),
			// never an undeclared output of the action.
			await buildDungeonResourceManifest( { targetPath: manifestPath, refreshSidecars: false } );
			const manifest = JSON.parse( await readFile( manifestPath, "utf8" ) );
			const built = await buildDungeonWorlds( manifest, { publishFiles: false } );
			const admitted = [ ...built.bundles.keys() ].sort( ( a, b ) => a - b );
			if ( admitted.join( "," ) !== sectors.join( "," ) ) {
				throw new Error( `dungeon worlds: manifest admits sectors [${admitted}], declared [${sectors}]` );
			}
			const records = new Map();
			for ( const [sector, bundle] of built.bundles ) {
				const file = path.join( root, relativePublic( dungeonRegionPublicPath( sector ) ) );
				await publishBytesAtomically( file, Buffer.from( JSON.stringify( bundle ) ) );
				for ( const record of bundle.objects.resources.textures ) records.set( record.imagePublicPath, record );
			}
			return {
				textures: built.textures,
				textureRecords: [ ...records.values() ].sort( ( a, b ) =>
					a.imagePublicPath < b.imagePublicPath ? -1 : a.imagePublicPath > b.imagePublicPath ? 1 : 0
				)
			};
		}
	};
}

/*
================
publishTextureRecords

The publish half of copyObjectMaterialTextures, from the records a run
kept: a block source encodes to its NTX1 container, any other texture
copies its converted PNG. Both skip a target that already holds the bytes.
================
*/
async function publishTextureRecords( records, root ) {
	const encodes = [];
	for ( const record of records ) {
		const blockFormat = record.blockFormat ?? null;
		const source = objectTextureSource( {
			textureSourcePath: record.sourcePath,
			blockFormat,
			extractedRoot,
			imageRoot: imageSourceRoot
		} );
		const target = path.join( root, relativePublic( record.imagePublicPath ) );
		if ( blockFormat ) encodes.push( publishBlockTextureFile( source, target ) );
		else await copyIntoPublicTree( source, target );
	}
	await Promise.all( encodes );
}

/*
================
publishDungeonWorlds

Runs the action through the store, then publishes what it names: the
water images and the object textures. Returns the published files.
================
*/
export async function publishDungeonWorlds( store, root = publicRoot ) {
	const sectors = await dungeonSectors();
	const action = dungeonWorldsAction( sectors, root );
	const outcome = (await runGraph( [ action ], { store } ))[action.name];
	await copyReferencedWaterImages( resolveWaterTextures() );
	await publishTextureRecords( outcome.result.textureRecords, root );
	return {
		hit: outcome.hit,
		manifestFile: path.join( root, relativePublic( DUNGEON_RESOURCE_PUBLIC_PATH ) ),
		files: sectors.map( sector => path.join( root, relativePublic( dungeonRegionPublicPath( sector ) ) ) ),
		textures: outcome.result.textures
	};
}
