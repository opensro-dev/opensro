/*
===========================================================================

withdrawal.test.mjs - native restoration bytes, bindings and dialog lifecycle

Load the shipped modules directly. Opening a dialog must not consume an
item, and a changed inventory must invalidate an earlier confirmation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const {
	withdrawalRequest,
	withdrawalSkillBindings,
	isRestorationPotion
} = await import( "../../src/engine/foundation/gameplay/withdrawal.ts" );
const { createWithdrawalDialog } = await import( "../../src/engine/runtime/ui/hud/withdrawal.ts" );
const { createTraining } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/training/training.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
skill

One root per rank with unequal prices catches last-rank multiplication.
================
*/
function skill( id, rank, price ) {
	return {
		id,
		group: 100,
		level: rank,
		name: `Skill ${rank}`,
		spCost: price,
		trainable: true,
		targetRequired: false,
		cooldownMs: 0,
		masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
		prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
	};
}

/*
================
game

A complete publication avoids depending on renderer or network fixtures.
================
*/
function game() {
	return {
		revision: 1,
		localGid: 1,
		pose: null,
		authoritativePose: null,
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 0,
		targetPending: 0,
		inventoryPending: false,
		vitals: [],
		casts: [],
		error: null,
		skills: [ 12 ],
		skillCatalog: [ skill( 10, 1, 2 ), skill( 11, 2, 7 ), skill( 12, 3, 13 ) ],
		quickSlots: [ { slot: 0, kind: 0x49, payload: 12 }, { slot: 41, kind: 0x49, payload: 12 } ],
		progression: { masteries: [ { id: 257, level: 1 } ] },
		inventory: [ {
			slot: 13,
			refObjId: 3828,
			name: "Skill Restoration Potion",
			typeFlags: 0x6ec,
			quantity: 1,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: []
		} ]
	};
}

test("restoration requests preserve native identities and absolute target rank", () => {
	assert.deepEqual( withdrawalRequest( { kind: "skill-withdraw", potion: 3828, id: 12, rank: 2 } ), {
		opcode: 0x74d6,
		payload: Uint8Array.from( [ 0xf4, 0x0e, 0, 0, 12, 0, 0, 0, 2 ] )
	} );
	assert.equal( withdrawalRequest( { kind: "mastery-withdraw", potion: 3828, id: 257, rank: 0 } ).opcode, 0x7606 );
	assert.throws( () => withdrawalRequest( { kind: "skill-withdraw", potion: 0, id: 12, rank: 0 } ) );
	assert.throws( () => withdrawalRequest( { kind: "skill-withdraw", potion: 3828, id: 12, rank: -1 } ) );
});

test("restoration receipts repair every hotbar and reject upward ranks", () => {
	const state = game();
	const lower = withdrawalSkillBindings( state, state.skillCatalog, 11 );
	assert.deepEqual( lower.skills, [ 11 ] );
	assert.deepEqual( lower.quickSlots.map( slot => slot.payload ), [ 11, 11 ] );
	assert.throws( () => withdrawalSkillBindings( lower, state.skillCatalog, 12 ) );
	const removed = withdrawalSkillBindings( lower, state.skillCatalog, 11 );
	assert.deepEqual( removed.skills, [] );
	assert.ok( removed.quickSlots.every( slot => slot.kind === 0 && slot.payload === 0 ) );
	assert.deepEqual( state.skills, [ 12 ] );
});

test("dialog quotes current inventory without spending on open or cancel", () => {
	const state = game(), dialog = createWithdrawalDialog();
	assert.ok( isRestorationPotion( state.inventory[0] ) );
	dialog.open( 3828 );
	dialog.select( "skill-withdraw:12" );
	assert.equal( dialog.read( state, {} ).command, null );
	dialog.adjust( 1 );
	const quote = dialog.read( state, {} );
	assert.equal( quote.choice?.refund, 13 );
	assert.deepEqual( quote.command, { kind: "skill-withdraw", potion: 3828, id: 12, rank: 2 } );
	assert.equal( dialog.read( { ...state, inventory: [] }, {} ).command, null );
	assert.equal( dialog.read( { ...state, trainingPending: true }, {} ).command, null );
	assert.equal( state.inventory[0].quantity, 1 );
	dialog.close();
	assert.equal( dialog.active(), false );
});

test("rank spinner sums unequal costs and clamps to current potion quantity", () => {
	const state = game(), dialog = createWithdrawalDialog();
	state.inventory[0].quantity = 3;
	dialog.open( 3828 );
	dialog.select( "skill-withdraw:12" );
	assert.equal( dialog.read( state, {} ).amount, 0 );
	dialog.adjust( 2 );
	assert.equal( dialog.read( state, {} ).choice?.refund, 20 );
	assert.equal( dialog.read( state, {} ).command?.rank, 1 );
	dialog.adjust( 100 );
	assert.equal( dialog.read( state, {} ).choice?.refund, 22 );
	assert.equal( dialog.read( state, {} ).command?.rank, 0 );
	state.inventory[0].quantity = 1;
	assert.equal( dialog.read( state, {} ).amount, 1 );
	assert.equal( dialog.read( state, {} ).choice?.refund, 13 );
	dialog.adjust( -1 );
	assert.equal( dialog.read( state, {} ).command, null );
});

test("withdrawal respects every learned dependency slot when choosing a target rank", () => {
	const state = game(), dialog = createWithdrawalDialog();
	const dependent = { ...skill( 20, 1, 1 ), group: 200 };
	dependent.prerequisites[2] = { ID: 100, Level: 2 };
	state.skills.push( dependent.id );
	state.skillCatalog.push( dependent );
	state.inventory[0].quantity = 3;
	dialog.open( 3828 );
	dialog.select( "skill-withdraw:12" );
	dialog.adjust( 3 );
	assert.equal( dialog.read( state, {} ).maximum, 1 );
	assert.equal( dialog.read( state, {} ).command?.rank, 2 );
	dialog.select( "mastery-withdraw:257" );
	dialog.adjust( 1 );
	assert.equal( dialog.read( state, {} ).maximum, 0 );
	assert.equal( dialog.read( state, {} ).command, null );
});

test("restoration uses the common uncertain-receipt gate", () => {
	const sent = [], owner = createTraining( frame => sent.push( frame ) );
	const request = withdrawalRequest( { kind: "skill-withdraw", potion: 3828, id: 12, rank: 2 } );
	owner.request( request, 12, 0 );
	owner.step( 10001 );
	assert.throws( () => owner.request( request, 12, 10002 ), /pending/ );
	owner.receipt( 0xb606 );
	assert.equal( owner.state().trainingPending, true );
	owner.receipt( 0xb4d6 );
	assert.equal( owner.state().trainingPending, false );
	assert.equal( sent.length, 1 );
});

test("gameplay commits a downgrade once and ignores a duplicate ambiguous receipt", () => {
	const state = game(), sent = [], owner = createGameplay( frame => sent.push( frame ) );
	owner.bootstrap( {
		character: { skills: state.skills, quickSlots: state.quickSlots },
		refSkillSnapshot: state.skillCatalog.map( row => ({
			id: row.id,
			group: row.group,
			level: row.level,
			token: false,
			status: false,
			effectRider: false,
			ui: row
		}) )
	} );
	owner.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		name: "Tester",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	} );
	owner.command( { kind: "skill-withdraw", potion: 3828, id: 12, rank: 2 }, 0, undefined );
	assert.deepEqual( owner.take()?.skills, [ 12 ] );
	const receipt = { opcode: 0xb4d6, payload: Uint8Array.from( [ 1, 11, 0, 0, 0 ] ) };
	owner.receive( receipt, 1 );
	assert.deepEqual( owner.take()?.skills, [ 11 ] );
	const saves = sent.filter( frame => frame.opcode === 0x7541 ).length;
	assert.equal( saves, 2 );
	owner.receive( receipt, 2 );
	assert.equal( sent.filter( frame => frame.opcode === 0x7541 ).length, saves );
	owner.command( { kind: "skill-withdraw", potion: 3828, id: 11, rank: 0 }, 3, undefined );
	owner.receive( receipt, 4 );
	assert.deepEqual( owner.take()?.skills, [] );
	owner.dispose();
});
