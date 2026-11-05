/*
===========================================================================

pet-heal-level.test.mjs - the pet window across heals, growth and training

#2564: potions on an attack pet changed nothing in its window while the
server held the pet full. The window divides the presented HP by the
max the held 0x3FB1 state carries (#1896), so every heal, level-up and
training change has to move both numbers together. This journey runs
the real gameplay reducer: restore push, spawn, damage, a potion, a
form change with its new max, damage again, then a training change and
a heal to the new max. The window text must equal the server's HP/max
at every step.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { petReference } = await import( "../../src/engine/foundation/ui/pet-training.ts" );
const { cosHpText } = await import( "../../src/engine/foundation/ui/cos-command.ts" );
const { OP_PET_TRAINING_STATE } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);

const GID = 0x2000001;

/*
================
statePayload

The server's 0x3FB1 layout with one skill and the version-1 stats tail
(maxHp, magic percents, primary flag), as in pet-crossing.test.mjs.
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
	bytes.push( 1, 1, 1, 15, 9 );
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

const u32le = n => [ n & 255, (n >>> 8) & 255, (n >>> 16) & 255, n >>> 24 ];

/*
================
spawnPayload

0x3158, band 3: gid, reference, hp, mp, experience, level 90, satiety,
command mode, an empty name, no bag, alive, bag slot 14.
================
*/
function spawnPayload( gid, hp ) {
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

/*
================
vitalsPayload

0x33A6 HP refresh: gid, source flags, the HP flag, the new HP.
================
*/
function vitalsPayload( gid, hp ) {
	return Uint8Array.from( [ ...u32le( gid ), 0, 0, 1, ...u32le( hp ) ] );
}

test("the pet window tracks the server across heals, a level-up and a training change (#2564)", () => {
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
	// The pet window's own read (ui.ts): HP over petReference of the held state.
	const windowText = snapshot => {
		const record = defined( snapshot?.cosRecords?.find( r => r.gid === GID ) );
		const published = snapshot?.petTraining?.gid === record.gid ? snapshot.petTraining : undefined;
		return cosHpText( record, petReference( catalog, published, undefined, 0, record.level ?? 0 ) );
	};
	// The login restore push (#1899), then the pet's record at full HP.
	game.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( GID, 8100 ) }, 1000 );
	game.receive( { opcode: 0x3158, payload: spawnPayload( GID, 8100 ) }, 1001 );
	assert.equal( windowText( game.take() ), "8100/8100 (100%)" );
	// Damage, then a potion: the refresh and the state push agree.
	game.receive( { opcode: 0x33a6, payload: vitalsPayload( GID, 3000 ) }, 2000 );
	assert.equal( windowText( game.take() ), "3000/8100 (37%)" );
	game.receive( { opcode: 0x33a6, payload: vitalsPayload( GID, 8100 ) }, 3000 );
	game.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( GID, 8100 ) }, 3001 );
	assert.equal( windowText( game.take() ), "8100/8100 (100%)", "the potion reads in the window" );
	// A level-up between heals: the form swap (0x3508 case 7), the new
	// form's max in a fresh state push, and growth's refill refresh.
	game.receive(
		{ opcode: 0x3508, payload: Uint8Array.from( [ ...u32le( GID ), 7, ...u32le( 104 ) ] ) },
		4000
	);
	game.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( GID, 8500 ) }, 4001 );
	game.receive( { opcode: 0x33a6, payload: vitalsPayload( GID, 8500 ) }, 4002 );
	assert.equal( windowText( game.take() ), "8500/8500 (100%)", "the new form's max replaces the old" );
	// Damage again, then a training change raises the max and a heal fills it.
	game.receive( { opcode: 0x33a6, payload: vitalsPayload( GID, 4000 ) }, 5000 );
	assert.equal( windowText( game.take() ), "4000/8500 (47%)" );
	game.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( GID, 9000 ) }, 5001 );
	game.receive( { opcode: 0x33a6, payload: vitalsPayload( GID, 9000 ) }, 5002 );
	assert.equal( windowText( game.take() ), "9000/9000 (100%)", "the trained max replaces the grown one" );
	game.dispose();
});
