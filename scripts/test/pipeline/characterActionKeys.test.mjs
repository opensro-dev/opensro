/*
===========================================================================

characterActionKeys.test.mjs - what moves a character model's action key

Plan slice B2 (buildRoster.mjs, buildEquipmentVisuals.mjs,
buildNpcModelAssets.mjs). Some inputs of these actions are chosen outside
them, from tables no action reads, so they must be parameters of the key:
a roster model's attached-motion requests (skilleffect.txt under the tomb
posture), an equipment item set's donor body (the roster textdata), and
an NPC resource bake's attached motions, monster weapon-skill states,
tomb posture and material-variant plan (bakeTables, materialVariantSlots).
A temp store with fixed code hashes keys the actions; no game data is read.

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
import { npcResourceAction } from "../../build/char/npcResourceBake.mjs";

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

const PUBLIC_ROOT = path.resolve( "public-assets" );
const WALK = [ { id: 188, set: "default", role: "attached-default-188" } ];

/*
================
npcRequest

One resource bake request under PUBLIC_ROOT; the paths are only keyed.
================
*/
function npcRequest( bsrPath, isMob, variantSlots = [] ) {
	const stem = bsrPath.replace( /\.\w+$/, "" );
	const output = { diskPath: path.join( PUBLIC_ROOT, "npc", `${stem}.glb` ), publicPath: `/npc/${stem}.glb` };
	return { bsrPath, output, isMob, variantSlots, publicAssetsRoot: PUBLIC_ROOT };
}

/*
================
npcTables

bakeTables' shape with chosen values.
================
*/
function npcTables(
	{ attachedMotionRequests = WALK, skillStates = [ [ "sword", [ 3, 1 ] ] ], tombCombat = false } = {}
) {
	return { attachedMotionRequests, skillStates: new Map( skillStates ), tombCombat };
}

const MOB_BSR = "res/char/npc/mob_ch_tiger.bsr";
const NPC_BSR = "res/char/npc/npc_ch_smith.bsr";
const CPD = "res/char/npc/shinmu.cpd";

test("an NPC bake's key moves with its attached motions and variant plan", async ( t ) => {
	const key = await keyer( t );
	const base = await key( npcResourceAction( npcRequest( MOB_BSR, true, [ 1 ] ), npcTables() ) );
	assert.equal( await key( npcResourceAction( npcRequest( MOB_BSR, true, [ 1 ] ), npcTables() ) ), base );
	const more = [ ...WALK, { id: 189, set: "default", role: "attached-default-189" } ];
	assert.notEqual(
		await key(
			npcResourceAction( npcRequest( MOB_BSR, true, [ 1 ] ), npcTables( { attachedMotionRequests: more } ) )
		),
		base
	);
	// A BMT appeared for slot 2: the BSR is unchanged, the outputs are not.
	assert.notEqual( await key( npcResourceAction( npcRequest( MOB_BSR, true, [ 1, 2 ] ), npcTables() ) ), base );
});

test("weapon-skill states key a monster or compound bake, never a plain NPC", async ( t ) => {
	const key = await keyer( t );
	const other = npcTables( { skillStates: [ [ "sword", [ 1, 3, 4 ] ] ] } );
	const mob = await key( npcResourceAction( npcRequest( MOB_BSR, true ), npcTables() ) );
	assert.notEqual( await key( npcResourceAction( npcRequest( MOB_BSR, true ), other ) ), mob );
	const cpd = await key( npcResourceAction( npcRequest( CPD, false ), npcTables() ) );
	assert.notEqual( await key( npcResourceAction( npcRequest( CPD, false ), other ) ), cpd );
	// Only the order differs: the same states, the same key.
	const reordered = npcTables( { skillStates: [ [ "sword", [ 1, 3 ] ] ] } );
	assert.equal( await key( npcResourceAction( npcRequest( MOB_BSR, true ), reordered ) ), mob );
	const npc = await key( npcResourceAction( npcRequest( NPC_BSR, false ), npcTables() ) );
	assert.equal( await key( npcResourceAction( npcRequest( NPC_BSR, false ), other ) ), npc );
});

test("the tomb posture keys a compound bake only", async ( t ) => {
	const key = await keyer( t );
	const tomb = npcTables( { tombCombat: true } );
	assert.notEqual(
		await key( npcResourceAction( npcRequest( CPD, false ), tomb ) ),
		await key( npcResourceAction( npcRequest( CPD, false ), npcTables() ) )
	);
	assert.equal(
		await key( npcResourceAction( npcRequest( MOB_BSR, true ), tomb ) ),
		await key( npcResourceAction( npcRequest( MOB_BSR, true ), npcTables() ) )
	);
});
