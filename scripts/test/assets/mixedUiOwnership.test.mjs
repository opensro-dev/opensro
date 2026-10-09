/*
===========================================================================

mixedUiOwnership.test.mjs - new mixed-family images match full UI ownership

The real preload generator supplies the full-build oracle. Focused fallback
sees an index plus incoming files, including siblings no longer loose.

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
import { buildUiImagePreloadManifest } from "../../build/uiImagePreload.mjs";
import { LOOSE_FAMILIES } from "../../build/families/looseFamilies.mjs";

const IMAGE_ROOT = "/assets/images/Media_extracted/";
const INTERFACE = IMAGE_ROOT + "interface/pet/pt_edge_effect.png";
const ICON = IMAGE_ROOT + "icon/buf_effect.png";
const NORMAL = IMAGE_ROOT + "icon/stateodd/button.png";
const STATE = IMAGE_ROOT + "icon/stateodd/button_focus.png";
const ORPHAN = IMAGE_ROOT + "icon/stateodd/ordinary.png";
const TEXTURE = IMAGE_ROOT + "interface/pet/native.texture";
const EXISTING = IMAGE_ROOT + "interface/pet/pt_life_effect.png";
const CHILD_FLAG = "--publish-fixture";
const CHILD_TIMEOUT_MS = 60000;
const run = promisify( execFile );

/*
================
publishFixture

Zero-byte image members exercise ownership and verified reads without using
the shared member-compression cache. All publication outputs are isolated.
================
*/
async function publishFixture() {
	const { CLIENT_PUBLIC_ROOT, GENERATED_ROOT } = await import( "../../lib/generatedRoot.mjs" );
	const { buildAssetPacks } = await import( "../../build/assetPacks.mjs" );
	const { PACK_INDEX_PATH } = await import( "../../build/shared/packGroupRefresh.mjs" );
	const { publishLooseFamily } = await import( "../../build/shared/looseFamilyPublication.mjs" );
	const { readPackedAssetBytesSync } = await import( "../../lib/publishedAsset.mjs" );
	const files = [ INTERFACE, ICON, NORMAL, STATE, EXISTING ];
	for ( const file of files ) {
		const filename = path.join( CLIENT_PUBLIC_ROOT, file.slice( 1 ) );
		await mkdir( path.dirname( filename ), { recursive: true } );
		await writeFile( filename, Buffer.alloc( 0 ) );
	}
	await buildAssetPacks( {
		publicRoot: CLIENT_PUBLIC_ROOT,
		outputRoot: path.dirname( PACK_INDEX_PATH ),
		hashCachePath: path.join( GENERATED_ROOT, "hashes.json" ),
		memberCacheRoot: path.join( GENERATED_ROOT, "members" ),
		groups: [
			{ name: "native-ui", load: "startup", files: [] },
			{ name: "ui-icons", load: "startup", files: [] },
			{ name: "game-images", load: "startup", files: [ EXISTING ] }
		]
	} );
	const before = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	await publishLooseFamily( {
		name: "mixed-ui-fixture",
		owner: "mixed-ui-fixture",
		files,
		defaultGroup: LOOSE_FAMILIES["slot-effect"].defaultGroup
	} );
	const after = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	const expected = new Map( [
		[ INTERFACE, "native-ui" ],
		[ ICON, "ui-icons" ],
		[ NORMAL, "native-ui" ],
		[ STATE, "native-ui" ],
		[ EXISTING, "game-images" ]
	] );
	assert.equal( after.assets.length, files.length );
	assert.deepEqual( new Map( after.assets.map( row => [ row.path, row.group ] ) ), expected );
	assert.deepEqual(
		after.assets.find( row => row.path === EXISTING ),
		before.assets.find( row => row.path === EXISTING )
	);
	for ( const file of files ) {
		assert.deepEqual( readPackedAssetBytesSync( file, CLIENT_PUBLIC_ROOT ), Buffer.alloc( 0 ), file );
	}
	assert.ok( after.groups.every( group => group.load === "startup" ) );
}

/*
================
fullPreload
================
*/
async function fullPreload( t, files ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-mixed-ui-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	for ( const file of files ) {
		const filename = path.join( root, file.slice( 1 ) );
		await mkdir( path.dirname( filename ), { recursive: true } );
		await writeFile( filename, "image fixture" );
	}
	return buildUiImagePreloadManifest( {
		publicRoot: root,
		imageRoot: path.join( root, "assets/images" ),
		targetPath: path.join( root, "preload.json" )
	} );
}

if ( process.argv[2] === CHILD_FLAG ) {
	await publishFixture();
} else {
	test("mixed UI fallbacks agree with full preload for new interface, state, normal and ordinary images", async t => {
		const files = [ INTERFACE, ICON, NORMAL, STATE, ORPHAN, TEXTURE ];
		const full = await fullPreload( t, files );
		assert.deepEqual(
			new Map( full.images.map( row => [ row.path, row.reason ] ) ),
			new Map( [
				[ INTERFACE, "native-interface" ],
				[ NORMAL, "interactive-normal" ],
				[ STATE, "interactive-state" ]
			] )
		);
		const expected = new Map( [
			[ INTERFACE, "native-ui" ],
			[ ICON, "ui-icons" ],
			[ NORMAL, "native-ui" ],
			[ STATE, "native-ui" ],
			[ ORPHAN, "ui-icons" ],
			[ TEXTURE, "game-images" ]
		] );
		for ( const family of [ "slot-effect", "overlay" ] ) {
			for ( const file of files ) {
				assert.equal(
					LOOSE_FAMILIES[family].defaultGroup( file, { assets: [] }, files ),
					expected.get( file ),
					`${family}: ${file}`
				);
			}
		}
		assert.equal(
			LOOSE_FAMILIES["slot-effect"].defaultGroup( "/assets/cif/cif-sprite-catalog.json", {}, files ),
			"game-data"
		);
	});

	test("focused normal companions use packed-only state inventory and incoming state inventory", async t => {
		const full = await fullPreload( t, [ NORMAL, STATE ] );
		assert.equal( full.images.find( row => row.path === NORMAL )?.reason, "interactive-normal" );
		for ( const family of [ "slot-effect", "overlay" ] ) {
			const group = LOOSE_FAMILIES[family].defaultGroup;
			assert.equal(
				group( NORMAL, { assets: [ { path: STATE.toUpperCase(), group: "native-ui" } ] }, [ NORMAL ] ),
				"native-ui"
			);
			assert.equal( group( NORMAL, { assets: [] }, [ NORMAL, STATE ] ), "native-ui" );
			assert.equal( group( NORMAL, { assets: [] }, [ NORMAL ] ), "ui-icons" );
		}
	});

	test("actual mixed-family publication assigns new images and preserves an existing noncanonical owner", async t => {
		const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-mixed-ui-publish-" ) );
		t.after( async () => {
			assert.equal( path.dirname( temporary ), path.resolve( os.tmpdir() ) );
			await rm( temporary, { recursive: true, force: true } );
		} );
		await run( process.execPath, [ fileURLToPath( import.meta.url ), CHILD_FLAG ], {
			env: {
				...process.env,
				SRO_GENERATED_ROOT: temporary,
				SRO_BUILD_HASH_CACHE: "0",
				SRO_ASSET_PACK_BASELINE: "",
				SRO_BUILD_JOBS: "1"
			},
			timeout: CHILD_TIMEOUT_MS,
			windowsHide: true
		} );
	});
}
