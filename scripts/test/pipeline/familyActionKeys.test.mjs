/*
===========================================================================

familyActionKeys.test.mjs - what moves a loose family's action key

Plan slice B2 (families/looseFamilies.mjs). The dungeon-worlds action
declares its region outputs before it runs, so the sector list read from
dungeoninfo.txt ahead of it is a parameter of its key: a new dungeon row
must miss even though no file the old build read changed. A temp store
with fixed code hashes keys the action; no game data is read.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { defineAction } from "../../build/graph/action.mjs";
import { keyOf, openStore } from "../../build/graph/store.mjs";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";
import { dungeonRegionPublicPath, dungeonWorldsAction } from "../../build/world/assets/dungeonWorldsAction.mjs";

const ROOT = path.resolve( "public" );

/*
================
keyer

A temp store whose code hashes are fixed; returns spec -> key.
================
*/
async function keyer( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-family-keys-" ) );
	const store = await openStore( {
		root: path.join( root, "store" ),
		hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) ),
		codeHashOf: async () => "code-1"
	} );
	t.after( async () => {
		await store.shutdown();
		await rm( root, { recursive: true, force: true } );
	} );
	return spec => keyOf( store, defineAction( spec ), {} );
}

test("the dungeon-worlds key moves with the admitted sectors", async ( t ) => {
	const key = await keyer( t );
	const sectors = [ 0x8001, 0x8002 ];
	const same = await key( dungeonWorldsAction( sectors, ROOT ) );
	assert.equal( await key( dungeonWorldsAction( [ ...sectors ], ROOT ) ), same );
	assert.notEqual( await key( dungeonWorldsAction( [ ...sectors, 0x8003 ], ROOT ) ), same );
});

test("the dungeon-worlds action declares the manifest and one bundle per sector", () => {
	const action = defineAction( dungeonWorldsAction( [ 0x8002, 0x8001 ], ROOT ) );
	assert.deepEqual( action.outputs, [
		"assets/world/dungeon/dungeon-resources.json",
		"assets/world/dungeon/regions/0x8001.json",
		"assets/world/dungeon/regions/0x8002.json"
	] );
	assert.equal( dungeonRegionPublicPath( 0x800a ), "/assets/world/dungeon/regions/0x800a.json" );
});
