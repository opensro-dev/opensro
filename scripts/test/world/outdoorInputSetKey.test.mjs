/*
===========================================================================

outdoorInputSetKey.test.mjs - the outdoor reuse check's whole-set key

#4337 item 35: findStaleOutdoorRegions hashed every region's inputs on
every build (3860 regions) even when all were reused. These tests pin the
whole-set key that replaces that: an unchanged input set is recognised
from stat signatures alone and makes no per-region digest calls, and one
moved region is the only region re-hashed and the only one named stale.
Fixture trees only - no retail data.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { publicPathToFile } from "../../build/shared/assetPaths.mjs";
import {
	OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH
} from "../../build/world/constants.mjs";
import {
	findStaleOutdoorRegions,
	outdoorInputSetKey,
	outdoorRegionInputDigest,
	outdoorRegionInputSignature,
	outdoorSharedInputDigest,
	readRegionLedger,
	writeTerrainTileLedger
} from "../../build/world/outdoorRegionLedger.mjs";

/*
================
makeFixtureWorld

A three-sector world in a temp tree: each sector's four input files under
Map_extracted, the shared inputs outdoorSharedInputDigest reads, and a
ledger path of its own. Nothing here touches the retail extraction.
================
*/
async function makeFixtureWorld() {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-outdoor-setkey-" ) );
	const extractedRoot = path.join( root, "extracted" );
	const mapRoot = path.join( extractedRoot, "Map_extracted" );
	const publicRoot = path.join( root, "public" );
	await mkdir( mapRoot, { recursive: true } );
	await writeFile( path.join( mapRoot, "object.ifo" ), "objects" );
	await writeFile( path.join( mapRoot, "tile2d.ifo" ), "tiles" );
	await writeFile( path.join( mapRoot, "layerobject.json" ), "{}" );
	for ( const publicPath of [ OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH, OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH ] ) {
		const file = publicPathToFile( publicPath, publicRoot );
		await mkdir( path.dirname( file ), { recursive: true } );
		await writeFile( file, "{}" );
	}
	const sectors = [];
	for ( const id of [ "r1", "r2", "r3" ] ) {
		const mapBasePath = path.join( mapRoot, `sector-${id}` );
		for ( const suffix of [ ".m", ".t", ".o2" ] ) {
			await writeFile( `${mapBasePath}${suffix}`, `${id}${suffix} bytes` );
		}
		const navmeshPath = `${mapBasePath}.nvm`;
		await writeFile( navmeshPath, `${id} navmesh` );
		sectors.push( { id, mapBasePath, navmeshPath } );
	}
	return { root, extractedRoot, publicRoot, sectors, ledgerPath: path.join( root, "ledger.json" ) };
}

/*
================
primeFixtureLedger

Record the ledger a full build would write: every sector's digest and
stat signature, the shared digest, and the whole-set key over them.
================
*/
async function primeFixtureLedger( world ) {
	const sharedDigest = await outdoorSharedInputDigest( world.extractedRoot, world.publicRoot );
	const inputs = new Map();
	const inputStats = new Map();
	for ( const sector of world.sectors ) {
		inputs.set( sector.id, await outdoorRegionInputDigest( sector, sharedDigest ) );
		inputStats.set( sector.id, await outdoorRegionInputSignature( sector ) );
	}
	await writeTerrainTileLedger( new Map(), new Map(), inputs, {
		ledgerPath: world.ledgerPath,
		inputStats,
		setKey: outdoorInputSetKey( sharedDigest, inputStats ),
		sharedDigest
	} );
	return sharedDigest;
}

/*
================
countingDigest

The regionDigest seam findStaleOutdoorRegions takes, counting each call:
the per-region work the whole-set key exists to skip.
================
*/
function countingDigest( counter ) {
	return async ( sector, sharedDigest ) => {
		counter.calls += 1;
		return outdoorRegionInputDigest( sector, sharedDigest );
	};
}

/*
================
checkFixture

findStaleOutdoorRegions over the fixture world with a counting digest.
================
*/
function checkFixture( world, counter ) {
	return findStaleOutdoorRegions( {
		extractedRoot: world.extractedRoot,
		publicRoot: world.publicRoot,
		sectors: world.sectors,
		jobs: 1,
		ledgerPath: world.ledgerPath,
		regionDigest: countingDigest( counter )
	} );
}

test("an unchanged input set is vouched by the set key with no per-region digest call", async () => {
	const world = await makeFixtureWorld();
	try {
		const sharedDigest = await primeFixtureLedger( world );
		const ledger = await readRegionLedger( world.ledgerPath );
		assert.equal( ledger.sharedDigest, sharedDigest );
		assert.equal( ledger.inputStats.size, world.sectors.length );
		assert.ok( ledger.setKey );

		const counter = { calls: 0 };
		assert.deepEqual( await checkFixture( world, counter ), [] );
		assert.equal( counter.calls, 0 );
	} finally {
		await rm( world.root, { recursive: true, force: true } );
	}
});

test("one moved region is the only region re-hashed and the only one named stale", async () => {
	const world = await makeFixtureWorld();
	try {
		await primeFixtureLedger( world );
		const moved = world.sectors[1];
		await writeFile( `${moved.mapBasePath}.m`, "r2.m bytes, rewritten longer" );

		const counter = { calls: 0 };
		assert.deepEqual( await checkFixture( world, counter ), [ "r2" ] );
		assert.equal( counter.calls, 1 );
	} finally {
		await rm( world.root, { recursive: true, force: true } );
	}
});

test("a shared input moving re-hashes every region under the new shared digest", async () => {
	const world = await makeFixtureWorld();
	try {
		await primeFixtureLedger( world );
		await writeFile( path.join( world.extractedRoot, "Map_extracted", "object.ifo" ), "objects, renumbered" );

		const counter = { calls: 0 };
		assert.deepEqual( await checkFixture( world, counter ), [ "r1", "r2", "r3" ] );
		assert.equal( counter.calls, 3 );
	} finally {
		await rm( world.root, { recursive: true, force: true } );
	}
});

test("a content change preserving size and mtime keeps the recorded digest standing", async () => {
	const world = await makeFixtureWorld();
	try {
		// The documented stat trust (fileHashCache.mjs): bytes rewritten at
		// the same size and mtime are invisible to the signature probe, and
		// no writer in this pipeline produces them. The mtime is pinned to a
		// whole second first so the probe sees it exactly as primed.
		const sector = world.sectors[0];
		const file = `${sector.mapBasePath}.m`;
		const pinned = new Date( "2026-01-01T00:00:00.000Z" );
		await utimes( file, pinned, pinned );
		await primeFixtureLedger( world );
		await writeFile( file, "r1.m bytex" );
		await utimes( file, pinned, pinned );

		const counter = { calls: 0 };
		assert.deepEqual( await checkFixture( world, counter ), [] );
		assert.equal( counter.calls, 0 );
	} finally {
		await rm( world.root, { recursive: true, force: true } );
	}
});
