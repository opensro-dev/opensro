/*
===========================================================================

buildMapLoaderRegionBundle.mjs - native asset compilation

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import {
	MAPM_TILES_PER_AXIS,
	MAPM_VERTICES_PER_AXIS,
	MAPO2_LOD_GROUPS,
	MAPO2_PLACEMENT_BYTES,
	MAPT_NATIVE_LIGHT_BYTES,
	WATER_NORMAL_FRAME_DURATION_MS
} from "../constants.mjs";
import { extractedRoot, gameRoot, toGameRelative, toHex16 } from "../paths.mjs";
import {
	parseJmxMapObjectPlacementO2,
	parseJmxMapTerrain,
	readJmxMapObjectInfo,
	readJmxMapTileCatalog
} from "../jmx/index.mjs";
import { deriveTitleTerrainSectorCoverage } from "../sworld/deriveTitleTerrainSectorCoverage.mjs";
import { readJmxMapTerrainTextureSector } from "../sworld/prepareCellTerrainTexture.mjs";
import { copyReferencedTerrainTileImages, resolveReferencedTerrainTiles } from "../assets/copyTerrainTileImages.mjs";
import { copyReferencedSkyImages, resolveSkyTextures } from "../assets/copySkyImages.mjs";
import { copyReferencedWaterImages, resolveWaterTextures } from "../assets/copyWaterImages.mjs";
import { buildTitleSectorObjectResources } from "../objects/buildTitleSectorObjectResources.mjs";
import { buildRegionNavmeshData } from "../navmesh/buildRegionNavmeshData.mjs";
import { exists } from "../io.mjs";

/*
================
buildJmxWorldRegionBundle
================
*/
export async function buildJmxWorldRegionBundle( options ) {
	const sectorX = options.sectorX;
	const sectorY = options.sectorY;
	const sectorId = options.sectorId ?? ((sectorY << 8) | sectorX);
	const area = options.area ?? `sector-${sectorId.toString( 16 ).padStart( 4, "0" )}`;
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const sourceGameRoot = options.gameRoot ?? gameRoot;

	const mapBase = path.join( sourceExtractedRoot, "Map_extracted", String( sectorY ), String( sectorX ) );
	const terrainPath = `${mapBase}.m`;
	const terrainTexturePath = `${mapBase}.t`;
	const objects2Path = `${mapBase}.o2`;
	const objectInfoPath = path.join( sourceExtractedRoot, "Map_extracted", "object.ifo" );
	const tileCatalogPath = path.join( sourceExtractedRoot, "Map_extracted", "tile2d.ifo" );
	const terrainCoverage = await filterExistingMapCoverage(
		deriveTitleTerrainSectorCoverage( options.titleManifest, {
			sectorX,
			sectorY,
			terrainSectorMargin: options.terrainSectorMargin
		} ),
		{
			sectorX,
			sectorY,
			sourceExtractedRoot,
			area
		}
	);

	const [terrainSectors, terrainTextureSectors, objectSectors, objectInfo, tileCatalog] = await Promise.all( [
		Promise.all(
			terrainCoverage.sectors.map( ( terrainSector ) =>
				readJmxMapTerrainSector(
					terrainSector.sectorX,
					terrainSector.sectorY,
					sourceExtractedRoot,
					sourceGameRoot
				)
			)
		),
		Promise.all(
			terrainCoverage.sectors.map( ( terrainSector ) =>
				readJmxMapTerrainTextureSector(
					terrainSector.sectorX,
					terrainSector.sectorY,
					sourceExtractedRoot,
					sourceGameRoot,
					area
				)
			)
		),
		Promise.all(
			terrainCoverage.sectors.map( ( objectSector ) =>
				readJmxMapObjectPlacementSector(
					objectSector.sectorX,
					objectSector.sectorY,
					sourceExtractedRoot,
					sourceGameRoot
				)
			)
		),
		options.objectInfo ?? readJmxMapObjectInfo( objectInfoPath ),
		options.tileCatalog ?? readJmxMapTileCatalog( tileCatalogPath )
	] );

	const seedTerrainSector = terrainSectors.find(
		( terrainSector ) => terrainSector.sectorX === sectorX && terrainSector.sectorY === sectorY
	);
	if ( !seedTerrainSector ) {
		throw new Error( `Derived terrain coverage did not include seed sector ${sectorY}/${sectorX}` );
	}

	const seedTerrainTextureSector = terrainTextureSectors.find(
		( textureSector ) => textureSector.sectorX === sectorX && textureSector.sectorY === sectorY
	);
	if ( !seedTerrainTextureSector ) {
		throw new Error( `Derived terrain texture coverage did not include seed sector ${sectorY}/${sectorX}` );
	}

	const seedObjectSector = objectSectors.find(
		( objectSector ) => objectSector.sectorX === sectorX && objectSector.sectorY === sectorY
	);
	if ( !seedObjectSector ) {
		throw new Error( `Derived object coverage did not include seed sector ${sectorY}/${sectorX}` );
	}

	const terrain = seedTerrainSector;
	const terrainTexture = seedTerrainTextureSector;
	const objects = mergeJmxMapObjectPlacementSectors( objectSectors );
	const missingObjectIds = objects.uniqueObjectIds.filter( ( objectId ) =>
		!objectInfo.entriesById[String( objectId )]
	);

	if ( missingObjectIds.length > 0 ) {
		throw new Error( `Missing object.ifo definitions for object IDs: ${missingObjectIds.join( ", " )}` );
	}

	const objectDefinitions = objects.uniqueObjectIds.map( ( objectId ) => {
		const definition = objectInfo.entriesById[String( objectId )];
		return {
			objectId,
			flags: definition.flags,
			sourcePath: definition.sourcePath
		};
	} );
	const referencedTerrainTiles = resolveReferencedTerrainTiles(
		collectTerrainTextureIds( terrainSectors ),
		tileCatalog,
		sourceExtractedRoot,
		sourceGameRoot
	);
	const skyTextures = options.skyResources ?? resolveSkyTextures();
	const waterTextures = options.waterResources ?? resolveWaterTextures();
	await copyReferencedTerrainTileImages( referencedTerrainTiles );
	if ( !options.sharedRenderResourcesPublicPath ) {
		await copyReferencedSkyImages( skyTextures );
		await copyReferencedWaterImages( waterTextures );
	}
	const objectResources = options.objectResources ??
		(await buildTitleSectorObjectResources( {
			area,
			extractedRoot: sourceExtractedRoot,
			gameRoot: sourceGameRoot,
			objectDefinitions,
			placements: objects.placements
		} ));

	// Navmesh: walkable terrain tiles + object-collision footprints for the seed region and
	// its loaded neighbours, in the bundle's base frame (relative to the source sector). The
	// intro crowd uses this for spawn placement and obstacle-aware patrol (so NPCs walk
	// around the fountain/statues/buildings exactly like the native navmesh-driven crowd).
	const navmesh = await buildRegionNavmeshData( {
		extractedRoot: sourceExtractedRoot,
		sourceSectorX: sectorX,
		sourceSectorY: sectorY,
		coverageSectors: terrainCoverage.sectors,
		objectResources
	} );

	return {
		format: "sro-world-region-bundle",
		version: 5,
		source: {
			area,
			sectorId: toHex16( sectorId ),
			sectorX,
			sectorY,
			terrain: toGameRelative( terrainPath, sourceGameRoot ),
			terrainTexture: toGameRelative( terrainTexturePath, sourceGameRoot ),
			terrainTileCatalog: toGameRelative( tileCatalogPath, sourceGameRoot ),
			objects2: toGameRelative( objects2Path, sourceGameRoot ),
			objectInfo: toGameRelative( objectInfoPath, sourceGameRoot ),
			reconstructionSources: [
				"SWorld_vFunc0_PreloadWorldNeighborhood",
				"sub_440460_MapLoader_LoadRegionTerrainAndO2Placements",
				"sub_440b64_MapLoader_TerrainTileTextureSelection",
				"sub_8bdf10_SWorld_PrepareCellTerrainTexture",
				"sub_899550_8cb770_MapSkyTextureAndRenderPath",
				"sub_899550_8ae2d0_MapWaterTextureAndRenderPath",
				"sub_4413b0_MapLoader_CreateObjectInstanceFromPlacement",
				"sub_443840_MapLoader_ResolveObjectResource",
				"JMXVRES0109_BSR_layout",
				"JMXVBMS0110_BMS_static_mesh_layout",
				"JMXVBMT0102_BMT_material_layout",
				"sub_4e1d10_4e1e20_CameraController_InsertSectorAdjustedKey",
				"SWorld_vFunc4_ObjectVisibilityCulling"
			]
		},
		terrain: {
			signature: terrain.signature,
			byteLength: terrain.byteLength,
			consumedBytes: terrain.consumedBytes,
			blockGrid: terrain.blockGrid,
			blockSizeBytes: terrain.blockSizeBytes,
			verticesPerBlockAxis: MAPM_VERTICES_PER_AXIS,
			tilesPerBlockAxis: MAPM_TILES_PER_AXIS,
			blockCount: terrain.blocks.length,
			blocks: terrain.blocks,
			sectorGrid: terrainCoverage.sectorGrid,
			sectorCount: terrainSectors.length,
			sectors: terrainSectors
		},
		terrainTextures: {
			signature: terrainTexture.signature,
			byteLength: terrainTexture.byteLength,
			consumedBytes: terrainTexture.consumedBytes,
			trailingByteLength: terrainTexture.trailingByteLength,
			lightmapPublicPath: terrainTexture.lightmapPublicPath,
			embeddedTexture: terrainTexture.embeddedTexture,
			blockGrid: terrainTexture.blockGrid,
			nativeLightByteCount: MAPT_NATIVE_LIGHT_BYTES,
			tilesPerBlockAxis: MAPM_TILES_PER_AXIS,
			blockCount: terrainTexture.blocks.length,
			blocks: terrainTexture.blocks,
			sectorCount: terrainTextureSectors.length,
			sectors: terrainTextureSectors,
			tileCatalog: {
				signature: tileCatalog.signature,
				sourcePath: toGameRelative( tileCatalogPath, sourceGameRoot ),
				declaredCount: tileCatalog.declaredCount,
				parsedCount: tileCatalog.entries.length,
				referencedTileCount: referencedTerrainTiles.length,
				referencedTiles: referencedTerrainTiles
			}
		},
		...(options.sharedRenderResourcesPublicPath ?
			{
				sharedRenderResourcesPublicPath: options.sharedRenderResourcesPublicPath
			} :
			{
				sky: skyTextures,
				water: {
					normalBlockCondition: {
						type: 0,
						nonzeroWaveType: true
					},
					normalBlockCount: countNormalWaterBlocks( terrainSectors ),
					normalFrameDurationMs: WATER_NORMAL_FRAME_DURATION_MS,
					normalFramePublicPaths: waterTextures.normalFramePublicPaths,
					reflectionBumpPublicPath: waterTextures.reflectionBumpPublicPath,
					specialTexturePublicPath: waterTextures.specialTexturePublicPath,
					waveTexturePublicPaths: waterTextures.waveTexturePublicPaths
				}
			}),
		objects: {
			signature: objects.signature,
			byteLength: seedObjectSector.byteLength,
			consumedBytes: seedObjectSector.consumedBytes,
			totalByteLength: objects.totalByteLength,
			totalConsumedBytes: objects.totalConsumedBytes,
			blockGrid: seedObjectSector.blockGrid,
			placementRecordBytes: objects.placementRecordBytes,
			lodGroupCount: MAPO2_LOD_GROUPS,
			slotCounts: objects.slotCounts,
			placementCount: objects.placements.length,
			uniqueObjectCount: objects.uniqueObjectIds.length,
			uniqueObjectIds: objects.uniqueObjectIds,
			regionIds: objects.regionIds,
			sectorCount: objectSectors.length,
			sectors: objectSectors,
			blocks: seedObjectSector.blocks,
			placements: objects.placements.map( ( placement ) => {
				const definition = objectInfo.entriesById[String( placement.objectId )];
				return {
					...placement,
					resourcePath: definition.sourcePath,
					objectFlags: definition.flags
				};
			} ),
			...(options.objectResourceIndexPublicPath ?
				{ resourceIndexPublicPath: options.objectResourceIndexPublicPath } :
				{ resources: objectResources })
		},
		objectInfo: {
			signature: objectInfo.signature,
			sourcePath: toGameRelative( objectInfoPath, sourceGameRoot ),
			declaredCount: objectInfo.declaredCount,
			parsedCount: objectInfo.entries.length,
			referencedDefinitions: objectDefinitions
		},
		navmesh
	};
}

/*
================
filterExistingMapCoverage
================
*/
async function filterExistingMapCoverage( terrainCoverage, options ) {
	const { sectorX: seedSectorX, sectorY: seedSectorY, sourceExtractedRoot, area } = options;
	const sectors = [];
	const missing = [];

	for ( const sector of terrainCoverage.sectors ) {
		if ( await hasRequiredMapSectorFiles( sector.sectorX, sector.sectorY, sourceExtractedRoot ) ) {
			sectors.push( sector );
		} else if ( sector.sectorX === seedSectorX && sector.sectorY === seedSectorY ) {
			throw new Error(
				`Seed map sector ${sector.sectorY}/${sector.sectorX} for ${area} is missing required map files`
			);
		} else {
			missing.push( sector );
		}
	}

	if ( missing.length > 0 ) {
		console.warn(
			`[world:${area}] skipped ${missing.length} missing optional map sector(s): ` +
				missing.map( ( sector ) => `${sector.sectorY}/${sector.sectorX}` ).join( ", " )
		);
	}

	return {
		...terrainCoverage,
		sectors,
		sectorGrid: rebuildSectorGrid( sectors, terrainCoverage.sectorGrid )
	};
}

/*
================
hasRequiredMapSectorFiles
================
*/
async function hasRequiredMapSectorFiles( sectorX, sectorY, sourceExtractedRoot ) {
	const mapBase = path.join( sourceExtractedRoot, "Map_extracted", String( sectorY ), String( sectorX ) );
	const requiredPaths = [ `${mapBase}.m`, `${mapBase}.t`, `${mapBase}.o2` ];
	const results = await Promise.all( requiredPaths.map( ( sourcePath ) => exists( sourcePath ) ) );

	return results.every( Boolean );
}

/*
================
rebuildSectorGrid
================
*/
function rebuildSectorGrid( sectors, previousGrid ) {
	if ( sectors.length === 0 ) {
		return {
			...previousGrid,
			minSectorX: 0,
			maxSectorX: 0,
			minSectorY: 0,
			maxSectorY: 0,
			width: 0,
			height: 0
		};
	}

	const minSectorX = Math.min( ...sectors.map( ( sector ) => sector.sectorX ) );
	const maxSectorX = Math.max( ...sectors.map( ( sector ) => sector.sectorX ) );
	const minSectorY = Math.min( ...sectors.map( ( sector ) => sector.sectorY ) );
	const maxSectorY = Math.max( ...sectors.map( ( sector ) => sector.sectorY ) );

	return {
		...previousGrid,
		minSectorX,
		maxSectorX,
		minSectorY,
		maxSectorY,
		width: maxSectorX - minSectorX + 1,
		height: maxSectorY - minSectorY + 1
	};
}

/*
================
readJmxMapObjectPlacementSector
================
*/
export async function readJmxMapObjectPlacementSector( sectorX, sectorY, sourceExtractedRoot, sourceGameRoot ) {
	const objects2Path = path.join( sourceExtractedRoot, "Map_extracted", String( sectorY ), `${sectorX}.o2` );
	const objects = parseJmxMapObjectPlacementO2( await readFile( objects2Path ), objects2Path );

	return {
		sectorId: toHex16( (sectorY << 8) | sectorX ),
		sectorX,
		sectorY,
		sourcePath: toGameRelative( objects2Path, sourceGameRoot ),
		signature: objects.signature,
		byteLength: objects.byteLength,
		consumedBytes: objects.consumedBytes,
		blockGrid: objects.blockGrid,
		placementRecordBytes: objects.placementRecordBytes,
		lodGroupCount: MAPO2_LOD_GROUPS,
		slotCounts: objects.slotCounts,
		placementCount: objects.placements.length,
		uniqueObjectCount: objects.uniqueObjectIds.length,
		uniqueObjectIds: objects.uniqueObjectIds,
		regionIds: objects.regionIds,
		blocks: objects.blocks,
		placements: objects.placements.map( ( placement ) => ({
			...placement,
			sourceSector: {
				sectorId: toHex16( (sectorY << 8) | sectorX ),
				sectorX,
				sectorY
			}
		}) )
	};
}

/*
================
mergeJmxMapObjectPlacementSectors
================
*/
function mergeJmxMapObjectPlacementSectors( objectSectors ) {
	const placements = [];
	const slotCounts = Array.from( { length: MAPO2_LOD_GROUPS }, () => 0 );
	const uniqueObjectIds = new Set();
	const regionIds = new Set();
	let totalByteLength = 0;
	let totalConsumedBytes = 0;

	for ( const objectSector of objectSectors ) {
		totalByteLength += objectSector.byteLength;
		totalConsumedBytes += objectSector.consumedBytes;

		for ( let index = 0; index < MAPO2_LOD_GROUPS; index += 1 ) {
			slotCounts[index] += objectSector.slotCounts[index] ?? 0;
		}

		for ( const objectId of objectSector.uniqueObjectIds ) {
			uniqueObjectIds.add( objectId );
		}

		for ( const regionId of objectSector.regionIds ) {
			regionIds.add( regionId );
		}

		for ( const placement of objectSector.placements ) {
			placements.push( {
				...placement,
				index: placements.length
			} );
		}
	}

	return {
		signature: "JMXVMAPO1001",
		totalByteLength,
		totalConsumedBytes,
		placementRecordBytes: MAPO2_PLACEMENT_BYTES,
		slotCounts,
		uniqueObjectIds: [ ...uniqueObjectIds ].sort( ( left, right ) => left - right ),
		regionIds: [ ...regionIds ].sort(),
		placements
	};
}

/*
================
readJmxMapTerrainSector
================
*/
export async function readJmxMapTerrainSector( sectorX, sectorY, sourceExtractedRoot, sourceGameRoot ) {
	const terrainPath = path.join( sourceExtractedRoot, "Map_extracted", String( sectorY ), `${sectorX}.m` );
	const terrain = parseJmxMapTerrain( await readFile( terrainPath ), terrainPath );

	return {
		sectorId: toHex16( (sectorY << 8) | sectorX ),
		sectorX,
		sectorY,
		sourcePath: toGameRelative( terrainPath, sourceGameRoot ),
		signature: terrain.signature,
		byteLength: terrain.byteLength,
		consumedBytes: terrain.consumedBytes,
		blockGrid: terrain.blockGrid,
		blockSizeBytes: terrain.blockSizeBytes,
		verticesPerBlockAxis: MAPM_VERTICES_PER_AXIS,
		tilesPerBlockAxis: MAPM_TILES_PER_AXIS,
		blockCount: terrain.blocks.length,
		blocks: terrain.blocks
	};
}

/*
================
collectTerrainTextureIds
================
*/
export function collectTerrainTextureIds( terrainSectors ) {
	const textureIds = new Set();

	for ( const terrainSector of terrainSectors ) {
		for ( const block of terrainSector.blocks ) {
			for ( const textureId of block.textureIds ) {
				textureIds.add( textureId );
			}
		}
	}

	return [ ...textureIds ].sort( ( left, right ) => left - right );
}

/*
================
countNormalWaterBlocks
================
*/
export function countNormalWaterBlocks( terrainSectors ) {
	return terrainSectors.reduce(
		( count, terrainSector ) =>
			count +
			terrainSector.blocks.filter( ( block ) => block.water.type === 0 && block.water.waveType !== 0 ).length,
		0
	);
}
