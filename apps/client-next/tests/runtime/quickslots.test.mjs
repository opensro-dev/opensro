/*
===========================================================================

quickslots.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/" + file ).href );
}
const { skillBindings, quickSlotPacket } = await load( "foundation/gameplay/quickslots.ts" );
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { quickSlotCommand, quickSlotItemSlot, actionEmote } = await load( "foundation/gameplay/quickslots.ts" );
const { itemActivation, equipmentSocket } = await load( "foundation/gameplay/item-activation.ts" );
test("equipment activation shares native sockets and uses inventory authority, never item-use", () => {
	const ring = { slot: 20, typeFlags: 0x2c | (5 << 7) | (3 << 11) },
		inventory = [ ring, { slot: 11, typeFlags: ring.typeFlags } ];
	assert.deepEqual( itemActivation( 20, inventory, 45, false ), {
		kind: "inventory-move",
		source: 20,
		destination: 12,
		quantity: 0
	} );
	assert.deepEqual( itemActivation( 11, inventory, 45, false ), {
		kind: "inventory-move",
		source: 11,
		destination: 13,
		quantity: 0
	} );
	assert.equal( itemActivation( 11, inventory, undefined, false ), null );
	assert.equal( itemActivation( 20, inventory, 45, true ), null );
	assert.equal( equipmentSocket( 0x2c | (1 << 7) | (2 << 11) ), 2 );
	assert.equal( equipmentSocket( 0x2c | (4 << 7) | (3 << 11) ), null );
	assert.equal( equipmentSocket( 0x6c | (4 << 7) ), 7 );
});

test("bag hotbar resolves current occupancy and never caches a consumed item", () => {
	const binding = { slot: 4, kind: 0x46, payload: 2 },
		state = { inventory: [ { slot: 15, refObjId: 7 } ], inventoryPending: false };
	assert.equal( quickSlotItemSlot( binding ), 15 );
	assert.deepEqual( quickSlotCommand( binding, state ), { kind: "item-use", slot: 15 } );
	assert.equal( quickSlotCommand( binding, { ...state, inventory: [] } ), null );
	assert.equal( quickSlotCommand( binding, { ...state, inventoryPending: true } ), null );
	assert.equal( quickSlotCommand( { slot: 1, kind: 0x4e, payload: 0 }, state ), null );
});
test("native emotes and pet charm use their own commands, independent of mounted attack", () => {
	assert.deepEqual( Array.from( { length: 7 }, ( _, i ) => actionEmote( 4000 + i ) ), [ 0, 6, 1, 5, 2, 3, 4 ] );
	const state = { inventory: [], activeCos: { gid: 72, dead: false }, target: 91 };
	assert.deepEqual( quickSlotCommand( { slot: 3, kind: 0x4a, payload: 0x02000fa1 }, state ), {
		kind: "action-command",
		id: 4001
	} );
	assert.equal( quickSlotCommand( { slot: 3, kind: 0x4a, payload: 5000 }, state ), null );
	assert.deepEqual(
		quickSlotCommand( { slot: 3, kind: 0x4a, payload: 5000 }, {
			...state,
			cosRecords: [ { gid: 73, band: 4, hp: 10, dead: false } ]
		} ),
		{ kind: "action-command", id: 5000 }
	);
	assert.deepEqual( quickSlotCommand( { slot: 3, kind: 0x25, payload: 2 }, state, 72 ), {
		kind: "cos-attack",
		gid: 91
	} );
	assert.equal( quickSlotCommand( { slot: 3, kind: 0x25, payload: 2 }, { ...state, activeCos: undefined } ), null );
	assert.equal( quickSlotCommand( { slot: 3, kind: 0x4a, payload: 9999 }, state ), null );
});
test("item bindings persist slot identity, send failure preserves configuration, actions use native bytes", () => {
	let blocked = false;
	const sent = [],
		g = createGameplay( frame => {
			if ( blocked ) throw Error( "closed" );
			sent.push( frame );
		} );
	g.bootstrap( {
		refItemSnapshot: [ { refObjId: 7, typeFlags: 0x6c } ],
		equipItems: [ { slot: 15, refObjId: 7, body: [ 7, 0, 0, 0, 5, 0 ] } ]
	} );
	g.seed( { gid: 1, regionId: 257, x: 1, y: 0, z: 1, heading: 0 } );
	const binding = { slot: 4, kind: 0x46, payload: 2 };
	g.command( { kind: "quickslot-set", binding }, 0 );
	assert.deepEqual( g.take().quickSlots, [ binding ] );
	blocked = true;
	assert.throws(
		() => g.command( { kind: "quickslot-set", binding: { slot: 4, kind: 0, payload: 0 } }, 0 ),
		/closed/
	);
	assert.deepEqual( g.take().quickSlots, [ binding ] );
	blocked = false;
	g.command( { kind: "action-command", id: 4001 }, 0 );
	assert.equal( sent.at( -1 ).opcode, 0x324b );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 6 ] );
	assert.throws( () => g.command( { kind: "action-command", id: 4999 }, 0 ), /unavailable/ );
	g.dispose();
});
test("native quickslot wire and bootstrap retain all 51 slots and reject invalid references", () => {
	assert.deepEqual( [ ...quickSlotPacket( { slot: 50, kind: 0x49, payload: 0x12345678 } ).payload ], [
		1,
		50,
		0x49,
		0x78,
		0x56,
		0x34,
		0x12
	] );
	const value = { character: { skills: [ 7 ], quickSlots: [ { slot: 50, kind: 0x49, payload: 7 } ] } };
	const state = skillBindings( value );
	value.character.skills[0] = 9;
	assert.deepEqual( state.skills, [ 7 ] );
	assert.throws( () => skillBindings( { character: { skills: [ 7, 7 ] } } ) );
	assert.throws( () => quickSlotPacket( { slot: 51, kind: 0, payload: 0 } ) );
	assert.throws( () => quickSlotPacket( { slot: 0, kind: 0x46, payload: 45 } ) );
});
test("binding configuration writes only after send and cannot grant an unlearned skill", () => {
	const frames = [], g = createGameplay( frame => frames.push( frame ) );
	g.bootstrap( { character: { skills: [ 7 ], quickSlots: [] } } );
	g.seed( { gid: 1, regionId: 257, x: 1, y: 0, z: 1, heading: 0 } );
	assert.throws( () => g.command( { kind: "quickslot-bind", slot: 0, skillId: 9 }, 0 ), /learned/ );
	assert.equal( frames.length, 0 );
	g.command( { kind: "quickslot-bind", slot: 50, skillId: 7 }, 0 );
	assert.equal( frames[0].opcode, 0x7541 );
	assert.deepEqual( g.take().quickSlots, [ { slot: 50, kind: 0x49, payload: 7 } ] );
	g.command( { kind: "quickslot-bind", slot: 50, skillId: 0 }, 0 );
	assert.deepEqual( g.take().quickSlots, [] );
	g.resetWorld();
	assert.deepEqual( g.take().skills, [ 7 ] );
	g.reset();
	assert.deepEqual( g.take().skills, [] );
	g.dispose();
	const failed = createGameplay( () => {
		throw Error( "closed" );
	} );
	failed.bootstrap( { character: { skills: [ 7 ] } } );
	failed.seed( { gid: 1, regionId: 257, x: 1, y: 0, z: 1, heading: 0 } );
	assert.throws( () => failed.command( { kind: "quickslot-bind", slot: 0, skillId: 7 }, 0 ), /closed/ );
	assert.deepEqual( failed.take().quickSlots, [] );
	failed.dispose();
});

const { quickSlotDrag, quickSlotDrop, extendedSlot } = await load( "foundation/gameplay/quickslots.ts" );
test("native quickslot drag swaps occupied slots and clears the moved source across both bars", () => {
	const a = { slot: 1, kind: 0x49, payload: 7 },
		b = { slot: 41, kind: 0x4a, payload: 1000 },
		state = { skills: [ 7 ], inventory: [], quickSlots: [ a, b ] };
	assert.deepEqual( quickSlotDrop( "hotbar:1", 41, state ), [ { ...b, slot: 1 }, { ...a, slot: 41 } ] );
	assert.deepEqual( quickSlotDrop( "hotbar:41", 50, state ), [ { slot: 41, kind: 0, payload: 0 }, {
		...b,
		slot: 50
	} ] );
	assert.deepEqual( quickSlotDrop( "hotbar:1", 1, state ), [] );
	assert.deepEqual( quickSlotDrop( "skill:7", 41, state ), [ { ...a, slot: 41 } ] );
	assert.deepEqual( state.quickSlots, [ a, b ] );
});
const { masteryTrainingReason, masteryCosts } = await load( "foundation/gameplay/skill-catalog.ts" );
const { createSkillCooldowns, skillCooldown } = await load( "foundation/gameplay/skill-cooldowns.ts" );
const { quickslotCooldownQuads } = await load( "foundation/ui/quickslot-cooldown.ts" );
test("both bars bind live references across all 51 slots without moving or granting content", () => {
	const state = {
		skills: [ 7 ],
		inventory: [ { slot: 13, refObjId: 8 } ],
		quickSlots: [ { slot: 1, kind: 0x49, payload: 7 } ]
	};
	assert.deepEqual( Array.from( { length: 10 }, ( _, i ) => extendedSlot( i ) ), [
		41,
		42,
		43,
		44,
		45,
		46,
		47,
		48,
		49,
		50
	] );
	for ( let slot = 0; slot < 51; slot++ ) {
		assert.deepEqual( quickSlotDrag( "skill:7", slot, state ), { slot, kind: 0x49, payload: 7 } );
		assert.deepEqual( quickSlotDrag( "hotbar:1", slot, state ), { slot, kind: 0x49, payload: 7 } );
		assert.deepEqual( quickSlotDrag( "slot:13", slot, state ), { slot, kind: 0x46, payload: 0 } );
		assert.deepEqual( quickSlotDrag( "action:2", slot, state ), { slot, kind: 0x25, payload: 2 } );
	}
	assert.equal( quickSlotDrag( "skill:8", 50, state ), null );
	assert.equal( quickSlotDrag( "slot:14", 50, state ), null );
	assert.deepEqual( state.skills, [ 7 ] );
	assert.equal( state.inventory[0].slot, 13 );
});
test("mastery UI prices current level and preserves the free first train at zero SP", () => {
	const costs = masteryCosts( {
			"1": { masteryTrainSpCost: 1 },
			"4": { masteryTrainSpCost: 2 },
			"5": { masteryTrainSpCost: 3 }
		} ),
		p = { level: 10, skillPoints: 0, masteries: [ { id: 257, level: 0 } ] };
	assert.equal( masteryTrainingReason( 257, p, {} ), null );
	assert.equal(
		masteryTrainingReason( 257, { ...p, masteries: [ { id: 257, level: 4 } ], skillPoints: 2 }, costs ),
		null
	);
	assert.match(
		masteryTrainingReason( 257, { ...p, masteries: [ { id: 257, level: 4 } ], skillPoints: 1 }, costs ),
		/Insufficient/
	);
	assert.match( masteryTrainingReason( 257, { ...p, level: 0 }, costs ), /level/ );
	assert.match( masteryTrainingReason( 257, { ...p, masteries: [ { id: 257, level: 1 } ] }, {} ), /unavailable/ );
});
test("accepted skill cooldowns share the native group, expire independently and draw retail atlas frames", () => {
	const owner = createSkillCooldowns();
	owner.accepted( { id: 7, cooldownGroup: 2, cooldownMs: 2000 }, 1000, 1000 );
	assert.deepEqual( skillCooldown( owner.state(), 9, 2, 2000 ), { remainingMs: 1000, fraction: .5 } );
	assert.equal( skillCooldown( owner.state(), 9, 0, 2000 ), null );
	assert.equal( skillCooldown( owner.state(), 7, 2, 3000 ), null );
	const r = [ 10, 20, 32, 32 ],
		clip = [ 0, 0, 100, 100 ],
		q = quickslotCooldownQuads( owner.state(), 9, 2, 2000, r, clip );
	assert.match( q[0].texture, /skill_delay/ );
	assert.deepEqual( q[0].uv, [ 7 / 16, 7 / 16, 1 / 16, 1 / 16 ] );
	assert.match( q[1].texture, /cool_time_1/ );
	assert.deepEqual( q[1].rect, [ 22, 30, 8, 12 ] );
	assert.match( quickslotCooldownQuads( owner.state(), 7, 2, 3000, r, clip )[0].texture, /skill_charge/ );
	owner.step( 3500 );
	assert.deepEqual( owner.state(), [] );
	owner.clear();
});
