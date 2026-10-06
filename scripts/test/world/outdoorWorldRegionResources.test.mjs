/*
===========================================================================

outdoorWorldRegionResources.test.mjs - outdoor world discovery, routing and publication

Every complete retail sector is discovered and matches the native mapinfo
bitmap; routing and catalog publication stay consistent across encodings.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";
import { brotliDecompressSync, gunzipSync } from "node:zlib";
import { verifyOutdoorPayloadContracts } from "../helpers/outdoorRegionPayloadCheck.mjs";
import { dataExtractedRoot } from "../../build/world/paths.mjs";
import {
	OUTDOOR_WORLD_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
	OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
	OUTDOOR_WORLD_SOURCE_NAME,
	buildOutdoorWorldRegionIndexDescriptor,
	discoverOutdoorWorldSectors,
	loadOutdoorWorldRegionResourceGroup,
	publishOutdoorWorldRegionRouting
} from "../../build/world/buildOutdoorWorldRegionResources.mjs";
import {
	buildWorldRegionCatalogDescriptor,
	overlayWorldRegionCatalogDescriptor
} from "../../build/world/buildWorldRegionCatalog.mjs";

const sectors = await discoverOutdoorWorldSectors();
const descriptor = buildOutdoorWorldRegionIndexDescriptor( sectors );
const outdoorGroup = {
	regionIndexDescriptor: descriptor,
	worldRegionsPublicPath: OUTDOOR_WORLD_INDEX_PUBLIC_PATH,
	sourceName: OUTDOOR_WORLD_SOURCE_NAME
};

test("global outdoor discovery admits every complete retail map/navmesh sector", () => {
	assert.equal( sectors.length, 2123 );
	assert.equal( new Set( sectors.map( ( sector ) => sector.id ) ).size, sectors.length );
	assert.ok(
		sectors.every(
			( sector ) =>
				(sector.regionId & 0x8000) === 0 &&
				sector.regionId === ((sector.sectorY << 8) | sector.sectorX)
		)
	);
	assert.deepEqual(
		[ ...sectors ].sort(
			( left, right ) => left.sectorY - right.sectorY || left.sectorX - right.sectorX
		),
		sectors
	);
});

test("routing publication keeps loose, Brotli, and gzip schema generations identical", async () => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-outdoor-sidecars-" ) );
	const indexPath = path.join( tempRoot, "world-regions.json" );
	const publishedDescriptor = {
		...buildOutdoorWorldRegionIndexDescriptor( [
			{ id: "0x6c4f", sectorX: 79, sectorY: 108 }
		] ),
		deliveryMode: "prebuilt"
	};

	try {
		await publishOutdoorWorldRegionRouting( {
			descriptor: publishedDescriptor,
			indexPath,
			publicRoot: tempRoot,
			allowMissingBundles: true,
			updateCatalog: false,
			jobs: 1
		} );
		const [loose, brotli, gzip] = await Promise.all( [
			readFile( indexPath ),
			readFile( `${indexPath}.br` ).then( brotliDecompressSync ),
			readFile( `${indexPath}.gz` ).then( gunzipSync )
		] );

		assert.deepEqual( brotli, loose );
		assert.deepEqual( gzip, loose );
		assert.equal( JSON.parse( gzip.toString( "utf8" ) ).version, 2 );
		assert.equal( JSON.parse( gzip.toString( "utf8" ) ).bundleLayout, "one-region-per-bundle" );
	} finally {
		await rm( tempRoot, { recursive: true, force: true } );
	}
});

test("global outdoor discovery exactly matches the native mapinfo availability bitmap", async () => {
	const mapInfo = await readFile(
		path.join( dataExtractedRoot, "navmesh", "mapinfo.mfo" )
	);
	assert.equal( mapInfo.subarray( 0, 12 ).toString( "ascii" ), "JMXVMFO 1000" );

	const width = mapInfo.readUInt16LE( 12 );
	const height = mapInfo.readUInt16LE( 14 );
	const bitmapOffset = 24; // 12-byte signature + 12-byte MapLoader header.
	assert.equal( width, 256 );
	assert.equal( height, 128 );
	assert.equal( mapInfo.length, bitmapOffset + 0x2000 );

	const nativeRegionIds = [];
	for ( let regionId = 0; regionId < width * height; regionId += 1 ) {
		const byte = mapInfo[bitmapOffset + (regionId >> 3)];
		if ( (byte & (0x80 >> (regionId & 7))) !== 0 ) {
			nativeRegionIds.push( regionId );
		}
	}

	assert.deepEqual(
		sectors.map( ( sector ) => sector.regionId ).sort( ( left, right ) => left - right ),
		nativeRegionIds
	);
});

test("global index gives every sector an independent source frame and bundle path", () => {
	assert.equal( descriptor.version, 2 );
	assert.equal( descriptor.seedRegionId, "0x0000" );
	assert.equal( descriptor.bundleLayout, "one-region-per-bundle" );
	assert.equal( descriptor.regions.length, sectors.length );
	assert.equal(
		new Set( descriptor.regions.map( ( region ) => region.bundlePublicPath ) ).size,
		descriptor.regions.length
	);

	for ( const region of descriptor.regions ) {
		assert.equal( region.seedRegionId, region.id );
		assert.equal(
			region.bundlePublicPath,
			`/assets/world/outdoor/regions/region-${region.id.slice( 2 )}.json`
		);
	}
});

test("published retail routing has a renderable independent payload for every enabled sector", async () => {
	const publishedIndexText = await readFile(
		CLIENT_PUBLIC_ROOT + "/assets/world/outdoor/world-regions.json",
		"utf8"
	);
	const publishedIndex = JSON.parse( publishedIndexText );
	assert.equal( publishedIndex.version, 2 );
	assert.equal( publishedIndex.bundleLayout, "one-region-per-bundle" );
	assert.equal( publishedIndex.deliveryMode, "prebuilt" );
	assert.deepEqual(
		publishedIndex.regions.map( ( region ) => region.id ),
		descriptor.regions.map( ( region ) => region.id )
	);

	const objectIndexText = await readFile(
		CLIENT_PUBLIC_ROOT + "/assets/world/outdoor/object-resources.json",
		"utf8"
	);
	const objectIndex = JSON.parse( objectIndexText );
	assert.equal( objectIndex.bsrCount, objectIndex.bsr.length );
	assert.equal( objectIndex.meshCount, objectIndex.meshFiles.length );
	const meshReferenceByPath = new Map(
		objectIndex.meshFiles.map( ( reference ) => [
			normalizeResourcePath( reference.sourcePath ),
			reference
		] )
	);

	// A compound root carries no render section of its own: each of its
	// branches is a complete BSR resource.
	for ( const resource of objectIndex.bsr.flatMap( ( root ) => root.branches ?? [ root ] ) ) {
		const meshPaths = new Set( [
			...resource.meshPaths,
			...resource.renderMeshSection.paths,
			...(resource.primaryMeshPath ? [ resource.primaryMeshPath ] : [])
		] );
		for ( const meshPath of meshPaths ) {
			assert.ok(
				meshReferenceByPath.has( normalizeResourcePath( meshPath ) ),
				`retail BSR ${resource.sourcePath} is missing mesh ${meshPath}`
			);
		}
	}

	// Validate the complete shared BMS plane, not only meshes selected by the
	// current city. MissionWorldRegionHydrator enforces these same metadata
	// invariants before a region can enter the synchronous residency fold. The
	// wrapper-content half of that check runs in verifyOutdoorPayloadContracts
	// below; the reference shape is asserted here.
	for ( const reference of objectIndex.meshFiles ) {
		assert.ok(
			meshWrapperPathHonorsDigest( reference.publicPath, reference.sha256 ),
			`mesh reference ${reference.sourcePath}: publicPath ${reference.publicPath} ` +
				`must end with /${reference.sha256}.json`
		);
	}

	// The per-file deep validation (every region bundle, every hashed mesh
	// wrapper) runs in outdoorRegionPayloadCheck.mjs: worker threads for the
	// parse work, with verdicts cached by (path, size, mtime) under a context
	// digest of both index planes and the validator source. Every sector is
	// still verified on every run; only re-parsing byte-identical payloads is
	// skipped.
	const verdict = await verifyOutdoorPayloadContracts( {
		publicRoot: CLIENT_PUBLIC_ROOT,
		regions: publishedIndex.regions.map( ( region ) => ({
			id: region.id,
			bundlePublicPath: region.bundlePublicPath
		}) ),
		meshReferences: objectIndex.meshFiles,
		indexSha256: createHash( "sha256" ).update( publishedIndexText, "utf8" ).digest( "hex" ),
		objectIndexSha256: createHash( "sha256" ).update( objectIndexText, "utf8" ).digest( "hex" ),
		sharedRenderPublicPath: OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
		objectIndexPublicPath: OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH
	} );
	if ( verdict.failures.length > 0 ) {
		assert.fail(
			`${verdict.failures.length} published payload(s) violate the routing contract:\n` +
				verdict.failures.join( "\n" )
		);
	}

	for ( const publicPath of verdict.referencedImages ) {
		await access( publicAssetUrl( publicPath ) );
	}
});

test("Jangan's southern continuation is globally indexed, not stage-cherry-picked", () => {
	const south = descriptor.regions.find( ( region ) => region.id === "0x60a8" );
	assert.deepEqual( south, {
		id: "0x60a8",
		sectorX: 168,
		sectorY: 96,
		seedRegionId: "0x60a8",
		bundlePublicPath: "/assets/world/outdoor/regions/region-60a8.json"
	} );

	assert.ok( descriptor.regions.some( ( region ) => region.id === "0x5fa8" ) );
	assert.ok( descriptor.regions.some( ( region ) => region.id === "0x61a8" ) );
});

test("catalog copies the per-entry seed instead of the descriptor's synthetic seed", () => {
	const catalog = buildWorldRegionCatalogDescriptor( [ outdoorGroup ] );
	const entry = catalog.regionsById["0x60a8"][0];
	assert.equal( entry.source, OUTDOOR_WORLD_SOURCE_NAME );
	assert.equal( entry.seedRegionId, "0x60a8" );
	assert.deepEqual( entry.seedSector, { sectorX: 168, sectorY: 96 } );
	assert.equal( entry.worldRegionsPublicPath, OUTDOOR_WORLD_INDEX_PUBLIC_PATH );
	assert.equal( entry.bundlePublicPath, "/assets/world/outdoor/regions/region-60a8.json" );
});

test("outdoor catalog overlay replaces only its global source family", () => {
	const base = {
		format: "sro-world-region-catalog",
		version: 1,
		regionsById: {
			"0x694e": [
				{
					id: "0x694e",
					area: "constantinople",
					seedRegionId: "0x694e",
					seedSector: { sectorX: 78, sectorY: 105 },
					sectorX: 78,
					sectorY: 105,
					worldRegionsPublicPath: "/assets/world/constantinople/world-regions-694e.json",
					bundlePublicPath: "/assets/world/constantinople/region-694e.json",
					source: "title"
				}
			],
			"0x60a8": [
				{
					id: "0x60a8",
					area: "old",
					seedRegionId: "0x60a8",
					seedSector: { sectorX: 168, sectorY: 96 },
					sectorX: 168,
					sectorY: 96,
					worldRegionsPublicPath: "/old-index.json",
					bundlePublicPath: "/old-bundle.json",
					source: OUTDOOR_WORLD_SOURCE_NAME
				}
			]
		}
	};
	const overlaid = overlayWorldRegionCatalogDescriptor( base, [ outdoorGroup ], {
		replaceSources: [ OUTDOOR_WORLD_SOURCE_NAME ]
	} );

	assert.equal( overlaid.regionsById["0x694e"][0].source, "title" );
	assert.equal( overlaid.regionsById["0x60a8"].length, 1 );
	assert.equal(
		overlaid.regionsById["0x60a8"][0].bundlePublicPath,
		"/assets/world/outdoor/regions/region-60a8.json"
	);
});

test("split outdoor payload contract uses one render/index plane plus hashed mesh files", () => {
	assert.equal(
		OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH,
		"/assets/world/outdoor/shared-render-resources.json"
	);
	assert.equal(
		OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH,
		"/assets/world/outdoor/object-resources.json"
	);
});

test("production catalog loading rejects a dev routing index with missing bundles", async () => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-outdoor-index-" ) );
	const indexPath = path.join( tempRoot, "world-regions.json" );
	const incomplete = {
		...descriptor,
		deliveryMode: "dev-on-demand",
		regions: [ descriptor.regions.find( ( region ) => region.id === "0x60a8" ) ]
	};
	await writeFile( indexPath, `${JSON.stringify( incomplete )}\n`, "utf8" );

	try {
		await assert.rejects(
			loadOutdoorWorldRegionResourceGroup( { indexPath, jobs: 1, publicRoot: tempRoot } ),
			/pnpm assets build world-outdoor/
		);
		// Cast: the undefined return only happens when the index file is absent,
		// and this test wrote indexPath above; allowDevOnDemand yields a group.
		const devGroup =
			/** @type {NonNullable<Awaited<ReturnType<typeof loadOutdoorWorldRegionResourceGroup>>>} */ (await loadOutdoorWorldRegionResourceGroup(
				{
					indexPath,
					jobs: 1,
					publicRoot: tempRoot,
					allowDevOnDemand: true
				}
			));
		assert.equal( devGroup.incompleteBundleCount, 1 );
	} finally {
		await rm( tempRoot, { recursive: true, force: true } );
	}
});

/*
================
publicAssetUrl
================
*/
function publicAssetUrl( publicPath ) {
	return pathToFileURL( path.join( CLIENT_PUBLIC_ROOT, publicPath.replace( /^\/+/, "" ) ) );
}

/**
 * The digest suffix is a LITERAL path segment, so this is a plain
 * string-suffix comparison, not a pattern. The previous check interpolated
 * sha256 into a RegExp unescaped; live digests are pure hex so it happened
 * to behave, but a metacharacter would have silently changed the pattern's
 * meaning ("." matching any character, "+" turning quantifier).
 * probe_regex_vacuity_mesh_suffix.mjs extracts this function and proves the
 * literal behaviour against hostile digests.
 */
/*
================
meshWrapperPathHonorsDigest
================
*/
function meshWrapperPathHonorsDigest( publicPath, sha256 ) {
	return typeof publicPath === "string" && publicPath.endsWith( `/${sha256}.json` );
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
