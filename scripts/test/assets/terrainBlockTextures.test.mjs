/*
===========================================================================

terrainBlockTextures.test.mjs - the shared block-texture publisher on fixtures

Synthetic DDJ/DDS sources (the nativeCharacterTextures fixture shape) prove
the probe's admission rules, the lightmap publisher's compatibility contract,
the tile publisher's block/PNG split, the cached-bundle migration an
incremental build runs, and the encode cache's validation. The whole suite
runs inside its own generated root (SRO_GENERATED_ROOT before any module
loads), so fixtures never touch the real cache or published tree.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const generatedRoot = await mkdtemp( path.join( os.tmpdir(), "sro-block-generated-" ) );
process.env.SRO_GENERATED_ROOT = generatedRoot;

const {
	probeBlockDdsPayload,
	probeBlockTextureFile,
	publishBlockTextureFile,
	writeAuthoredBlockContainer
} = await import( "../../build/world/assets/blockTextures.mjs" );
const { publishTerrainLightmap, terrainLightmapPublicPath } = await import(
	"../../build/world/assets/copyTerrainLightmaps.mjs"
);
const {
	copyReferencedTerrainTileImages,
	migrateCachedTerrainTileReferences,
	resolveReferencedTerrainTiles,
	terrainTileImagePublicPath,
	terrainTileReferencesCurrent,
	terrainTileTexturePublicPath
} = await import( "../../build/world/assets/copyTerrainTileImages.mjs" );
const { publicRoot, imageSourceRoot } = await import( "../../build/world/paths.mjs" );
const { exists } = await import( "../../build/world/io.mjs" );
const { publicPathToFile } = await import( "../../build/shared/assetPaths.mjs" );
const { sha256Hex } = await import( "../../build/shared/hash.mjs" );
const { refreshCachedTerrainTileBundle } = await import( "../../build/world/buildOutdoorWorldRegionResources.mjs" );

test.after( () => rm( generatedRoot, { recursive: true, force: true } ) );

const DXT1 = 0x31545844;
const DDJ_HEADER = 20;
const DDS_HEADER = 128;

/*
================
fixtureDds

One 4x4 DXT1 block with distinguishable payload bytes; the header offsets
match both source shapes the pipeline reads (DDJ-wrapped DDJ files and the
bare DDS embedded in MAPT sectors).
================
*/
function fixtureDds( { width = 4, height = 4, fourcc = DXT1, levels = 1, fill = 0x39 } = {} ) {
	const blockBytes = (fourcc === DXT1 ? 8 : 16) * levels;
	const dds = Buffer.alloc( DDS_HEADER + blockBytes, 0 );
	dds.write( "DDS ", 0, "ascii" );
	dds.writeUInt32LE( 124, 4 );
	dds.writeUInt32LE( height, 12 );
	dds.writeUInt32LE( width, 16 );
	dds.writeUInt32LE( levels, 28 );
	dds.writeUInt32LE( 32, 76 );
	dds.writeUInt32LE( 4, 80 );
	dds.writeUInt32LE( fourcc, 84 );
	dds.fill( fill, DDS_HEADER );
	return dds;
}

/*
================
fixtureDdj
================
*/
function fixtureDdj( options = {} ) {
	const dds = fixtureDds( options );
	const ddj = Buffer.alloc( DDJ_HEADER + dds.byteLength, 0 );
	ddj.write( "JMXVDDJ 1000", 0, "ascii" );
	dds.copy( ddj, DDJ_HEADER );
	return ddj;
}

test("the probe admits power-of-two DXT DDJs and bare DDS payloads, nothing else", async () => {
	const dir = await mkdtemp( path.join( os.tmpdir(), "sro-block-probe-" ) );
	try {
		const admitted = path.join( dir, "tile.ddj" );
		await writeFile( admitted, fixtureDdj() );
		assert.equal( await probeBlockTextureFile( admitted ), "dxt1" );
		// A second read answers from the memo with the same verdict.
		assert.equal( await probeBlockTextureFile( admitted ), "dxt1" );

		const bare = path.join( dir, "sector.dds" );
		await writeFile( bare, fixtureDds() );
		assert.equal( await probeBlockTextureFile( bare ), "dxt1" );

		const nonPot = path.join( dir, "nonpot.dds" );
		await writeFile( nonPot, fixtureDds( { width: 5, height: 4 } ) );
		assert.equal( await probeBlockTextureFile( nonPot ), null );

		const missing = path.join( dir, "absent.ddj" );
		assert.equal( await probeBlockTextureFile( missing ), null );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
	assert.equal( probeBlockDdsPayload( fixtureDds() ), "dxt1" );
	assert.equal( probeBlockDdsPayload( fixtureDds( { width: 5, height: 5 } ) ), null );
	assert.equal( probeBlockDdsPayload( fixtureDds( { width: 16384 } ) ), null );
	assert.equal( probeBlockDdsPayload( fixtureDds( { height: 16384 } ) ), null );
	assert.equal( probeBlockDdsPayload( Buffer.from( "JMXVDDJ 1000 short" ) ), null );
});

test("the python lane keeps the authored base block and serves repeats from the cache", async () => {
	const dir = await mkdtemp( path.join( os.tmpdir(), "sro-block-publish-" ) );
	try {
		const source = path.join( dir, "tile.ddj" );
		await writeFile( source, fixtureDdj() );
		const first = path.join( dir, "out", "tile.texture" );
		await publishBlockTextureFile( source, first );
		const container = await readFile( first );
		assert.equal( container.readUInt32LE( 0 ), 0x3158544e );
		assert.equal( container.readUInt32LE( 4 ), 4 );
		assert.equal( container.readUInt32LE( 8 ), 4 );
		assert.equal( container.readUInt32LE( 12 ), DXT1 );
		// Authored blocks are copied verbatim behind the NTX header.
		assert.ok(
			container.subarray( 20, 28 ).equals(
				fixtureDdj().subarray( DDJ_HEADER + DDS_HEADER, DDJ_HEADER + DDS_HEADER + 8 )
			)
		);

		const second = path.join( dir, "out2", "tile.texture" );
		await publishBlockTextureFile( source, second );
		assert.ok( (await readFile( second )).equals( container ) );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
});

test("terrain containers ship the authored levels only, verbatim", async () => {
	const dir = await mkdtemp( path.join( os.tmpdir(), "sro-block-authored-" ) );
	try {
		// Single-level source (the retail lightmap/tile shape): one level,
		// the authored blocks byte-for-byte, no generated suffix.
		const single = path.join( dir, "single.texture" );
		await writeAuthoredBlockContainer( fixtureDdj(), "single", single );
		const one = await readFile( single );
		assert.equal( one.readUInt32LE( 16 ), 1 );
		assert.equal( one.length, 20 + 8 );
		assert.ok( one.subarray( 20 ).equals( fixtureDdj().subarray( DDJ_HEADER + DDS_HEADER ) ) );

		// A multi-level source keeps exactly its authored count - retail
		// sampled the file's own levels, so the remap never adds or drops.
		const three = path.join( dir, "three.texture" );
		const authored = fixtureDdj( { levels: 3 } );
		await writeAuthoredBlockContainer( authored, "three", three );
		const multi = await readFile( three );
		assert.equal( multi.readUInt32LE( 16 ), 3 );
		// Level 0 verbatim; the extent is exactly three levels.
		assert.ok(
			multi.subarray( 20, 28 ).equals( authored.subarray( DDJ_HEADER + DDS_HEADER, DDJ_HEADER + DDS_HEADER + 8 ) )
		);
		assert.equal( multi.length, 20 + 8 + 8 + 8 );

		// A lying header (count 5 on a 4x4 surface) clamps to the chain the
		// dimensions describe; a truncated payload fails loudly.
		const liar = fixtureDdj( { levels: 5 } );
		const clamped = path.join( dir, "clamped.texture" );
		await writeAuthoredBlockContainer( liar, "clamped", clamped );
		assert.equal( (await readFile( clamped )).readUInt32LE( 16 ), 3 );

		const truncated = fixtureDdj();
		truncated.fill( 0, DDJ_HEADER + DDS_HEADER + 4 );
		await assert.rejects(
			() =>
				writeAuthoredBlockContainer(
					truncated.subarray( 0, DDJ_HEADER + DDS_HEADER + 4 ),
					"short",
					path.join( dir, "bad.texture" )
				),
			/truncated authored level/
		);

		// Bare DDS payloads remap identically (the MAPT lightmap shape).
		const bare = path.join( dir, "bare.texture" );
		await writeAuthoredBlockContainer( fixtureDds(), "bare", bare );
		const bareContainer = await readFile( bare );
		assert.equal( bareContainer.readUInt32LE( 16 ), 1 );
		assert.ok( bareContainer.subarray( 20 ).equals( fixtureDds().subarray( DDS_HEADER ) ) );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
});

test("a persisted tile ledger without recorded references is discarded, not trusted", async () => {
	// The ledger-hit path the review named: a ledger whose tiles do not
	// record the bundle's reference cannot be checked against the probe.
	// Version 4 records it, version 5 adds each region's named paths; older
	// ledgers are refused and re-read.
	const { readTerrainTileLedger } = await import( "../../build/world/buildOutdoorWorldRegionResources.mjs" );
	const ledgerDir = path.join( generatedRoot, "intermediate" );
	await mkdir( ledgerDir, { recursive: true } );
	await writeFile(
		path.join( ledgerDir, "outdoor-terrain-tiles.json" ),
		JSON.stringify( { version: 3, regions: { "27024": [ { ddjFileName: "x.ddj", sourcePath: "y" } ] } } )
	);
	const stale = await readTerrainTileLedger();
	assert.equal( stale.size, 0, "a v3 ledger is ignored" );
	await writeFile(
		path.join( ledgerDir, "outdoor-terrain-tiles.json" ),
		JSON.stringify( {
			version: 4,
			regions: { "27024": [ { ddjFileName: "x.ddj", sourcePath: "y", imagePublicPath: "/z.png" } ] }
		} )
	);
	assert.equal( (await readTerrainTileLedger()).size, 0, "a v4 ledger without named paths is ignored" );
	await writeFile(
		path.join( ledgerDir, "outdoor-terrain-tiles.json" ),
		JSON.stringify( {
			version: 5,
			regions: { "27024": [ { ddjFileName: "x.ddj", sourcePath: "y", imagePublicPath: "/z.png" } ] },
			references: { "27024": [ "/z.png" ] }
		} )
	);
	const fresh = await readTerrainTileLedger();
	assert.equal( fresh.size, 1, "a v5 ledger is read" );
});

test("the lightmap publisher retains the raw sibling for existing bundles", async () => {
	const area = "__test-lightmaps";
	const sectors = await publishTerrainLightmap( area, 250, 250, fixtureDds() );
	assert.equal( sectors, terrainLightmapPublicPath( area, 250, 250, true ) );
	assert.match( sectors, /terrain-lightmaps\/250-250\.texture$/ );
	const raw = terrainLightmapPublicPath( area, 250, 250, false );
	// Another bundle can still reference the pre-block publisher's output.
	const stalePath = publicPathToFile( raw, publicRoot );
	await mkdir( path.dirname( stalePath ), { recursive: true } );
	await writeFile( stalePath, Buffer.from( "stale" ) );
	await publishTerrainLightmap( area, 250, 250, fixtureDds() );
	assert.ok( (await readFile( stalePath )).equals( Buffer.from( "stale" ) ) );
	assert.ok( await exists( publicPathToFile( sectors, publicRoot ) ) );
});

test("referenced tiles split between block containers and converted PNG copies", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-tiles-" ) );
	const ddjName = "zzz_test_tile_01.ddj";
	try {
		await mkdir( path.join( extracted, "Map_extracted", "tile2d" ), { recursive: true } );
		await writeFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), fixtureDdj() );
		const catalog = { entriesById: { 1: { id: 1, flags: 0, category: 0, ddjFileName: ddjName, metadata: {} } } };
		const resolved = await resolveReferencedTerrainTiles( [ 1 ], catalog, extracted, extracted );
		assert.equal( resolved[0].blockFormat, "dxt1" );
		assert.equal( resolved[0].imagePublicPath, terrainTileTexturePublicPath( ddjName ) );

		const published = publicPathToFile( terrainTileTexturePublicPath( ddjName ), publicRoot );
		// Another region can still reference the earlier converted PNG.
		await mkdir( path.dirname( published ), { recursive: true } );
		await writeFile( published.replace( /\.texture$/, ".png" ), Buffer.from( "stale" ) );
		await copyReferencedTerrainTileImages( resolved, extracted );
		assert.ok( await exists( published ) );
		assert.equal( await exists( published.replace( /\.texture$/, ".png" ) ), true );

		// A non-block tile (non-power-of-two) keeps the converted PNG path and
		// publishes the staging copy unchanged.
		const pngName = "zzz_test_tile_02.ddj";
		await writeFile(
			path.join( extracted, "Map_extracted", "tile2d", pngName ),
			fixtureDdj( { width: 5, height: 5 } )
		);
		const staging = path.join( imageSourceRoot, "Map_extracted", "tile2d", "zzz_test_tile_02.png" );
		await mkdir( path.dirname( staging ), { recursive: true } );
		await writeFile( staging, Buffer.from( "converted-png" ) );
		const pngCatalog = { entriesById: { 2: { id: 2, flags: 0, category: 0, ddjFileName: pngName, metadata: {} } } };
		const pngResolved = await resolveReferencedTerrainTiles( [ 2 ], pngCatalog, extracted, extracted );
		assert.equal( pngResolved[0].blockFormat, null );
		assert.equal( pngResolved[0].imagePublicPath, terrainTileImagePublicPath( pngName ) );
		await copyReferencedTerrainTileImages( pngResolved, extracted );
		const publishedPng = publicPathToFile( terrainTileImagePublicPath( pngName ), publicRoot );
		assert.ok( (await readFile( publishedPng )).equals( Buffer.from( "converted-png" ) ) );
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});

/*
================
An incremental build after the format change

A cached region bundle still names the .png a pre-container build published.
Migrate the chosen bundle while preserving files other bundles still name.
The reverse migration uses the same dependency-first publication order.
================
*/
test("a cached bundle's tile references migrate while preserving compatibility", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-migrate-" ) );
	const ddjName = "zzz_test_tile_03.ddj";
	try {
		await mkdir( path.join( extracted, "Map_extracted", "tile2d" ), { recursive: true } );
		await writeFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), fixtureDdj() );

		// The pre-container state: the cached bundle references the .png and
		// the converted .png sits in the published tree.
		const bundle = {
			terrainTextures: {
				tileCatalog: {
					referencedTiles: [ {
						ddjFileName: ddjName,
						sourcePath: "extracted/Map_extracted/tile2d/" + ddjName,
						imagePublicPath: terrainTileImagePublicPath( ddjName )
					} ]
				}
			}
		};
		const stalePng = publicPathToFile( terrainTileImagePublicPath( ddjName ), publicRoot );
		await mkdir( path.dirname( stalePng ), { recursive: true } );
		await writeFile( stalePng, Buffer.from( "converted" ) );

		// The incremental reuse: migrate the cached references, then publish.
		assert.equal( await migrateCachedTerrainTileReferences( bundle, extracted ), true );
		assert.equal(
			bundle.terrainTextures.tileCatalog.referencedTiles[0].imagePublicPath,
			terrainTileTexturePublicPath( ddjName )
		);
		await copyReferencedTerrainTileImages( bundle.terrainTextures.tileCatalog.referencedTiles, extracted );

		// Both the migrated reference and earlier bundles' PNG resolve.
		const published = publicPathToFile( terrainTileTexturePublicPath( ddjName ), publicRoot );
		assert.ok( await exists( published ) );
		assert.equal( await exists( stalePng ), true );
		assert.equal( (await readFile( published )).readUInt32LE( 0 ), 0x3158544e );

		// The reverse: a source the probe no longer admits falls back to the
		// PNG reference the publisher actually serves.
		const pngName = "zzz_test_tile_04.ddj";
		await writeFile(
			path.join( extracted, "Map_extracted", "tile2d", pngName ),
			fixtureDdj( { width: 5, height: 5 } )
		);
		const staging = path.join( imageSourceRoot, "Map_extracted", "tile2d", "zzz_test_tile_04.png" );
		await mkdir( path.dirname( staging ), { recursive: true } );
		await writeFile( staging, Buffer.from( "converted-png" ) );
		const stale = {
			terrainTextures: {
				tileCatalog: {
					referencedTiles: [ {
						ddjFileName: pngName,
						sourcePath: "extracted/Map_extracted/tile2d/" + pngName,
						imagePublicPath: terrainTileTexturePublicPath( pngName )
					} ]
				}
			}
		};
		assert.equal( await migrateCachedTerrainTileReferences( stale, extracted ), true );
		assert.equal(
			stale.terrainTextures.tileCatalog.referencedTiles[0].imagePublicPath,
			terrainTileImagePublicPath( pngName )
		);
		await copyReferencedTerrainTileImages( stale.terrainTextures.tileCatalog.referencedTiles, extracted );
		const publishedPng = publicPathToFile( terrainTileImagePublicPath( pngName ), publicRoot );
		assert.ok( (await readFile( publishedPng )).equals( Buffer.from( "converted-png" ) ) );

		// An up-to-date bundle migrates nothing.
		assert.equal( await migrateCachedTerrainTileReferences( bundle, extracted ), false );
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});

/*
================
Corrupt cache entries fail the build

Whatever wrote a partial or foreign artifact into the content-addressed
cache, the copy out of the cache re-validates and refuses to ship it. The
cache key itself folds in the encoder's bytes, proven by recomputing it
here the same way the publisher does.
================
*/
test("a corrupt cache entry is rejected instead of published", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-corrupt-" ) );
	const ddjName = "zzz_test_tile_05.ddj";
	try {
		await mkdir( path.join( extracted, "Map_extracted", "tile2d" ), { recursive: true } );
		// A distinct payload keeps this test's cache key private: corrupting a
		// shared key would poison every later fixture run.
		const source = fixtureDdj( { fill: 0x4b } );
		await writeFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), source );

		// Publish once so the cache entry exists, then corrupt it in place.
		const first = path.join( extracted, "out", "tile.texture" );
		await publishBlockTextureFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), first );
		const generator = sha256Hex(
			await readFile(
				path.join( import.meta.dirname, "..", "..", "build", "native_texture_mips.py" )
			)
		);
		const key = sha256Hex( generator + sha256Hex( source ) );
		const cached = path.join( generatedRoot, "intermediate", "block-texture-cache", key + ".texture" );
		assert.ok( await exists( cached ), "fixture cache entry exists" );
		await writeFile( cached, Buffer.from( "truncated" ) );

		const second = path.join( extracted, "out2", "tile.texture" );
		await assert.rejects(
			() => publishBlockTextureFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), second ),
			/Invalid block container/
		);
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});

/*
================
A ledger hit whose reference the probe no longer publishes

The builder trusts a recorded tile list only while every reference matches
the probe; the publisher refuses a stale reference instead of sweeping the
file the bundle still reads.
================
*/
test("a stale recorded reference is detected and never published under", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-ledger-hit-" ) );
	const ddjName = "zzz_test_tile_06.ddj";
	try {
		await mkdir( path.join( extracted, "Map_extracted", "tile2d" ), { recursive: true } );
		await writeFile( path.join( extracted, "Map_extracted", "tile2d", ddjName ), fixtureDdj( { fill: 0x5a } ) );
		const current = [ {
			ddjFileName: ddjName,
			sourcePath: "y",
			imagePublicPath: terrainTileTexturePublicPath( ddjName )
		} ];
		const stale = [ {
			ddjFileName: ddjName,
			sourcePath: "y",
			imagePublicPath: terrainTileImagePublicPath( ddjName )
		} ];
		assert.equal( await terrainTileReferencesCurrent( current, extracted ), true );
		assert.equal( await terrainTileReferencesCurrent( stale, extracted ), false );

		// The bundle still reads the PNG: publishing the container would sweep it.
		const referencedPng = publicPathToFile( terrainTileImagePublicPath( ddjName ), publicRoot );
		await mkdir( path.dirname( referencedPng ), { recursive: true } );
		await writeFile( referencedPng, Buffer.from( "still-referenced" ) );
		await assert.rejects( () => copyReferencedTerrainTileImages( stale, extracted ), /migrate the bundle first/ );
		assert.ok( await exists( referencedPng ), "the referenced PNG survives" );
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});

test("a scoped rebuild keeps shared tile and lightmap references usable in an untouched bundle", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-shared-" ) );
	try {
		const name = "zzz_shared_regions.ddj";
		const source = path.join( extracted, "Map_extracted", "tile2d", name );
		await mkdir( path.dirname( source ), { recursive: true } );
		await writeFile( source, fixtureDdj() );
		const tilePath = terrainTileImagePublicPath( name );
		const lightmapPath = terrainLightmapPublicPath( "__test-shared", 240, 241, false );
		for ( const publicPath of [ tilePath, lightmapPath ] ) {
			const file = publicPathToFile( publicPath, publicRoot );
			await mkdir( path.dirname( file ), { recursive: true } );
			await writeFile( file, Buffer.from( "previous published bytes" ) );
		}
		const bundle = {
			terrainTextures: {
				tileCatalog: { referencedTiles: [ { ddjFileName: name, imagePublicPath: tilePath } ] },
				sectors: [ { lightmapPublicPath: lightmapPath } ]
			}
		};
		const selected = path.join( extracted, "selected.json" );
		const untouched = path.join( extracted, "untouched.json" );
		const original = JSON.stringify( bundle );
		await writeFile( selected, original );
		await writeFile( untouched, original );
		await refreshCachedTerrainTileBundle( selected, extracted );
		const migrated = JSON.parse( await readFile( selected, "utf8" ) );
		migrated.terrainTextures.sectors[0].lightmapPublicPath = await publishTerrainLightmap(
			"__test-shared",
			240,
			241,
			fixtureDds()
		);
		await writeFile( selected, JSON.stringify( migrated ) );
		assert.equal( await readFile( untouched, "utf8" ), original );
		for ( const file of [ selected, untouched ] ) {
			const current = JSON.parse( await readFile( file, "utf8" ) );
			for (
				const publicPath of [
					current.terrainTextures.tileCatalog.referencedTiles[0].imagePublicPath,
					current.terrainTextures.sectors[0].lightmapPublicPath
				]
			) {
				assert.ok( await exists( publicPathToFile( publicPath, publicRoot ) ), `${file}: ${publicPath}` );
			}
		}
		assert.equal(
			migrated.terrainTextures.tileCatalog.referencedTiles[0].imagePublicPath,
			terrainTileTexturePublicPath( name )
		);
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});

test("a failed dependency publication preserves the persisted bundle and its earlier texture", async () => {
	const extracted = await mkdtemp( path.join( os.tmpdir(), "sro-block-failed-migration-" ) );
	try {
		const name = "zzz_failed_migration.ddj";
		const source = path.join( extracted, "Map_extracted", "tile2d", name );
		await mkdir( path.dirname( source ), { recursive: true } );
		// The header qualifies, but its incomplete authored block must fail
		// validation before the persisted bundle can advertise a new path.
		await writeFile( source, fixtureDdj().subarray( 0, DDJ_HEADER + DDS_HEADER + 4 ) );
		const tilePath = terrainTileImagePublicPath( name );
		const previous = publicPathToFile( tilePath, publicRoot );
		await mkdir( path.dirname( previous ), { recursive: true } );
		await writeFile( previous, Buffer.from( "previous published bytes" ) );
		const output = path.join( extracted, "region.json" );
		const original = JSON.stringify( {
			terrainTextures: { tileCatalog: { referencedTiles: [ { ddjFileName: name, imagePublicPath: tilePath } ] } }
		} );
		await writeFile( output, original );
		await assert.rejects( () => refreshCachedTerrainTileBundle( output, extracted ), /truncated authored level/ );
		assert.equal( await readFile( output, "utf8" ), original );
		assert.equal( await readFile( previous, "utf8" ), "previous published bytes" );
		assert.equal( await exists( publicPathToFile( terrainTileTexturePublicPath( name ), publicRoot ) ), false );
	} finally {
		await rm( extracted, { recursive: true, force: true } );
	}
});
