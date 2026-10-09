/*
===========================================================================

worldMapPackOwnership.test.mjs - mixed world-map ownership survives refresh

Runs the actual pack publication boundary in an isolated generated root.
The legacy single-group request must fail; the owner-aware request preserves
both groups, updated bytes, and compacted unrelated members.

===========================================================================
*/
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const run = promisify( execFile );
const UI_IMAGE = "/assets/images/Media_extracted/interface/worldmap/map/map_worldmap.png";
const GAME_IMAGE = "/assets/images/Media_extracted/interface/worldmap/map/map_region.png";
const UNRELATED = "/assets/images/retained.png";

/*
================
runFixture
================
*/
async function runFixture( legacy ) {
	const { CLIENT_PUBLIC_ROOT, GENERATED_ROOT } = await import( "../../lib/generatedRoot.mjs" );
	const { buildAssetPacks } = await import( "../../build/assetPacks.mjs" );
	const { PACK_INDEX_PATH, refreshPackGroups } = await import( "../../build/shared/packGroupRefresh.mjs" );
	const { refreshOwnedPackFiles } = await import( "../../build/shared/ownedPackRefresh.mjs" );
	const { readPackedAssetBytesSync } = await import( "../../lib/publishedAsset.mjs" );
	for ( const file of [ UI_IMAGE, GAME_IMAGE, UNRELATED ] ) {
		const target = path.join( CLIENT_PUBLIC_ROOT, file.slice( 1 ) );
		await mkdir( path.dirname( target ), { recursive: true } );
		await writeFile( target, "original" );
	}
	await buildAssetPacks( {
		publicRoot: CLIENT_PUBLIC_ROOT,
		outputRoot: path.dirname( PACK_INDEX_PATH ),
		hashCachePath: path.join( GENERATED_ROOT, "hashes.json" ),
		memberCacheRoot: path.join( GENERATED_ROOT, "members" ),
		groups: [
			{ name: "native-ui", load: "startup", files: [ UI_IMAGE ] },
			{ name: "game-images", load: "startup", files: [ GAME_IMAGE, UNRELATED ] }
		]
	} );
	const before = await readFile( PACK_INDEX_PATH );
	await rm( path.join( CLIENT_PUBLIC_ROOT, UNRELATED.slice( 1 ) ) );
	for ( const file of [ UI_IMAGE, GAME_IMAGE ] ) {
		await writeFile( path.join( CLIENT_PUBLIC_ROOT, file.slice( 1 ) ), Buffer.alloc( 0 ) );
	}
	if ( legacy ) {
		await assert.rejects(
			refreshPackGroups( {
				name: "world-map",
				deltas: [ { groupName: "game-images", startup: true, files: [ UI_IMAGE, GAME_IMAGE ] } ]
			} ),
			/repeats asset|publication closure/
		);
		assert.deepEqual( await readFile( PACK_INDEX_PATH ), before );
		return;
	}
	const result = await refreshOwnedPackFiles( {
		name: "world-map",
		startup: true,
		files: [ UI_IMAGE, GAME_IMAGE ]
	} );
	assert.equal( result.updates.length, 2 );
	const after = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	assert.deepEqual( after.assets.filter( a => a.path === UI_IMAGE ).map( a => a.group ), [ "native-ui" ] );
	assert.deepEqual( after.assets.filter( a => a.path === GAME_IMAGE ).map( a => a.group ), [ "game-images" ] );
	for ( const file of [ UI_IMAGE, GAME_IMAGE ] ) {
		assert.deepEqual( readPackedAssetBytesSync( file, CLIENT_PUBLIC_ROOT ), Buffer.alloc( 0 ) );
	}
	assert.equal( readPackedAssetBytesSync( UNRELATED, CLIENT_PUBLIC_ROOT ).toString(), "original" );
	assert.ok( after.groups.every( group => group.load === "startup" ) );
	const committed = await readFile( PACK_INDEX_PATH );
	await assert.rejects(
		refreshOwnedPackFiles( {
			name: "world-map",
			startup: true,
			files: [ "/assets/images/unowned.png" ]
		} ),
		/has no asset-pack owner/
	);
	assert.deepEqual( await readFile( PACK_INDEX_PATH ), committed );
}

if ( process.argv[2] === "--fixture" ) {
	await runFixture( process.argv[3] === "legacy" );
} else {
	for ( const mode of [ "legacy", "owned" ] ) {
		test(`world-map mixed owners: ${mode}`, async t => {
			const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-world-map-owners-" ) );
			t.after( async () => {
				assert.equal( path.dirname( temporary ), path.resolve( os.tmpdir() ) );
				await rm( temporary, { recursive: true, force: true } );
			} );
			await run( process.execPath, [ fileURLToPath( import.meta.url ), "--fixture", mode ], {
				env: {
					...process.env,
					SRO_GENERATED_ROOT: temporary,
					SRO_BUILD_HASH_CACHE: "0",
					SRO_ASSET_PACK_BASELINE: "",
					SRO_BUILD_JOBS: "1"
				},
				timeout: 60000,
				windowsHide: true
			} );
		});
	}
}
