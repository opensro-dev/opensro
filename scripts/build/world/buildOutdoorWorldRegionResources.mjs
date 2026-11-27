/*
===========================================================================

buildOutdoorWorldRegionResources.mjs - the outdoor world's region bundles and routing

Builds one bundle per outdoor sector (terrain, textures, objects, navmesh)
with the shared render and object stores (outdoorSharedStores.mjs), then
publishes the region index and catalog once every bundle exists. A region whose bundle already exists
is reused, and its terrain tiles are re-published from the terrain tile
ledger, so reuse never leaves a bundle naming images that are gone.

Reuse trusts only bundles this builder's current code wrote: the code stamp
(shared/codeStamp.mjs) covers this module and everything it imports, and a
stamp that does not match forces every region and shared index to rebuild.
Code is not the whole of it: a region is reused only while the digest of its
inputs (its sector's .m/.t/.o2 and navmesh, plus the shared catalogues its
object ids resolve through) matches the one recorded when it was built. An
import that renumbers a sector's objects (Baghdad's, 11-16) rebuilds that
sector; without the digest the old bundle named ids the new index lacked
("Missing navigation BSR 2631"). The resource build refuses stale bundles.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { buildJobs } from "../shared/buildParallelism.mjs";
import { codeHash, invalidateStamp, stampIsCurrent, writeStamp } from "../shared/codeStamp.mjs";
import { claimKeptOutput, detachedString, namedPublicPaths } from "../shared/publicationLedger.mjs";
import {
	OUTDOOR_WORLD_AREA,
	OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
	REGION_SIZE
} from "./constants.mjs";
import { buildOutdoorSharedObjectResources, buildOutdoorSharedRenderResources } from "./outdoorSharedStores.mjs";
import {
	findStaleOutdoorRegions,
	outdoorInputSetKey,
	outdoorRegionInputDigest,
	outdoorRegionInputSignature,
	outdoorSharedInputDigest,
	readRegionLedger,
	writeTerrainTileLedger
} from "./outdoorRegionLedger.mjs";
import {
	copyReferencedTerrainTileImages,
	migrateCachedTerrainTileReferences,
	terrainTileReferencesCurrent
} from "./assets/copyTerrainTileImages.mjs";
import { OUTDOOR_WORLD_REGION_CATALOG_PUBLIC_PATH, overlayWorldRegionCatalog } from "./buildWorldRegionCatalog.mjs";
import { normalizeAssetPath, publicPathToFile } from "../shared/assetPaths.mjs";
import { mapWithConcurrency } from "../shared/asyncUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exists, writeCompactJson, writeJson } from "./io.mjs";

import { readJmxMapObjectInfo, readJmxMapTileCatalog } from "./jmx/index.mjs";
import { buildJmxWorldRegionBundle } from "./maploader/buildMapLoaderRegionBundle.mjs";
import { WORLD_LAYER_TABLE } from "./maploader/worldLayers.mjs";
import { extractedRoot, gameRoot, normalizeRegionId, publicRoot, toHex16 } from "./paths.mjs";

export const OUTDOOR_WORLD_SOURCE_NAME = "mission-outdoor-global";
export const OUTDOOR_WORLD_INDEX_PUBLIC_PATH = "/assets/world/outdoor/world-regions.json";
export const OUTDOOR_WORLD_BUNDLE_PATH_TEMPLATE = "/assets/world/outdoor/regions/region-{id}.json";
export {
	OUTDOOR_WORLD_AREA,
	OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH
} from "./constants.mjs";
export {
	findStaleOutdoorRegions,
	outdoorRegionInputDigest,
	outdoorSharedInputDigest,
	readTerrainTileLedger
} from "./outdoorRegionLedger.mjs";
export { outdoorObjectIndexMatchesDefinitions } from "./outdoorSharedStores.mjs";

const OUTDOOR_WORLD_INDEX_PATH = publicPathToFile( OUTDOOR_WORLD_INDEX_PUBLIC_PATH, publicRoot );
// Names the code stamp of every outdoor reuse cache (regions, shared indexes).
const OUTDOOR_CODE_STAMP = "outdoor-world";

/*
================
bundleTerrainTiles

The terrain tiles a region bundle references, reduced to what
copyReferencedTerrainTileImages reads: the ledger stays small. The kept
strings are detached from a parsed bundle's text (detachedString).
================
*/
function bundleTerrainTiles( bundle ) {
	const kept = value => typeof value === "string" ? detachedString( value ) : value;
	return (bundle.terrainTextures?.tileCatalog?.referencedTiles ?? []).map( ( tile ) => ({
		ddjFileName: kept( tile.ddjFileName ),
		sourcePath: kept( tile.sourcePath ),
		imagePublicPath: kept( tile.imagePublicPath )
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
 * the availability bitmap consumed by CMapLoader_ReadMapInfoMfo
 * and MapLoader_CanLoadTerrainNvmRegion (CMapLoader_TerrainNavMeshExists). Every enabled bit must
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
	if ( !options.allowDevOnDemand ) {
		// extractedRoot and sectors default to the variant's; a test hands in
		// its own fixture tree, as findStaleOutdoorRegions takes them.
		const sectors = options.sectors ??
			(await discoverOutdoorWorldSectors( { extractedRoot: options.extractedRoot } ));
		const stale = await findStaleOutdoorRegions( {
			extractedRoot: options.extractedRoot,
			sectors,
			jobs: normalizeJobs( options.jobs ),
			publicRoot: options.publicRoot,
			ledgerPath: options.ledgerPath
		} );
		if ( stale.length > 0 ) {
			throw new Error(
				`${stale.length} outdoor region bundle(s) were built from other inputs than the variant holds ` +
					`(first: ${stale.slice( 0, 5 ).join( ", " )}). Run "pnpm assets build world-outdoor" ` +
					`(or "pnpm assets build full") before a production resource build.`
			);
		}
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
	// The seasonal layer table (worldLayers.mjs) changes the bundles' layer
	// tags and the .o2 files beside it without touching any code, so its
	// bytes join the stamp: a new or removed layer import rebuilds every region.
	const layerTable = await readFile( path.join( sourceExtractedRoot, "Map_extracted", WORLD_LAYER_TABLE ) ).catch(
		error => {
			if ( error.code === "ENOENT" ) return Buffer.alloc( 0 );
			throw error;
		}
	);
	const builderHash = sha256Hex( `${await codeHash( import.meta.url )}:${sha256Hex( layerTable )}` );
	const codeCurrent = await stampIsCurrent( OUTDOOR_CODE_STAMP, builderHash );
	// This run writes with other code: the old stamp must not survive it.
	if ( !codeCurrent ) await invalidateStamp( OUTDOOR_CODE_STAMP );
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
	const {
		tiles: tilesByRegion,
		references: referencesByRegion,
		inputs: inputsByRegion,
		inputStats: inputStatsByRegion,
		sharedDigest: ledgerSharedDigest
	} = await readRegionLedger();
	// After the shared stores above: their published bytes join every digest.
	const sharedDigest = await outdoorSharedInputDigest( sourceExtractedRoot, publicRoot );
	// This run's signatures, the whole-set key's input: the ledger map may
	// still name sectors discovery no longer finds, the key must not.
	const runSignatures = new Map();
	await mapWithConcurrency( selectedSectors, jobs, async ( sector, index ) => {
		const publicPath = outdoorRegionBundlePublicPath( sector.id );
		const outputPath = publicPathToFile( publicPath, publicRoot );
		// A region whose input files kept their size and mtime since its
		// digest was recorded is not re-read or re-hashed: the recorded
		// digest stands, while the shared digest it was folded under stands.
		const recordedSignature = inputStatsByRegion.get( String( sector.id ) );
		const signature = await outdoorRegionInputSignature( sector );
		inputStatsByRegion.set( String( sector.id ), signature );
		runSignatures.set( String( sector.id ), signature );
		const inputDigest = !force && ledgerSharedDigest === sharedDigest &&
				signature === recordedSignature && inputsByRegion.has( String( sector.id ) ) ?
			inputsByRegion.get( String( sector.id ) ) :
			await outdoorRegionInputDigest( sector, sharedDigest );
		if ( !force && inputsByRegion.get( String( sector.id ) ) === inputDigest && (await exists( outputPath )) ) {
			// Reuse keeps the bundle, not a promise that its images still exist.
			let tiles = tilesByRegion.get( String( sector.id ) );
			// A ledger hit names the references the bundle held when it was
			// recorded; when the probe's answer moved since, re-read and migrate
			// the bundle rather than publish under a reference it does not hold.
			if ( tiles && !(await terrainTileReferencesCurrent( tiles, sourceExtractedRoot )) ) {
				tiles = undefined;
			}
			let references = referencesByRegion.get( String( sector.id ) );
			if ( !tiles ) {
				tiles = await refreshCachedTerrainTileBundle( outputPath, sourceExtractedRoot );
				tilesByRegion.set( String( sector.id ), tiles );
				// A migration may have rewritten the bundle: re-read what it names.
				references = undefined;
			} else {
				await copyReferencedTerrainTileImages( tiles, sourceExtractedRoot );
			}
			if ( !references ) {
				references = namedPublicPaths( await readFile( outputPath, "utf8" ) );
				referencesByRegion.set( String( sector.id ), references );
			}
			// A kept bundle claims itself and what it names (its lightmaps).
			await claimKeptOutput( outputPath, references );
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
		const text = await writeCompactJson( outputPath, bundle );
		tilesByRegion.set( String( sector.id ), bundleTerrainTiles( bundle ) );
		referencesByRegion.set( String( sector.id ), namedPublicPaths( text ) );
		inputsByRegion.set( String( sector.id ), inputDigest );
		built += 1;
		reportProgress( options, {
			phase: "regions",
			completed: index + 1,
			total: selectedSectors.length,
			regionId: sector.id,
			status: "built"
		} );
	} );

	// The whole-set key vouches for the input set only when this run saw
	// every sector (the same gate as the code stamp below); a partial run
	// refreshes the signatures it touched and leaves the key unwritten. The
	// recorded shared digest stands only while every recorded input digest
	// was computed under it: a partial run under a moved shared digest
	// leaves the set mixed and records none.
	const fullRun = selectedSectors.length === sectors.length;
	await writeTerrainTileLedger( tilesByRegion, referencesByRegion, inputsByRegion, {
		inputStats: inputStatsByRegion,
		setKey: fullRun ? outdoorInputSetKey( sharedDigest, runSignatures ) : null,
		sharedDigest: fullRun || ledgerSharedDigest === sharedDigest ? sharedDigest : null
	} );
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
	if ( bundle.terrain.sectorCount !== 1 || bundle.terrainTextures.sectorCount !== 1 ) {
		throw new Error(
			`Outdoor ${expectedId} emitted ${bundle.terrain.sectorCount} terrain sectors; expected one`
		);
	}
	// One sector travels once, in the top-level blocks (sectorList in
	// buildMapLoaderRegionBundle.mjs).
	if ( bundle.terrain.sectors || bundle.terrainTextures.sectors ) {
		throw new Error( `Outdoor ${expectedId} repeats its terrain in a sector list` );
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
outdoorRegionBundlePublicPath
================
*/
export function outdoorRegionBundlePublicPath( id ) {
	const hex = normalizeRegionId( id ).slice( 2 );
	return OUTDOOR_WORLD_BUNDLE_PATH_TEMPLATE.replace( "{id}", hex );
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
