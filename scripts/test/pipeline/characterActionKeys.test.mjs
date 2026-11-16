/*
===========================================================================

characterActionKeys.test.mjs - what moves a character model's action key

Plan slice B2 (buildRoster.mjs, buildEquipmentVisuals.mjs). Two inputs of
these actions are chosen outside them, from tables no action reads, so
they must be parameters of the key: a roster model's attached-motion
requests (skilleffect.txt under the tomb posture) and an equipment item
set's donor body (the roster textdata). A temp store with fixed code
hashes keys the actions; no game data is read.

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
import { rosterModelAction } from "../../build/char/buildRoster.mjs";
import { itemSetAction } from "../../build/char/buildEquipmentVisuals.mjs";

const MAN = { codename: "CHAR_CH_MAN_ADVENTURER", bsrPath: "res/char/china/chinaman_adventurer.bsr", isMount: false };
const OTHER_MAN = { codename: "CHAR_CH_MAN_SOLDIER", bsrPath: "res/char/china/chinaman_soldier.bsr", isMount: false };

/*
================
keyer

A temp store whose code hashes are fixed; returns spec -> key.
================
*/
async function keyer( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-char-keys-" ) );
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

/*
================
itemSet
================
*/
function itemSet( donor ) {
	return {
		key: "CH_M_0123456789abcdef0123",
		body: "CH_M",
		donor,
		paths: [ "res/item/china/man/ch_m_heavy_01_ba_a.bsr" ],
		held: false
	};
}

test("a roster model's key moves with its attached-motion requests", async ( t ) => {
	const key = await keyer( t );
	const walk = [ { id: 188, set: "default", role: "attached-default-188" } ];
	const more = [ ...walk, { id: 189, set: "default", role: "attached-default-189" } ];
	assert.equal( await key( rosterModelAction( MAN, walk ) ), await key( rosterModelAction( MAN, walk ) ) );
	assert.notEqual( await key( rosterModelAction( MAN, walk ) ), await key( rosterModelAction( MAN, more ) ) );
});

test("an equipment set's key moves with its donor body", async ( t ) => {
	const key = await keyer( t );
	const donorWithReads = () => {
		throw new Error( "keying never loads a donor" );
	};
	const same = await key( itemSetAction( itemSet( MAN ), donorWithReads ) );
	assert.equal( await key( itemSetAction( itemSet( { ...MAN } ), donorWithReads ) ), same );
	// The roster textdata moved CH_M to another model: the files the old
	// donor read are unchanged, so only the key can tell.
	assert.notEqual( await key( itemSetAction( itemSet( OTHER_MAN ), donorWithReads ) ), same );
});
