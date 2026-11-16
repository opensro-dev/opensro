/*
===========================================================================

outdoorRegionPayloadCheck.mjs - deep validation of the published outdoor tree

Deep per-payload validation for the published outdoor routing tree, with a
verdict cache so the region test does not re-parse ~2.3 GB of byte-identical
JSON on every run.

Soundness of the cache: a verdict is only ever recorded after the full deep
validation of a payload passed, and it is only trusted again while ALL of
the following still hold:
  - the payload's (size, mtimeMs) stat is unchanged,
  - the published world-regions index and object-resources index are
    byte-identical (their sha256 is part of the cache context digest),
  - the expected routing constants are unchanged (part of the digest),
  - this module's own source is unchanged (part of the digest), so any
    edit to the assertions invalidates every cached verdict.
Every enabled sector is therefore still checked on every run - the cache
merely replaces a redundant re-parse of provably unchanged content with a
stat-level check. Any file that misses the cache goes through the exact
deep validation the test always performed, spread across worker threads.

Set SRO_OUTDOOR_PAYLOAD_VERDICT_CACHE=off to ignore and skip writing the
cache (forces the full deep validation of every payload). A corrupt or
stale cache file is discarded, never trusted.

===========================================================================
*/

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Worker, isMainThread, parentPort, workerData } from "node:worker_threads";
import { writeStateFile } from "../../build/shared/stateFile.mjs";
import { buildStatePath } from "../../lib/generatedRoot.mjs";

const CACHE_FORMAT = "sro-outdoor-payload-verdicts";
// Beside the generated tree whose payloads it vouches for (generatedRoot.mjs).
const CACHE_PATH = buildStatePath( "outdoorPayloadVerdicts.json" );
const CACHE_DISABLED = /^(1|on|true|yes)$/i.test( process.env.SRO_OUTDOOR_PAYLOAD_VERDICT_CACHE_DISABLE ?? "" ) ||
	/^(0|off|false|no)$/i.test( process.env.SRO_OUTDOOR_PAYLOAD_VERDICT_CACHE ?? "" );

/*
================
verifyOutdoorPayloadContracts

Validate every region bundle and every hashed mesh wrapper in the published
outdoor tree. Returns { failures, referencedImages }; failures is empty only
when every payload passed either a trusted cached verdict or a fresh deep
validation.

options:
  publicRoot              absolute path of .generated/client-public
  regions                 [{ id, bundlePublicPath }] from world-regions.json
  meshReferences          objectIndex.meshFiles entries
  indexSha256             sha256 hex of the raw world-regions.json text
  objectIndexSha256       sha256 hex of the raw object-resources.json text
  sharedRenderPublicPath  expected bundle.sharedRenderResourcesPublicPath
  objectIndexPublicPath   expected bundle.objects.resourceIndexPublicPath
================
*/
export async function verifyOutdoorPayloadContracts( options ) {
	const contextDigest = await computeContextDigest( options );
	const cache = CACHE_DISABLED ? emptyCache() : await loadCache( contextDigest );

	const tasks = [
		...options.regions.map( ( region ) => ({
			kind: "region",
			key: region.bundlePublicPath,
			id: region.id,
			publicPath: region.bundlePublicPath
		}) ),
		...options.meshReferences.map( ( reference ) => ({
			kind: "mesh",
			key: reference.publicPath,
			publicPath: reference.publicPath,
			reference
		}) )
	];

	const failures = [];
	const referencedImages = new Set();
	const nextEntries = { regions: {}, meshes: {} };
	const misses = [];

	const stats = await statAll( options.publicRoot, tasks );
	for ( let index = 0; index < tasks.length; index += 1 ) {
		const task = tasks[index];
		const fileStat = stats[index];
		const bucket = task.kind === "region" ? cache.regions : cache.meshes;
		const entry = bucket[task.key];
		if (
			fileStat !== null &&
			entry &&
			entry.size === fileStat.size &&
			entry.mtimeMs === fileStat.mtimeMs
		) {
			recordVerdict( nextEntries, task, entry, cache.imageTable, referencedImages );
			continue;
		}
		misses.push( task );
	}

	if ( misses.length > 0 ) {
		const results = await runDeepValidation( options, misses );
		for ( const task of misses ) {
			const result = results.get( task.key );
			if ( !result ) {
				failures.push( `${task.publicPath}: worker returned no verdict` );
				continue;
			}
			if ( !result.ok ) {
				failures.push( `${task.publicPath}: ${result.message}` );
				continue;
			}
			const entry = { size: result.size, mtimeMs: result.mtimeMs };
			if ( task.kind === "region" ) {
				entry.images = result.images;
			}
			recordVerdict( nextEntries, task, entry, null, referencedImages );
		}
	}

	if ( !CACHE_DISABLED ) {
		await writeCache( contextDigest, nextEntries );
	}

	return { failures, referencedImages };
}

/*
================
recordVerdict

Copy a passing verdict into the next cache generation and fold its
referenced images into the run's union set. `imageTable` is non-null when
the entry came from the cache and stores images as string-table indices.
================
*/
function recordVerdict( nextEntries, task, entry, imageTable, referencedImages ) {
	if ( task.kind === "region" ) {
		const images = imageTable ?
			entry.images.map( ( imageIndex ) => imageTable[imageIndex] ) :
			entry.images;
		for ( const image of images ) {
			referencedImages.add( image );
		}
		nextEntries.regions[task.key] = { size: entry.size, mtimeMs: entry.mtimeMs, images };
		return;
	}
	nextEntries.meshes[task.key] = { size: entry.size, mtimeMs: entry.mtimeMs };
}

/*
================
computeContextDigest

The digest that scopes every cached verdict. Any change to the published
index planes, to the expected routing constants, or to this module's own
assertion source produces a different digest and orphans the entire cache.
================
*/
async function computeContextDigest( options ) {
	const validatorSource = await readFile( fileURLToPath( import.meta.url ), "utf8" );
	return sha256Hex(
		JSON.stringify( {
			format: CACHE_FORMAT,
			indexSha256: options.indexSha256,
			objectIndexSha256: options.objectIndexSha256,
			sharedRenderPublicPath: options.sharedRenderPublicPath,
			objectIndexPublicPath: options.objectIndexPublicPath,
			validatorSha256: sha256Hex( validatorSource )
		} )
	);
}

/*
================
emptyCache
================
*/
function emptyCache() {
	return { regions: {}, meshes: {}, imageTable: [] };
}

/*
================
loadCache

Load the verdict cache, discarding it entirely when unreadable, malformed,
or recorded under a different context digest.
================
*/
async function loadCache( contextDigest ) {
	try {
		const parsed = JSON.parse( await readFile( CACHE_PATH, "utf8" ) );
		if (
			parsed?.format !== CACHE_FORMAT ||
			parsed?.contextDigest !== contextDigest ||
			!Array.isArray( parsed?.imageTable ) ||
			typeof parsed?.regions !== "object" ||
			typeof parsed?.meshes !== "object"
		) {
			return emptyCache();
		}
		return { regions: parsed.regions, meshes: parsed.meshes, imageTable: parsed.imageTable };
	} catch {
		return emptyCache();
	}
}

/*
================
writeCache

Persist passing verdicts whole (stateFile.mjs) so a crashed run can never
leave a half-written cache that the next run would trust.
================
*/
async function writeCache( contextDigest, nextEntries ) {
	const imageTable = [];
	const imageIndexByPath = new Map();
	const regions = {};
	for ( const [key, entry] of Object.entries( nextEntries.regions ) ) {
		const images = entry.images.map( ( image ) => {
			let imageIndex = imageIndexByPath.get( image );
			if ( imageIndex === undefined ) {
				imageIndex = imageTable.length;
				imageTable.push( image );
				imageIndexByPath.set( image, imageIndex );
			}
			return imageIndex;
		} );
		regions[key] = { size: entry.size, mtimeMs: entry.mtimeMs, images };
	}

	const payload = JSON.stringify( {
		format: CACHE_FORMAT,
		contextDigest,
		imageTable,
		regions,
		meshes: nextEntries.meshes
	} );
	try {
		await writeStateFile( CACHE_PATH, payload );
	} catch {
		// A cache that fails to persist only costs the next run time, never
		// correctness; writeStateFile leaves no temp file behind.
	}
}

/*
================
statAll
================
*/
function statAll( publicRoot, tasks ) {
	return Promise.all(
		tasks.map( ( task ) =>
			stat( publicFilePath( publicRoot, task.publicPath ) ).then(
				( fileStat ) => ({ size: fileStat.size, mtimeMs: fileStat.mtimeMs }),
				() => null
			)
		)
	);
}

/*
================
runDeepValidation

Spread the missed tasks across worker threads. Each worker performs the
exact deep validation the test historically ran inline. Tasks are dealt
round-robin so heavyweight city regions do not pile onto one worker.
================
*/
async function runDeepValidation( options, misses ) {
	const workerCount = Math.max(
		1,
		Math.min( 8, os.availableParallelism() - 1, Math.ceil( misses.length / 32 ) )
	);
	/** @type {Array<Array<unknown>>} */
	const chunks = Array.from( { length: workerCount }, () => [] );
	for ( let index = 0; index < misses.length; index += 1 ) {
		chunks[index % workerCount].push( misses[index] );
	}

	const results = new Map();
	await Promise.all(
		chunks.map( async ( chunk ) => {
			for ( const result of await runWorkerChunk( options, chunk ) ) {
				results.set( result.key, result );
			}
		} )
	);
	return results;
}

/*
================
runWorkerChunk
================
*/
function runWorkerChunk( options, chunk ) {
	return new Promise( ( resolvePromise, rejectPromise ) => {
		const worker = new Worker( fileURLToPath( import.meta.url ), {
			workerData: {
				sroOutdoorPayloadWorker: true,
				publicRoot: options.publicRoot,
				sharedRenderPublicPath: options.sharedRenderPublicPath,
				objectIndexPublicPath: options.objectIndexPublicPath,
				tasks: chunk
			}
		} );
		worker.once( "message", ( message ) => resolvePromise( message.results ) );
		worker.once( "error", rejectPromise );
		worker.once( "exit", ( code ) => {
			if ( code !== 0 ) {
				rejectPromise( new Error( `outdoor payload validation worker exited with code ${code}` ) );
			}
		} );
	} );
}

// ---------------------------------------------------------------------------
// Worker side: the deep validation itself. These assertions are the property
// the test pins; keep them byte-for-byte in sync with what the inline test
// asserted before the cache existed.
// ---------------------------------------------------------------------------

if ( !isMainThread && workerData?.sroOutdoorPayloadWorker ) {
	workerMain( workerData ).then(
		// Cast: parentPort is always non-null on the worker side of the fork.
		( results ) => /** @type {import("node:worker_threads").MessagePort} */ (parentPort).postMessage( { results } ),
		( error ) => {
			throw error;
		}
	);
}

/*
================
workerMain
================
*/
async function workerMain( data ) {
	const needsObjectIndex = data.tasks.some( ( task ) => task.kind === "region" );
	const lookups = needsObjectIndex ?
		buildObjectIndexLookups(
			JSON.parse(
				await readFile( publicFilePath( data.publicRoot, data.objectIndexPublicPath ), "utf8" )
			)
		) :
		null;

	const results = [];
	for ( const task of data.tasks ) {
		results.push( await validateTask( data, task, lookups ) );
	}
	return results;
}

/*
================
validateTask
================
*/
async function validateTask( data, task, lookups ) {
	const filePath = publicFilePath( data.publicRoot, task.publicPath );
	try {
		const fileStat = await stat( filePath );
		const text = await readFile( filePath, "utf8" );
		const images = task.kind === "region" ?
			validateRegionBundle( data, task, JSON.parse( text ), lookups ) :
			validateMeshWrapper( task, JSON.parse( text ) );
		return {
			key: task.key,
			ok: true,
			size: fileStat.size,
			mtimeMs: fileStat.mtimeMs,
			images
		};
	} catch ( error ) {
		return { key: task.key, ok: false, message: error.message };
	}
}

/*
================
buildObjectIndexLookups
================
*/
function buildObjectIndexLookups( objectIndex ) {
	return {
		bsrByObjectId: new Map( objectIndex.bsr.map( ( resource ) => [ resource.objectId, resource ] ) ),
		bsrByPath: new Map(
			objectIndex.bsr.map( ( resource ) => [ normalizeResourcePath( resource.sourcePath ), resource ] )
		),
		missingResourcePaths: new Set(
			objectIndex.missing.map( ( entry ) => normalizeResourcePath( entry.sourcePath ) )
		)
	};
}

/*
================
validateRegionBundle

The per-sector renderability property: independent source frame, single
owned sector on every plane, placement ownership, and a retail resource
record behind every placement. Returns the image public paths the bundle
references so the caller can assert they exist.
================
*/
function validateRegionBundle( data, task, bundle, lookups ) {
	const expectedRegionId = Number.parseInt( task.id.slice( 2 ), 16 );

	assert.equal( bundle.source.sectorId, task.id );
	assert.equal( packRegion( bundle.source.sectorX, bundle.source.sectorY ), expectedRegionId );
	assert.equal( bundle.sharedRenderResourcesPublicPath, data.sharedRenderPublicPath );
	assert.equal( bundle.objects.resourceIndexPublicPath, data.objectIndexPublicPath );
	assert.equal( bundle.objects.resources, undefined );

	assert.equal( bundle.terrain.sectors.length, 1 );
	assert.equal(
		packRegion( bundle.terrain.sectors[0].sectorX, bundle.terrain.sectors[0].sectorY ),
		expectedRegionId
	);
	assert.equal( bundle.terrainTextures.sectors.length, 1 );
	assert.equal(
		packRegion(
			bundle.terrainTextures.sectors[0].sectorX,
			bundle.terrainTextures.sectors[0].sectorY
		),
		expectedRegionId
	);
	assert.equal( bundle.navmesh.regions.length, 1 );
	assert.equal( bundle.navmesh.regions[0].regionId & 0xffff, expectedRegionId );
	assert.equal( bundle.objects.sectors.length, 1 );
	assert.equal(
		packRegion( bundle.objects.sectors[0].sectorX, bundle.objects.sectors[0].sectorY ),
		expectedRegionId
	);

	const topPlacements = bundle.objects.placements;
	const sectorPlacements = bundle.objects.sectors[0].placements;
	const sectorPlacementIds = new Set( sectorPlacements.map( objectPlacementIdentity ) );
	assert.equal( bundle.objects.placementCount, topPlacements.length );
	assert.equal( sectorPlacements.length, topPlacements.length );
	for ( const placement of topPlacements ) {
		assert.ok( sectorPlacementIds.has( objectPlacementIdentity( placement ) ) );
		const owner = placement.sourceSector ?? placement.region;
		assert.equal(
			packRegion( owner.sectorX, owner.sectorY ),
			expectedRegionId,
			`${task.id} placement ${placement.uid} lost source-sector ownership`
		);
		const resource = lookups.bsrByObjectId.get( placement.objectId ) ??
			lookups.bsrByPath.get( normalizeResourcePath( placement.resourcePath ) );
		assert.ok(
			resource || lookups.missingResourcePaths.has( normalizeResourcePath( placement.resourcePath ) ),
			`${task.id} placement ${placement.uid} has no retail resource record`
		);
	}

	const images = [];
	const textureSector = bundle.terrainTextures.sectors[0];
	if ( textureSector.lightmapPublicPath ) {
		images.push( textureSector.lightmapPublicPath );
	}
	for ( const tile of bundle.terrainTextures.tileCatalog.referencedTiles ) {
		if ( tile.imagePublicPath ) {
			images.push( tile.imagePublicPath );
		}
	}
	return images;
}

/*
================
validateMeshWrapper

A hashed mesh wrapper must carry the exact metadata its object-resources
reference promises; MissionWorldRegionHydrator enforces the same invariants
before a region can enter the synchronous residency fold.
================
*/
function validateMeshWrapper( task, wrapper ) {
	const reference = task.reference;
	assert.equal( wrapper.format, "sro-world-object-mesh-resource" );
	assert.equal( normalizeResourcePath( wrapper.mesh.sourcePath ), reference.sourcePath );
	assert.equal( wrapper.mesh.byteLength, reference.byteLength );
	assert.equal( wrapper.mesh.vertexCount, reference.vertexCount );
	assert.equal( wrapper.mesh.triangleCount, reference.triangleCount );
	assert.deepEqual( wrapper.mesh.bounds, reference.bounds );
	return undefined;
}

/*
================
publicFilePath
================
*/
function publicFilePath( publicRoot, publicPath ) {
	return path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
}

/*
================
sha256Hex
================
*/
function sha256Hex( text ) {
	return createHash( "sha256" ).update( text, "utf8" ).digest( "hex" );
}

/*
================
packRegion
================
*/
function packRegion( sectorX, sectorY ) {
	return (((sectorY & 0xff) << 8) | (sectorX & 0xff)) & 0xffff;
}

/*
================
objectPlacementIdentity
================
*/
function objectPlacementIdentity( placement ) {
	return `${placement.regionId}:${placement.uid}:${placement.index}:${placement.recordOffset}`;
}

/*
================
normalizeResourcePath
================
*/
function normalizeResourcePath( resourcePath ) {
	return String( resourcePath ?? "" )
		.replaceAll( "\\", "/" )
		.replace( /\/+/g, "/" )
		.replace( /^\/+/, "" )
		.toLowerCase();
}
