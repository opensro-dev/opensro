/*
===========================================================================

npc-click-refusal.test.mjs - tests for npc-click-refusal.ts and its gate in
gameplay.ts: a ground click next to an NPC does not walk

CNavigationDeadreckon_StartGroundMove (878600) returns without a walk when
an NPC or guard stands within 20 units (3D, distance squared below 400) of
the clicked point. Monsters, pets and players never refuse.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { isNpcClass, npcRefusesWalk } = await import( "../../src/engine/foundation/gameplay/npc-click-refusal.ts" );

const LOCAL_GID = 7;
const REGION = 257;
// Type words: IsNPC (808A80) and the guard test, then the classes that never refuse.
const NPC_TID = 0x146, GUARD_TID = 0x244, MONSTER_TID = 0xc6, COS_TID = 0x1c6, PLAYER_TID = 0x26;

/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 1907,
	kind: "local-player",
	name: "walker",
	regionId: REGION,
	x: 100,
	y: 0,
	z: 100,
	heading: 0
};

/*
================
standing

An entity of tidWord standing at x, z (y 0) in REGION.
================
*/
function standing( tidWord, x, z, kind = "npc", regionId = REGION ) {
	return { gid: 100 + x, refObjId: 2000, kind, name: "n", regionId, x, y: 0, z, heading: 0, tidWord };
}

/*
================
clickMove

Seeds the local player, hands gameplay the entity table, issues a move to
(x, 0, z) and reports whether a walk started (a frame went out and the
ground marker shows).
================
*/
function clickMove( x, z, table ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const game = createGameplay( f => sent.push( f ) );
	game.bootstrap( { simulationProtocolVersion: 1, character: { skills: [] }, refSkillSnapshot: [] } );
	game.bindReferences( {
		country: () => undefined,
		playerModels: () => [],
		item: () => undefined,
		monster: () => undefined,
		npcs: () => table
	} );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	sent.length = 0;
	game.take();
	const frame = game.command(
		{ kind: "move", destination: { regionId: REGION, x, y: 0, z, angle: 0 } },
		0,
		undefined,
		local
	);
	const marker = game.take()?.selectionDecal;
	game.dispose();
	return { walked: frame !== null && sent.length > 0, marker: marker?.kind === "ground" };
}

test("only IsNPC types and guard types are CICNPC kinds", () => {
	assert.equal( isNpcClass( NPC_TID ), true );
	assert.equal( isNpcClass( GUARD_TID ), true );
	// IsNPC tests bit 1; the guard test does not.
	assert.equal( isNpcClass( NPC_TID & ~2 ), false );
	assert.equal( isNpcClass( GUARD_TID | 2 ), true );
	for ( const tid of [ MONSTER_TID, COS_TID, PLAYER_TID, 0x2c6, 0 ] ) assert.equal( isNpcClass( tid ), false, String( tid ) );
});

test("the refusal is strictly below 20 units, in 3D", () => {
	const at = { regionId: REGION, x: 300, y: 0, z: 300, angle: 0 };
	assert.equal( npcRefusesWalk( at, [ standing( NPC_TID, 319, 300 ) ] ), true );
	assert.equal( npcRefusesWalk( at, [ standing( NPC_TID, 320, 300 ) ] ), false, "exactly 20 units walks" );
	// 12 across and 16 up is 20 in 3D: a flat test would refuse it.
	assert.equal( npcRefusesWalk( { ...at, y: 16 }, [ standing( NPC_TID, 312, 300 ) ] ), false );
	assert.equal( npcRefusesWalk( { ...at, y: 15 }, [ standing( NPC_TID, 312, 300 ) ] ), true );
});

test("an NPC across a region border refuses by its world distance", () => {
	// Region 258 is the next sector east: its x 5 is 1925 + 5 units past
	// region 257's x 1915 (sectors are 1920 wide).
	const at = { regionId: REGION, x: 1915, y: 0, z: 300, angle: 0 };
	assert.equal( npcRefusesWalk( at, [ standing( NPC_TID, 5, 300, "npc", REGION + 1 ) ] ), true );
	assert.equal( npcRefusesWalk( at, [ standing( NPC_TID, 25, 300, "npc", REGION + 1 ) ] ), false );
});

test("a click at an NPC's feet starts no walk and draws no marker", () => {
	assert.deepEqual( clickMove( 300, 300, [ standing( NPC_TID, 310, 300 ) ] ), { walked: false, marker: false } );
	assert.deepEqual( clickMove( 300, 300, [ standing( GUARD_TID, 300, 310 ) ] ), { walked: false, marker: false } );
});

test("a click just clear of an NPC, or beside a monster or pet, walks", () => {
	assert.deepEqual( clickMove( 300, 300, [ standing( NPC_TID, 321, 300 ) ] ), { walked: true, marker: true } );
	assert.deepEqual( clickMove( 300, 300, [ standing( MONSTER_TID, 305, 300, "monster" ) ] ), {
		walked: true,
		marker: true
	} );
	assert.deepEqual( clickMove( 300, 300, [ standing( COS_TID, 305, 300, "cos" ) ] ), { walked: true, marker: true } );
});
