/*
===========================================================================

buildOutdoorWorldRegionResources.mjs - the outdoor world's region bundles and routing

Builds one bundle per outdoor sector (terrain, textures, objects, navmesh)
with the shared render and object stores, then publishes the region index
and catalog once every bundle exists. A region whose bundle already exists
is reused, and its terrain tiles are re-published from the terrain tile
ledger, so reuse never leaves a bundle naming images that are gone.

Reuse trusts only bundles this builder's current code wrote: the code stamp
(shared/codeStamp.mjs) covers this module and everything it imports, and a
stamp that does not match forces every region and shared index to rebuild.

===========================================================================
*/
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { buildJobs } from "../shared/buildParallelism.mjs";
import { codeHash, stampIsCurrent, writeStamp } from "../shared/codeStamp.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH, REGION_SIZE, WATER_NORMAL_FRAME_DURATION_MS } from "./constants.mjs";
import { copyReferencedSkyImages, resolveSkyTextures } from "./assets/copySkyImages.mjs";

import {
	copyReferencedTerrainTileImages,
	migrateCachedTerrainTileReferences,
	terrainTileReferencesCurrent
} from "./assets/copyTerrainTileImages.mjs";
import { copyReferencedWaterImages, resolveWaterTextures } from "./assets/copyWaterImages.mjs";
import { OUTDOOR_WORLD_REGION_CATALOG_PUBLIC_PATH, overlayWorldRegionCatalog } from "./buildWorldRegionCatalog.mjs";
import { publicPathToFile } from "../shared/assetPaths.mjs";
import { mapWithConcurrency } from "../shared/asyncUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exists, writeJson } from "./io.mjs";
import { parseJmxMapObjectPlacementO2, readJmxMapObjectInfo, readJmxMapTileCatalog } from "./jmx/index.mjs";
import { buildJmxWorldRegionBundle } from "./maploader/buildMapLoaderRegionBundle.mjs";
import { buildTitleSectorObjectResources } from "./objects/buildTitleSectorObjectResources.mjs";
import { extractedRoot, gameRoot, generatedRoot, normalizeRegionId, publicRoot, toHex16 } from "./paths.mjs";

export const OUTDOOR_WORLD_AREA = "outdoor";
export const OUTDOOR_WORLD_SOURCE_NAME = "mission-outdoor-global";
export const OUTDOOR_WORLD_INDEX_PUBLIC_PATH = "/assets/world/outdoor/world-regions.json";
export const OUTDOOR_WORLD_BUNDLE_PATH_TEMPLATE = "/assets/world/outdoor/regions/region-{id}.json";
export const OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH = "/assets/world/outdoor/object-resources.json";
export const OUTDOOR_WORLD_OBJECT_MESH_ROOT_PUBLIC_PATH = "/assets/world/outdoor/object-meshes";
export { OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH } from "./constants.mjs";

const OUTDOOR_WORLD_INDEX_PATH = publicPathToFile( OUTDOOR_WORLD_INDEX_PUBLIC_PATH, publicRoot );
const OUTDOOR_WORLD_OBJECT_INDEX_PATH = publicPathToFile( OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH, publicRoot );
const OUTDOOR_WORLD_SHARED_RENDER_PATH = publicPathToFile( OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH, publicRoot );
// Build cache, never published: the terrain tiles each region bundle
// references. A reused region re-publishes its tiles from here, so a bundle
// can never be "current" while the images it names are missing; a region
// this ledger does not know yet is read from its bundle once.
const TERRAIN_TILE_LEDGER_PATH = path.join( generatedRoot, "intermediate", "outdoor-terrain-tiles.json" );
// v4: each ledger tile records the reference its bundle names
// (imagePublicPath), so a hit is trusted only while every reference still
// matches the probe; an older ledger cannot tell and is discarded.
const TERRAIN_TILE_LEDGER_VERSION = 4;
// Names the code stamp of every outdoor reuse cache (regions, shared indexes).
const OUTDOOR_CODE_STAMP = "outdoor-world";

/*
================
readTerrainTileLedger
================
*/
export async function readTerrainTileLedger() {
	try {
		const ledger = JSON.parse( await readFile( TERRAIN_TILE_LEDGER_PATH, "utf8" ) );
		if ( ledger.version === TERRAIN_TILE_LEDGER_VERSION && ledger.regions ) {
			return new Map( Object.entries( ledger.regions ) );
		}
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	return new Map();
}

/*
================
writeTerrainTileLedger
================
*/
async function writeTerrainTileLedger( tilesByRegion ) {
	await mkdir( path.dirname( TERRAIN_TILE_LEDGER_PATH ), { recursive: true } );
	const regions = Object.fromEntries( [ ...tilesByRegion ].sort( ( [a], [b] ) => a.localeCompare( b ) ) );
	await writeFile( TERRAIN_TILE_LEDGER_PATH, JSON.stringify( { version: TERRAIN_TILE_LEDGER_VERSION, regions } ) );
}

/*
================
bundleTerrainTiles

The terrain tiles a region bundle references, reduced to what
copyReferencedTerrainTileImages reads: the ledger stays small.
================
*/
function bundleTerrainTiles( bundle ) {
	return (bundle.terrainTextures?.tileCatalog?.referencedTiles ?? []).map( ( tile ) => ({
		ddjFileName: tile.ddjFileName,
		sourcePath: tile.sourcePath,
		imagePublicPath: tile.imagePublicPath
	}) );
}

/*
================
refreshCachedTerrainTileBundle

Publish and validate migrated dependencies before changing the persisted
bundle. A failed texture publish leaves its previous references usable.
================
*/
export async function refreshCachedTerrainTileBundle( outputPath, sourceExtractedRoot ) {
	const bundle = JSON.parse( await readFile( outputPath, "utf8" ) );
	const migrated = await migrateCachedTerrainTileReferences( bundle, sourceExtractedRoot );
	const tiles = bundleTerrainTiles( bundle );
	await copyReferencedTerrainTileImages( tiles, sourceExtractedRoot );
	if ( migrated ) await writeCompactJson( outputPath, bundle );
	return tiles;
}

/*
================
discoverOutdoorWorldSectors
================
*/
/**
 * Discover exactly the retail outdoor sectors enabled by navmesh/mapinfo.mfo,
 * the availability bitmap consumed by CMapLoader_ReadMapInfoMfo (sub_43c7b0)
 * and MapLoader_CanLoadTerrainNvmRegion (sub_43f900). Every enabled bit must
 * have the complete MAPM/MAPT/MAPO2/JMXVNVM data plane.
 */
export async function discoverOutdoorWorldSectors( options = {} ) {
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const mapRoot = path.join( sourceExtractedRoot, "Map_extracted" );
	const navmeshRoot = path.join( sourceExtractedRoot, "Data_extracted", "navmesh" );
	const mapInfoPath = path.join( navmeshRoot, "mapinfo.mfo" );
	const mapInfo = await readFile( mapInfoPath );
	const availability = parseMapInfoAvailability( mapInfo, mapInfoPath );
	const sectors = [];

	for ( const regionId of availability.regionIds ) {
		const sectorX = regionId & 0xff;
		const sectorY = (regionId >> 8) & 0xff;
		const basePath = path.join( mapRoot, String( sectorY ), String( sectorX ) );
		const nvmPath = path.join( navmeshRoot, `nv_${regionId.toString( 16 ).padStart( 4, "0" )}.nvm` );
		const requiredPaths = [ `${basePath}.m`, `${basePath}.t`, `${basePath}.o2`, nvmPath ];
		const present = await Promise.all( requiredPaths.map( ( filePath ) => exists( filePath ) ) );
		const missing = requiredPaths.filter( ( _filePath, index ) => !present[index] );
		if ( missing.length > 0 ) {
			throw new Error(
				`Native mapinfo enables ${toHex16( regionId )}, but its extracted data plane is incomplete: ` +
					missing.join( ", " )
			);
		}

		sectors.push( {
			id: toHex16( regionId ),
			regionId,
			sectorX,
			sectorY,
			mapBasePath: basePath,
			navmeshPath: nvmPath
		} );
	}

	sectors.sort( compareSectors );
	assertUniqueSectors( sectors );
	return sectors;
}

/*
================
parseMapInfoAvailability
================
*/
function parseMapInfoAvailability( bytes, sourcePath ) {
	const signatureBytes = 12;
	const headerBytes = 12;
	const bitmapOffset = signatureBytes + headerBytes;
	if (
		bytes.length < bitmapOffset ||
		bytes.subarray( 0, signatureBytes ).toString( "ascii" ) !== "JMXVMFO 1000"
	) {
		throw new Error( `Invalid JMXVMFO availability resource: ${sourcePath}` );
	}

	const width = bytes.readUInt16LE( signatureBytes );
	const height = bytes.readUInt16LE( signatureBytes + 2 );
	if ( width !== 0x100 || height !== 0x80 ) {
		throw new Error( `Invalid JMXVMFO dimensions ${width}x${height}: ${sourcePath}` );
	}

	const bitCount = width * height;
	const bitmapBytes = Math.ceil( bitCount / 8 );
	if ( bytes.length < bitmapOffset + bitmapBytes ) {
		throw new Error( `Truncated JMXVMFO availability bitmap: ${sourcePath}` );
	}

	const regionIds = [];
	for ( let sectorY = 0; sectorY < height; sectorY += 1 ) {
		for ( let sectorX = 0; sectorX < width; sectorX += 1 ) {
			const regionId = (sectorY << 8) | sectorX;
			const byte = bytes[bitmapOffset + (regionId >> 3)];
			if ( (byte & (0x80 >> (regionId & 7))) !== 0 ) {
				regionIds.push( regionId );
			}
		}
	}
	return { width, height, regionIds };
}

/*
================
buildOutdoorWorldRegionIndexDescriptor
================
*/
export function buildOutdoorWorldRegionIndexDescriptor( sectors ) {
	const sorted = [ ...sectors ].sort( compareSectors );
	assertUniqueSectors( sorted );

	return {
		format: "sro-world-region-index",
		version: 2,
		area: OUTDOOR_WORLD_AREA,
		regionSize: REGION_SIZE,
		seedRegionId: "0x0000",
		seedSector: { sectorX: 0, sectorY: 0 },
		bundleLayout: "one-region-per-bundle",
		regions: sorted.map( ( sector ) => {
			const id = normalizeRegionId( sector.id ?? sector.regionId );
			return {
				id,
				sectorX: sector.sectorX,
				sectorY: sector.sectorY,
				seedRegionId: id,
				bundlePublicPath: outdoorRegionBundlePublicPath( id )
			};
		} )
	};
}

/*
================
loadOutdoorWorldRegionResourceGroup
================
*/
/**
 * Load the already-emitted outdoor index for inclusion in the normal catalog
 * build. The ordinary resource build never regenerates all outdoor regions.
 */
export async function loadOutdoorWorldRegionResourceGroup( options = {} ) {
	const indexPath = options.indexPath ?? OUTDOOR_WORLD_INDEX_PATH;
	if ( !(await exists( indexPath )) ) {
		return undefined;
	}

	const descriptor = JSON.parse( await readFile( indexPath, "utf8" ) );
	validateOutdoorIndex( descriptor );
	const missingBundlePaths = await findMissingBundlePaths(
		descriptor.regions,
		normalizeJobs( options.jobs ),
		options.publicRoot
	);
	if ( missingBundlePaths.length > 0 && !options.allowDevOnDemand ) {
		throw new Error(
			`${OUTDOOR_WORLD_INDEX_PUBLIC_PATH} advertises ${descriptor.regions.length} outdoor regions, ` +
				`but ${missingBundlePaths.length} bundle(s) are missing. Run "pnpm assets build world-outdoor" ` +
				`before a production resource build; Vite's dev-on-demand routing index is not a production artifact.`
		);
	}
	return {
		regionIndexDescriptor: descriptor,
		worldRegionsPublicPath: OUTDOOR_WORLD_INDEX_PUBLIC_PATH,
		sourceName: OUTDOOR_WORLD_SOURCE_NAME,
		incompleteBundleCount: missingBundlePaths.length
	};
}

/*
================
publishOutdoorWorldRegionRouting
================
*/
/**
 * Publish the global routing plane. Production callers require every bundle to
 * exist first; the Vite dev adapter may opt into an on-demand backing source.
 */
export async function publishOutdoorWorldRegionRouting( options = {} ) {
	const indexPath = options.indexPath ?? OUTDOOR_WORLD_INDEX_PATH;
	const outputPublicRoot = options.publicRoot ?? publicRoot;
	const sectors = options.sectors ??
		(await discoverOutdoorWorldSectors( { extractedRoot: options.extractedRoot ?? extractedRoot } ));
	const descriptor = options.descriptor ?? buildOutdoorWorldRegionIndexDescriptor( sectors );
	const missingBundlePaths = await findMissingBundlePaths(
		descriptor.regions,
		normalizeJobs( options.jobs ),
		outputPublicRoot
	);
	if ( missingBundlePaths.length > 0 && !options.allowMissingBundles ) {
		throw new Error(
			`Cannot publish outdoor routing: ${missingBundlePaths.length} region bundle(s) are missing`
		);
	}

	const publishedDescriptor = {
		...descriptor,
		deliveryMode: missingBundlePaths.length === 0 ? "prebuilt" : "dev-on-demand"
	};
	await writeJson( indexPath, publishedDescriptor );
	// The pack/runtime path consumes the precompressed JSON member, not the
	// loose file. Publishing one schema generation without its sidecars can
	// therefore make the browser observe an older contract than every source
	// and loose-artifact check sees. Keep all representations one atomic build
	// generation before any catalog or pack refresh can advertise the index.
	await refreshPrecompressedSidecars( [ indexPath ], { onlyWhenStale: true } );
	const catalog = options.updateCatalog === false ?
		undefined :
		await overlayWorldRegionCatalog(
			[
				{
					regionIndexDescriptor: publishedDescriptor,
					worldRegionsPublicPath: OUTDOOR_WORLD_INDEX_PUBLIC_PATH,
					sourceName: OUTDOOR_WORLD_SOURCE_NAME
				}
			],
			{
				replaceSources: [ OUTDOOR_WORLD_SOURCE_NAME ],
				// Keep mutable outdoor routing in the outdoor pack ownership
				// slice, rather than invalidating the startup game-data packs.
				mirrorPublicPaths: [ OUTDOOR_WORLD_REGION_CATALOG_PUBLIC_PATH ]
			}
		);

	return {
		sectors,
		descriptor: publishedDescriptor,
		missingBundlePaths,
		catalog,
		onDemand: missingBundlePaths.length > 0
	};
}

/*
================
buildOutdoorWorldRegionResources
================
*/
export async function buildOutdoorWorldRegionResources( options = {} ) {
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const sourceGameRoot = options.gameRoot ?? gameRoot;
	const jobs = normalizeJobs( options.jobs );
	const sectors = options.sectors ?? (await discoverOutdoorWorldSectors( { extractedRoot: sourceExtractedRoot } ));
	const descriptor = buildOutdoorWorldRegionIndexDescriptor( sectors );
	const selectedSectors = selectRequestedSectors( sectors, options.regionIds );

	if ( options.planOnly ) {
		return {
			planOnly: true,
			sectorCount: sectors.length,
			selectedSectorCount: selectedSectors.length,
			descriptor,
			selectedRegions: selectedSectors.map( ( sector ) => sector.id )
		};
	}

	// Output from older builder code is never reused; a partial run (regionIds)
	// rebuilds what it selects and leaves the stamp stale for the next full run.
	const builderHash = await codeHash( import.meta.url );
	const codeCurrent = await stampIsCurrent( OUTDOOR_CODE_STAMP, builderHash );
	const force = Boolean( options.force ) || !codeCurrent;
	const forceShared = force || Boolean( options.forceShared );

	const mapRoot = path.join( sourceExtractedRoot, "Map_extracted" );
	const [objectInfo, tileCatalog, sharedRender, sharedObjects] = await Promise.all( [
		readJmxMapObjectInfo( path.join( mapRoot, "object.ifo" ) ),
		readJmxMapTileCatalog( path.join( mapRoot, "tile2d.ifo" ) ),
		buildOutdoorSharedRenderResources( { force: forceShared } ),
		buildOutdoorSharedObjectResources( {
			sectors,
			extractedRoot: sourceExtractedRoot,
			gameRoot: sourceGameRoot,
			jobs,
			force: forceShared
		} )
	] );

	let built = 0;
	let reused = 0;
	const tilesByRegion = await readTerrainTileLedger();
	await mapWithConcurrency( selectedSectors, jobs, async ( sector, index ) => {
		const publicPath = outdoorRegionBundlePublicPath( sector.id );
		const outputPath = publicPathToFile( publicPath, publicRoot );
		if ( !force && (await exists( outputPath )) ) {
			// Reuse keeps the bundle, not a promise that its images still exist.
			let tiles = tilesByRegion.get( String( sector.id ) );
			// A ledger hit names the references the bundle held when it was
			// recorded; when the probe's answer moved since, re-read and migrate
			// the bundle rather than publish under a reference it does not hold.
			if ( tiles && !(await terrainTileReferencesCurrent( tiles, sourceExtractedRoot )) ) {
				tiles = undefined;
			}
			if ( !tiles ) {
				tiles = await refreshCachedTerrainTileBundle( outputPath, sourceExtractedRoot );
				tilesByRegion.set( String( sector.id ), tiles );
			} else {
				await copyReferencedTerrainTileImages( tiles, sourceExtractedRoot );
			}
			// A kept bundle claims itself; the ledger claims what it references (lightmaps).
			claimPublicFile( outputPath );
			reused += 1;
			reportProgress( options, {
				phase: "regions",
				completed: index + 1,
				total: selectedSectors.length,
				regionId: sector.id,
				status: "reused"
			} );
			return;
		}

		const bundle = await buildJmxWorldRegionBundle( {
			area: OUTDOOR_WORLD_AREA,
			sectorId: sector.regionId,
			sectorX: sector.sectorX,
			sectorY: sector.sectorY,
			terrainSectorMargin: 0,
			extractedRoot: sourceExtractedRoot,
			gameRoot: sourceGameRoot,
			objectInfo,
			tileCatalog,
			objectResources: sharedObjects.collisionResources,
			objectResourceIndexPublicPath: OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
			sharedRenderResourcesPublicPath: OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
			skyResources: sharedRender.sky,
			waterResources: sharedRender.water
		} );
		assertIndependentOutdoorBundle( bundle, sector );
		await writeCompactJson( outputPath, bundle );
		tilesByRegion.set( String( sector.id ), bundleTerrainTiles( bundle ) );
		built += 1;
		reportProgress( options, {
			phase: "regions",
			completed: index + 1,
			total: selectedSectors.length,
			regionId: sector.id,
			status: "built"
		} );
	} );

	await writeTerrainTileLedger( tilesByRegion );
	const missingBundlePaths = await findMissingBundlePaths( descriptor.regions, jobs );
	let published = false;
	let catalog;
	if ( missingBundlePaths.length === 0 ) {
		const routing = await publishOutdoorWorldRegionRouting( {
			sectors,
			descriptor,
			jobs,
			updateCatalog: options.updateCatalog
		} );
		catalog = routing.catalog;
		published = true;
	}
	if ( !codeCurrent && published && selectedSectors.length === sectors.length ) {
		await writeStamp( OUTDOOR_CODE_STAMP, builderHash );
	}

	return {
		planOnly: false,
		sectorCount: sectors.length,
		selectedSectorCount: selectedSectors.length,
		built,
		reused,
		jobs,
		published,
		missingBundlePaths,
		descriptor,
		sharedObjectIndex: {
			publicPath: OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
			bsrCount: sharedObjects.index.bsrCount,
			materialSetCount: sharedObjects.index.materialSetCount,
			meshCount: sharedObjects.index.meshCount,
			textureCount: sharedObjects.index.textureCount
		},
		sharedRenderPublicPath: OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
		catalog
	};
}

/*
================
buildOutdoorSharedRenderResources
================
*/
async function buildOutdoorSharedRenderResources( options = {} ) {
	if ( !options.force && (await exists( OUTDOOR_WORLD_SHARED_RENDER_PATH )) ) {
		const existing = JSON.parse( await readFile( OUTDOOR_WORLD_SHARED_RENDER_PATH, "utf8" ) );
		validateSharedRenderResources( existing );
		claimPublicFile( OUTDOOR_WORLD_SHARED_RENDER_PATH );
		return existing;
	}

	const sky = resolveSkyTextures();
	const waterTextures = resolveWaterTextures();
	await Promise.all( [ copyReferencedSkyImages( sky ), copyReferencedWaterImages( waterTextures ) ] );
	const shared = {
		format: "sro-world-shared-render-resources",
		version: 1,
		sky,
		water: {
			normalBlockCondition: {
				type: 0,
				nonzeroWaveType: true
			},
			normalFrameDurationMs: WATER_NORMAL_FRAME_DURATION_MS,
			normalFramePublicPaths: waterTextures.normalFramePublicPaths,
			reflectionBumpPublicPath: waterTextures.reflectionBumpPublicPath,
			specialTexturePublicPath: waterTextures.specialTexturePublicPath,
			waveTexturePublicPaths: waterTextures.waveTexturePublicPaths
		}
	};
	await writeJson( OUTDOOR_WORLD_SHARED_RENDER_PATH, shared );
	return shared;
}

/*
================
buildOutdoorSharedObjectResources
================
*/
async function buildOutdoorSharedObjectResources( options ) {
	if ( !options.force && (await exists( OUTDOOR_WORLD_OBJECT_INDEX_PATH )) ) {
		const index = JSON.parse( await readFile( OUTDOOR_WORLD_OBJECT_INDEX_PATH, "utf8" ) );
		validateObjectResourceIndex( index );
		claimPublicFile( OUTDOOR_WORLD_OBJECT_INDEX_PATH );
		return {
			index,
			collisionResources: collisionResourcesFromIndex( index )
		};
	}

	const objectInfoPath = path.join( options.extractedRoot, "Map_extracted", "object.ifo" );
	const objectInfo = await readJmxMapObjectInfo( objectInfoPath );
	const usage = await collectOutdoorObjectUsage( options.sectors, {
		extractedRoot: options.extractedRoot,
		jobs: options.jobs
	} );
	const missingObjectIds = usage.objectIds.filter(
		( objectId ) => !objectInfo.entriesById[String( objectId )]
	);
	if ( missingObjectIds.length > 0 ) {
		throw new Error(
			`Outdoor MAPO2 sectors reference missing object.ifo ids: ${missingObjectIds.join( ", " )}`
		);
	}

	const objectDefinitions = usage.objectIds.map( ( objectId ) => {
		const entry = objectInfo.entriesById[String( objectId )];
		return {
			objectId,
			flags: entry.flags,
			sourcePath: entry.sourcePath
		};
	} );
	const resources = await buildTitleSectorObjectResources( {
		area: OUTDOOR_WORLD_AREA,
		extractedRoot: options.extractedRoot,
		gameRoot: options.gameRoot,
		objectDefinitions,
		placements: [],
		placementCountsByObjectId: usage.placementCountsByObjectId
	} );

	const meshes = resources.meshes;
	const collisionMeshes = [];
	const meshFiles = [];
	for ( let index = 0; index < meshes.length; index += 1 ) {
		const mesh = meshes[index];
		const wrapper = {
			format: "sro-world-object-mesh-resource",
			version: 1,
			mesh
		};
		const bytes = Buffer.from( `${JSON.stringify( wrapper )}\n`, "utf8" );
		const sha256 = sha256Hex( bytes );
		const publicPath = `${OUTDOOR_WORLD_OBJECT_MESH_ROOT_PUBLIC_PATH}/${sha256}.json`;
		const outputPath = publicPathToFile( publicPath, publicRoot );
		if ( options.force || !(await exists( outputPath )) ) {
			await mkdir( path.dirname( outputPath ), { recursive: true } );
			await writeFile( outputPath, bytes );
		}
		claimPublicFile( outputPath );

		meshFiles.push( {
			sourcePath: mesh.sourcePath,
			publicPath,
			sha256,
			byteLength: mesh.byteLength,
			vertexCount: mesh.vertexCount,
			triangleCount: mesh.triangleCount,
			bounds: mesh.bounds
		} );
		collisionMeshes.push( {
			sourcePath: mesh.sourcePath,
			bounds: mesh.bounds
		} );
		// Deliberate memory release once the mesh is serialized; the JSDoc cast keeps the
		// array's element type intact for the reads above (the loop never revisits an index).
		meshes[index] = /** @type {any} */ (undefined);
	}

	const { meshes: _discardedMeshes, ...resourceIndexFields } = resources;
	const index = {
		...resourceIndexFields,
		format: "sro-world-object-resource-index",
		version: 1,
		meshFiles
	};
	await writeJson( OUTDOOR_WORLD_OBJECT_INDEX_PATH, index );

	return {
		index,
		collisionResources: {
			...resources,
			meshes: collisionMeshes
		}
	};
}

/*
================
collectOutdoorObjectUsage
================
*/
async function collectOutdoorObjectUsage( sectors, options ) {
	const objectIds = new Set();
	const placementCountsByObjectId = new Map();

	await mapWithConcurrency( sectors, options.jobs, async ( sector ) => {
		const o2Path = path.join(
			options.extractedRoot,
			"Map_extracted",
			String( sector.sectorY ),
			`${sector.sectorX}.o2`
		);
		const placements = parseJmxMapObjectPlacementO2( await readFile( o2Path ), o2Path );
		for ( const placement of placements.placements ) {
			objectIds.add( placement.objectId );
			placementCountsByObjectId.set(
				placement.objectId,
				(placementCountsByObjectId.get( placement.objectId ) ?? 0) + 1
			);
		}
	} );

	return {
		objectIds: [ ...objectIds ].sort( ( left, right ) => left - right ),
		placementCountsByObjectId
	};
}

/*
================
collisionResourcesFromIndex
================
*/
function collisionResourcesFromIndex( index ) {
	return {
		...index,
		format: "sro-title-sector-object-resources",
		meshes: index.meshFiles.map( ( mesh ) => ({
			sourcePath: mesh.sourcePath,
			bounds: mesh.bounds
		}) )
	};
}

/*
================
selectRequestedSectors
================
*/
function selectRequestedSectors( sectors, requestedIds ) {
	if ( !requestedIds || requestedIds.length === 0 ) {
		return sectors;
	}

	const requested = new Set( requestedIds.map( normalizeRegionId ) );
	const selected = sectors.filter( ( sector ) => requested.has( normalizeRegionId( sector.id ) ) );
	const found = new Set( selected.map( ( sector ) => normalizeRegionId( sector.id ) ) );
	const missing = [ ...requested ].filter( ( id ) => !found.has( id ) );
	if ( missing.length > 0 ) {
		throw new Error( `Requested outdoor region(s) are not complete extracted sectors: ${missing.join( ", " )}` );
	}
	return selected;
}

/*
================
findMissingBundlePaths
================
*/
async function findMissingBundlePaths( regions, jobs, outputPublicRoot = publicRoot ) {
	const missing = [];
	await mapWithConcurrency( regions, jobs, async ( region ) => {
		if ( !(await exists( publicPathToFile( region.bundlePublicPath, outputPublicRoot ) )) ) {
			missing.push( region.bundlePublicPath );
		}
	} );
	return missing.sort();
}

/*
================
assertIndependentOutdoorBundle
================
*/
function assertIndependentOutdoorBundle( bundle, sector ) {
	const expectedId = normalizeRegionId( sector.id );
	if ( normalizeRegionId( bundle.source.sectorId ) !== expectedId ) {
		throw new Error(
			`Outdoor ${expectedId} emitted source frame ${bundle.source.sectorId}; every split bundle must own its region frame`
		);
	}
	if ( bundle.terrain.sectorCount !== 1 || bundle.terrain.sectors?.length !== 1 ) {
		throw new Error(
			`Outdoor ${expectedId} emitted ${
				bundle.terrain.sectorCount ?? bundle.terrain.sectors?.length ?? 0
			} terrain sectors; expected one`
		);
	}
	if (
		bundle.sharedRenderResourcesPublicPath !== OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH ||
		bundle.objects.resourceIndexPublicPath !== OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH ||
		bundle.objects.resources
	) {
		throw new Error( `Outdoor ${expectedId} did not emit the split shared-resource contract` );
	}
}

/*
================
validateOutdoorIndex
================
*/
function validateOutdoorIndex( descriptor ) {
	if (
		descriptor?.format !== "sro-world-region-index" ||
		descriptor.version !== 2 ||
		descriptor.area !== OUTDOOR_WORLD_AREA ||
		descriptor.bundleLayout !== "one-region-per-bundle" ||
		![ "prebuilt", "dev-on-demand" ].includes( descriptor.deliveryMode ) ||
		!Array.isArray( descriptor.regions )
	) {
		throw new Error( `${OUTDOOR_WORLD_INDEX_PUBLIC_PATH} is not a split outdoor region index` );
	}
	for ( const region of descriptor.regions ) {
		if (
			normalizeRegionId( region.seedRegionId ) !== normalizeRegionId( region.id ) ||
			region.bundlePublicPath !== outdoorRegionBundlePublicPath( region.id )
		) {
			throw new Error( `Outdoor index entry ${region.id} does not own an independent bundle/source frame` );
		}
	}
}

/*
================
validateSharedRenderResources
================
*/
function validateSharedRenderResources( resources ) {
	if (
		resources?.format !== "sro-world-shared-render-resources" ||
		!resources.sky ||
		!resources.water
	) {
		throw new Error( `${OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH} has an invalid format` );
	}
}

/*
================
validateObjectResourceIndex
================
*/
function validateObjectResourceIndex( index ) {
	if (
		index?.format !== "sro-world-object-resource-index" ||
		!Array.isArray( index.bsr ) ||
		!Array.isArray( index.materialSets ) ||
		!Array.isArray( index.meshFiles ) ||
		index.meshFiles.some(
			( mesh ) =>
				typeof mesh.sourcePath !== "string" ||
				typeof mesh.publicPath !== "string" ||
				!/^[0-9a-f]{64}$/.test( mesh.sha256 ?? "" ) ||
				!Object.hasOwn( mesh, "bounds" )
		)
	) {
		throw new Error( `${OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH} has an invalid format` );
	}
}

/*
================
outdoorRegionBundlePublicPath
================
*/
export function outdoorRegionBundlePublicPath( id ) {
	const hex = normalizeRegionId( id ).slice( 2 );
	return OUTDOOR_WORLD_BUNDLE_PATH_TEMPLATE.replace( "{id}", hex );
}

/*
================
writeCompactJson
================
*/
async function writeCompactJson( outputPath, value ) {
	await mkdir( path.dirname( outputPath ), { recursive: true } );
	await writeFile( outputPath, `${JSON.stringify( value )}\n`, "utf8" );
	claimPublicFile( outputPath );
}

/*
================
reportProgress
================
*/
function reportProgress( options, progress ) {
	if ( typeof options.onProgress === "function" ) {
		options.onProgress( progress );
		return;
	}
	if (
		progress.completed === progress.total ||
		progress.completed === 1 ||
		progress.completed % 25 === 0
	) {
		console.log(
			`[world:outdoor] ${progress.completed}/${progress.total} ${progress.status} ${progress.regionId}`
		);
	}
}

/*
================
normalizeJobs
================
*/
function normalizeJobs( value ) {
	if ( value === undefined ) return buildJobs();
	const parsed = Number( value );
	if ( !Number.isInteger( parsed ) || parsed < 1 ) {
		throw new Error( `Outdoor region build jobs must be a positive integer; got ${value}` );
	}
	return parsed;
}

/*
================
compareSectors
================
*/
function compareSectors( left, right ) {
	return left.sectorY - right.sectorY || left.sectorX - right.sectorX;
}

/*
================
assertUniqueSectors
================
*/
function assertUniqueSectors( sectors ) {
	const ids = new Set();
	for ( const sector of sectors ) {
		const id = normalizeRegionId( sector.id ?? sector.regionId );
		if ( !isSectorByte( sector.sectorX ) || !isSectorByte( sector.sectorY ) ) {
			throw new Error( `Outdoor ${id} has out-of-range sector coordinates` );
		}
		if ( ids.has( id ) ) {
			throw new Error( `Outdoor discovery repeated ${id}` );
		}
		ids.add( id );
	}
}

/*
================
isSectorByte
================
*/
function isSectorByte( value ) {
	return Number.isInteger( value ) && value >= 0 && value <= 0xff;
}
