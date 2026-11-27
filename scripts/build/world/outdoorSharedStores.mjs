/*
===========================================================================

outdoorSharedStores.mjs - the shared stores every outdoor region resolves through

The two stores a region bundle names but does not carry: the shared
render resources (sky and water) and the shared object index with its
content-addressed meshes. The region builder builds or reuses them once
per world build, before the regions, because every region's input digest
folds their published bytes in (outdoorSharedInputDigest); a retained
store claims what it names and is trusted only while its recorded shape
still describes the current inputs.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { normalizeAssetPath, publicPathToFile } from "../shared/assetPaths.mjs";
import { mapWithConcurrency } from "../shared/asyncUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { claimKeptOutput, claimPublicFile } from "../shared/publicationLedger.mjs";
import { writeIntoPublicTree } from "../shared/publicWrite.mjs";
import {
	OUTDOOR_WORLD_AREA,
	OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
	WATER_NORMAL_FRAME_DURATION_MS
} from "./constants.mjs";
import { copyReferencedSkyImages, resolveSkyTextures } from "./assets/copySkyImages.mjs";
import { copyReferencedWaterImages, resolveWaterTextures } from "./assets/copyWaterImages.mjs";
import { exists, publishedJson, writeJson } from "./io.mjs";
import { parseJmxMapObjectPlacementO2, readJmxMapObjectInfo } from "./jmx/index.mjs";
import { buildTitleSectorObjectResources } from "./objects/buildTitleSectorObjectResources.mjs";
import { publicRoot } from "./paths.mjs";

const OUTDOOR_WORLD_OBJECT_INDEX_PATH = publicPathToFile( OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH, publicRoot );
const OUTDOOR_WORLD_SHARED_RENDER_PATH = publicPathToFile( OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH, publicRoot );
export const OUTDOOR_WORLD_OBJECT_MESH_ROOT_PUBLIC_PATH = "/assets/world/outdoor/object-meshes";

/*
================
buildOutdoorSharedRenderResources
================
*/
export async function buildOutdoorSharedRenderResources( options = {} ) {
	if ( !options.force && (await exists( OUTDOOR_WORLD_SHARED_RENDER_PATH )) ) {
		const existing = JSON.parse( await readFile( OUTDOOR_WORLD_SHARED_RENDER_PATH, "utf8" ) );
		validateSharedRenderResources( existing );
		// The kept resources claim the sky and water textures they name.
		await claimKeptOutput( OUTDOOR_WORLD_SHARED_RENDER_PATH );
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
export async function buildOutdoorSharedObjectResources( options ) {
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
			sourcePath: entry.sourcePath,
			placementCount: usage.placementCountsByObjectId.get( objectId )
		};
	} );
	if ( !options.force && (await exists( OUTDOOR_WORLD_OBJECT_INDEX_PATH )) ) {
		const index = JSON.parse( await readFile( OUTDOOR_WORLD_OBJECT_INDEX_PATH, "utf8" ) );
		validateObjectResourceIndex( index );
		// Code stamps cannot notice added source sectors. Check their complete
		// object usage before trusting a shared index from an earlier region set.
		if ( outdoorObjectIndexMatchesDefinitions( index, objectDefinitions ) ) {
			await claimKeptOutput( OUTDOOR_WORLD_OBJECT_INDEX_PATH );
			return { index, collisionResources: collisionResourcesFromIndex( index ) };
		}
	}
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
		// Content-addressed: a forced rebuild still leaves identical bytes alone,
		// so their mtimes, sidecars and pack hashes stay fresh.
		if ( options.force || !(await exists( outputPath )) ) {
			await writeIntoPublicTree( outputPath, publishedJson( bytes ) );
		} else claimPublicFile( outputPath );

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
outdoorObjectIndexMatchesDefinitions

A retained resource index must describe every currently used object, with
the same catalog identity and placement count. Added regions can introduce
objects without changing any builder code or the existing index's shape.
================
*/
export function outdoorObjectIndexMatchesDefinitions( index, definitions ) {
	const resources = new Map( index.bsr.map( ( resource ) => [ resource.objectId, resource ] ) );
	// The same count both ways: an index that still holds the objects of sectors
	// since removed is as stale as one missing a new object.
	return resources.size === index.bsr.length && resources.size === definitions.length &&
		definitions.every( ( definition ) => {
			const resource = resources.get( definition.objectId );
			return resource !== undefined && resource.objectFlags === definition.flags &&
				resource.placementCount === definition.placementCount &&
				normalizeAssetPath( resource.sourcePath ) === normalizeAssetPath( definition.sourcePath );
		} );
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
