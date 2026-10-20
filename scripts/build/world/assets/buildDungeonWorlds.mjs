/*
===========================================================================

buildDungeonWorlds.mjs - publish per-region 0x8000-bit dungeon world bundles

Builds the /assets/world/dungeon/regions/0xXXXX.json world bundles from the
published dungeon resource manifest (sro-dungeon-resources v3), wiring each
DOF room block's BSR/BMS/BMT resources, visibility graph, block fog, and
resolved water surfaces into the runtime world bundle contract.

===========================================================================
*/

import { mkdir } from "node:fs/promises";
import path from "node:path";
import { publishBytesAtomically } from "../../shared/atomicPublish.mjs";
import { buildTitleSectorObjectResources } from "../objects/buildTitleSectorObjectResources.mjs";
import { extractedRoot, gameRoot, normalizeAssetPath, publicRoot } from "../paths.mjs";
import { copyReferencedWaterImages, resolveWaterTextures } from "./copyWaterImages.mjs";

/*
================
buildDungeonWorlds

The native dungeoninfo entries remain the admission authority. Discovering
another DOF on disk must not silently add it to the playable world catalog.
When options.publishFiles is false, builds and returns the bundles in memory
without writing region JSONs or texture files to disk.
================
*/
export async function buildDungeonWorlds( manifest, options = {} ) {
	if ( manifest.format !== "sro-dungeon-resources" || manifest.version !== 3 ) {
		throw Error( "Invalid dungeon publication" );
	}
	const publishFiles = options.publishFiles !== false;
	const water = resolveWaterTextures();
	const files = [];
	const textures = new Set();
	const bundles = new Map();
	if ( publishFiles ) {
		await copyReferencedWaterImages( water );
	}
	for ( const frame of water.normalFramePublicPaths ) {
		textures.add( frame );
	}
	const built = new Map();
	for ( const entry of manifest.entries ) {
		const resource = manifest.resources.find(
			( row ) => normalizeAssetPath( row.normalizedName ) === normalizeAssetPath( entry.normalizedName )
		);
		if ( !resource?.presentation || !resource.waterSurfaces ) {
			throw Error( "Rebuild dungeon provider publication first" );
		}
		let objects = built.get( resource.normalizedName );
		const blocks = resource.presentation.blocks;
		if ( !objects ) {
			const definitions = blocks.map( ( block ) => ({
				objectId: block.index,
				flags: 0,
				sourcePath: block.path
			}) );
			objects = await buildTitleSectorObjectResources( {
				extractedRoot,
				gameRoot,
				area: "dungeon",
				objectDefinitions: definitions,
				publishTextures: publishFiles
			} );
			if ( objects.missingCount ) {
				throw Error( "Incomplete dungeon render resources: " + JSON.stringify( objects.missing ) );
			}
			built.set( resource.normalizedName, objects );
			for ( const image of objects.textures ) {
				textures.add( image.imagePublicPath );
			}
		}
		const region = entry.sectorId;
		const bundle = {
			source: { sectorX: region & 255, sectorY: region >>> 8 },
			terrain: { blocks: [] },
			terrainTextures: { tileCatalog: { referencedTiles: [] } },
			water,
			dungeonBlocks: blocks.map( ( { index, visibleBlocks, ambientColor, fog } ) => ({
				index,
				visibleBlocks,
				ambientColor,
				fog
			}) ),
			dungeonWater: resource.waterSurfaces,
			objects: {
				resources: objects,
				placements: blocks.map( ( block ) => ({
					dungeonBlock: block.index,
					objectId: block.index,
					uid: block.index,
					regionId: String( region ),
					position: {
						x: block.position[0],
						y: block.position[1],
						z: block.position[2]
					},
					yaw: block.yaw
				}) )
			}
		};
		bundles.set( region, bundle );
		const file = path.join(
			publicRoot,
			`assets/world/dungeon/regions/0x${region.toString( 16 ).padStart( 4, "0" )}.json`
		);
		if ( publishFiles ) {
			await mkdir( path.dirname( file ), { recursive: true } );
			await publishBytesAtomically( file, Buffer.from( JSON.stringify( bundle ) ) );
		}
		files.push( file );
	}
	return { files, textures: [ ...textures ], bundles };
}
