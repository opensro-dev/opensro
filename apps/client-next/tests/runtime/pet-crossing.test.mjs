/*
===========================================================================

pet-crossing.test.mjs - the pet window's max across a world crossing

#2564: a level-90 wolf at its server max (8100 of a 39434 catalog row)
read 8100/39434 (20%) in the pet window. That reading is the no-state
fallback, reached only while no 0x3FB1 is held. This journey holds the
state the server's login restore push sends (#1899), crosses regions
the way core.ts does (a 0x3369 reset into gameplay.resetWorld) and
re-spawns the pet's 0x3158 record; the window must keep dividing by
the held max. Only a session teardown (core.clear -> clearState)
drops the state, and the restore push replaces it there.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { resetTravelRegion } = await import( "../../src/engine/foundation/gameplay/travel.ts" );
const { petReference } = await import( "../../src/engine/foundation/ui/pet-training.ts" );
const { cosHpText } = await import( "../../src/engine/foundation/ui/cos-command.ts" );
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );
const { OP_PET_TRAINING_STATE } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);

const wolf = PET_DESIGN.treeSkills.find( s => s.pet === "Wolf" && s.row === 0 && s.status === "Implemented" );
if ( !wolf ) throw new Error( "Missing implemented wolf fixture" );
const WOLF_SKILL = wolf.id;

/*
================
statePayload

The server's 0x3FB1 layout, the one skill this journey needs: gid,
points, percents, two rows, slotted, skills, no gear, then the stats
tail (version 1: maxHp, magic percents, primary flag).
================
*/
function statePayload( gid, maxHp ) {
	const bytes = [];
	const u16 = v => bytes.push( v & 0xff, v >> 8 );
	const u32 = v => bytes.push( v & 0xff, (v >> 8) & 0xff, (v >> 16) & 0xff, v >>> 24 );
	u32( gid );
	bytes.push( 3 );
	u16( 109 );
	u16( 103 );
	u32( 0 );
	u16( 0 );
	bytes.push( 0 );
	u32( 0 );
	u16( 0 );
	bytes.push( 0 );
	bytes.push( 0, 0, 0 );
	bytes.push( 1 );
	bytes.push( WOLF_SKILL, 1, 1, 15, 9 );
	u16( 50 );
	u16( 60 );
	bytes.push( 0 );
	bytes.push( 1 );
	u32( maxHp );
	u16( 100 );
	u16( 100 );
	bytes.push( 0 );
	return Uint8Array.from( bytes );
}

/*
================
spawnPayload

0x3158, band 3: gid, reference, hp, mp, experience, level 90, satiety,
command mode, an empty name, no bag, alive, bag slot 14.
================
*/
function spawnPayload( gid, hp ) {
	const u32le = n => [ n & 255, (n >>> 8) & 255, (n >>> 16) & 255, n >>> 24 ];
	return Uint8Array.from( [
		...u32le( gid ),
		...u32le( 103 ),
		...u32le( hp ),
		...u32le( 0 ),
		...u32le( 0 ),
		...u32le( 0 ),
		90,
		16,
		39,
		...u32le( 0 ),
		0,
		0,
		0,
		...u32le( 0 ),
		14
	] );
}

test("a held 0x3FB1 max survives a travel reset and the pet's re-spawn (#2564)", () => {
	const GID = 0x2000001;
	const catalog = {
		maxHp: 39434,
		physicalDefence: 100,
		magicalDefence: 100,
		icon: "",
		rideable: false,
		skills: [],
		hit: 100,
		parry: 100
	};
	const game = createGameplay( () => {} );
	game.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 103, tidWord: 3 << 11 | 0x1c6 } ]
	} );
	game.seed( { gid: 1, refObjId: 1907, kind: "player", name: "Player", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	// The login restore push (#1899) holds an effective max of 8100,
	// which the window must keep instead of the catalog's 39434.
	game.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( GID, 8100 ) }, 1000 );
	// The pet window's own read (ui.ts): the published state merges only
	// under the record's gid; the text divides HP by the merged max.
	const windowText = snapshot => {
		const record = snapshot?.cosRecords?.find( r => r.gid === GID );
		if ( !record ) return undefined;
		const published = snapshot?.petTraining?.gid === record.gid ? snapshot.petTraining : undefined;
		return cosHpText( record, petReference( catalog, published, undefined, 0, record.level ?? 0 ) );
	};
	game.receive( { opcode: 0x3158, payload: spawnPayload( GID, 8100 ) }, 1001 );
	const before = game.take();
	assert.equal( before?.petTraining?.stats?.maxHp, 8100, "the restore push is held" );
	assert.equal( windowText( before ), "8100/8100 (100%)" );
	// The reported reading is what the same window shows with no held
	// state: the catalog fallback, not a crossing's doing.
	const record = defined( before?.cosRecords?.find( r => r.gid === GID ) );
	assert.equal( cosHpText( record, petReference( catalog, undefined ) ), "8100/39434 (20%)" );
	// A crossing: core.ts answers a reset frame with gameplay.resetWorld()
	// and the pet's record re-spawns on the far side under the same gid.
	assert.equal( resetTravelRegion( { opcode: 0x3369, payload: Uint8Array.of( 0x6f, 0x5c ) } ), 0x5c6f );
	game.resetWorld();
	game.receive( { opcode: 0x3158, payload: spawnPayload( GID, 8100 ) }, 2000 );
	const after = game.take();
	assert.equal( after?.petTraining?.stats?.maxHp, 8100, "the held state survived the reset" );
	assert.equal( windowText( after ), "8100/8100 (100%)" );
	game.dispose();
});
