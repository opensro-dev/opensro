/*
===========================================================================

runtimeTextAssets.test.mjs - raw runtime tables survive pack collection

Synthetic public trees exercise the real collector and pack writer without
licensed data. Unrelated text is excluded and native bytes stay unchanged.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { collectAssetPackGroups } from "../../build/assetPackGroups.mjs";
import { buildAssetPacks } from "../../build/assetPacks.mjs";
import { REQUIRED_RUNTIME_TEXT_ASSETS, requireRuntimeTextAssets } from "../../build/assetPackOwnership.mjs";
import { decodeStoredMember, parsePackHeader, storedMemberBytes } from "../../build/shared/packFormat.mjs";

/*
================
TestRuntimeTextPacking
================
*/
test("canonical collection packs required raw tables exactly once and preserves their bytes", async t => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-runtime-text-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const publicRoot = path.join( root, "public" );
	for ( const directory of [ "char/vat", "npc/vat", "anim", "audio", "textdata", "config" ] ) {
		await mkdir( path.join( publicRoot, "assets", directory ), { recursive: true } );
	}
	const bytes = Buffer.from( [ 0xb0, 0xa1, 0x0d, 0x0a, 0x31, 0x09, 0x32 ] );
	for (
		const name of [
			...REQUIRED_RUNTIME_TEXT_ASSETS,
			"/assets/config/unrelated.txt",
			"/assets/textdata/unrelated.txt"
		]
	) {
		await writeFile( path.join( publicRoot, name.slice( 1 ) ), bytes );
	}
	const { groups } = await collectAssetPackGroups( {
		publicRoot,
		uiImagePreloadPaths: [],
		missionMinimapTilePaths: [],
		includeOutdoorWorld: false
	} );
	requireRuntimeTextAssets( groups.flatMap( group => group.files ) );
	for ( const name of REQUIRED_RUNTIME_TEXT_ASSETS ) {
		assert.deepEqual( groups.filter( group => group.files.includes( name ) ).map( group => group.name ), [
			"game-data"
		] );
	}
	assert.ok( !groups.some( group => group.files.some( name => name.endsWith( "/unrelated.txt" ) ) ) );
	const result = await buildAssetPacks( {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets/packs/fixture" ),
		groups,
		hashCachePath: path.join( root, "hash-cache.json" ),
		memberCacheRoot: path.join( root, "member-cache" )
	} );
	const index = JSON.parse( await readFile( result.outputPath, "utf8" ) );
	for ( const name of REQUIRED_RUNTIME_TEXT_ASSETS ) {
		const member = index.assets.find( row => row.path === name );
		const packed = await readFile( path.join( publicRoot, member.packPath.slice( 1 ) ) );
		const { dataStart } = parsePackHeader( packed, member.packPath );
		assert.deepEqual(
			decodeStoredMember( storedMemberBytes( packed, dataStart, member, member.packPath ), member ),
			bytes
		);
	}
});
