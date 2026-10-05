/*
===========================================================================

cos-container.test.mjs - tests for gameplay.ts, cos-transfer.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const entity = { gid: 7, refObjId: 102, kind: "cos", ownerGid: 1 };

test("COS ground requests and receipts keep the player bag isolated", () => {
	const { owner } = fixture();
	const drop = owner.command( { kind: "cos-drop", gid: 7, slot: 0 }, 0, entity );
	assert.deepEqual( [ ...drop.payload ], [ 0x12, 7, 0, 0, 0, 0 ] );
	owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...drop.payload ] ) }, 1 );
	let s = owner.take();
	assert.equal( s.cosRecords[0].inventory.length, 1 );
	assert.deepEqual( s.inventory, [] );
	const pickup = owner.command( { kind: "cos-pickup", gid: 7, target: 123 }, 2, entity );
	assert.deepEqual( [ ...pickup.payload ], [ 0x11, 7, 0, 0, 0, 123, 0, 0, 0 ] );
	const reply = { opcode: 0xb06d, payload: Uint8Array.from( [ 1, 0x11, ...u32( 7 ), 0, ...u32( 8 ), 30, 0 ] ) };
	owner.receive( reply, 3 );
	s = owner.take();
	assert.equal( s.cosRecords[0].inventory.length, 2 );
	assert.equal( s.inventoryPending, false );
	assert.deepEqual( s.inventory, [] );
	assert.throws( () => owner.receive( reply, 4 ) );
	owner.command( { kind: "cos-pickup", gid: 7, target: 124 }, 5, entity );
	owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, 0x11, ...u32( 7 ), 254, ...u32( 50 ) ] ) }, 6 );
	assert.equal( owner.take().cosRecords[0].inventory.length, 2 );
});
function fixture() {
	const sent = [], owner = createGameplay( f => sent.push( f ) );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 102, tidWord: 0x21c6 } ],
		refItemSnapshot: [ { refObjId: 8, typeFlags: 0x86c, nativeFields: { maxStack: 50 } } ]
	} );
	owner.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	owner.receive( {
		opcode: 0x3158,
		payload: Uint8Array.from( [
			...u32( 7 ),
			...u32( 102 ),
			...u32( 100 ),
			...u32( 50 ),
			...u32( 0x47 ),
			0,
			0,
			4,
			2,
			0,
			...u32( 8 ),
			30,
			0,
			1,
			...u32( 8 ),
			40,
			0,
			...u32( 0 ),
			14
		] )
	}, 0 );
	const initial = owner.take();
	return { owner, sent, initial };
}
function move( owner, source, destination, quantity ) {
	const f = owner.command( { kind: "cos-inventory-move", gid: 7, source, destination, quantity }, 0, entity );
	owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...f.payload ] ) }, 1 );
	return f;
}
test("COS container uses native capped merges, splits and full-destination count swaps", () => {
	const { owner } = fixture();
	const f = move( owner, 0, 1, 1 );
	assert.deepEqual( [ ...f.payload ], [ 16, 7, 0, 0, 0, 0, 1, 1, 0 ] );
	assert.deepEqual( owner.take().cosRecords[0].inventory.map( x => [ x.slot, x.quantity ] ), [ [ 0, 20 ], [
		1,
		50
	] ] );
	move( owner, 0, 1, 1 );
	assert.deepEqual( owner.take().cosRecords[0].inventory.map( x => [ x.slot, x.quantity ] ), [ [ 0, 50 ], [
		1,
		20
	] ] );
	move( owner, 0, 2, 5 );
	assert.deepEqual( owner.take().cosRecords[0].inventory.map( x => [ x.slot, x.quantity ] ), [ [ 0, 45 ], [ 1, 20 ], [
		2,
		5
	] ] );
});
test("COS operation shares inventory latch and cannot accept stale or mismatched receipts", () => {
	const { owner, sent } = fixture();
	assert.throws( () =>
		owner.command( { kind: "cos-inventory-move", gid: 7, source: 0, destination: 4, quantity: 5 }, 0, entity )
	);
	assert.throws( () =>
		owner.command( { kind: "cos-inventory-move", gid: 7, source: 0, destination: 2, quantity: 5 }, 0, {
			...entity,
			ownerGid: 2
		} )
	);
	assert.equal( sent.length, 0 );
	const f = owner.command(
		{ kind: "cos-inventory-move", gid: 7, source: 0, destination: 2, quantity: 5 },
		0,
		entity
	);
	assert.equal( owner.take().inventoryPending, true );
	assert.throws( () =>
		owner.command( { kind: "cos-inventory-move", gid: 7, source: 0, destination: 3, quantity: 1 }, 0, entity )
	);
	const wrong = Uint8Array.from( [ 1, ...f.payload ] );
	wrong[7] = 3;
	assert.throws( () => owner.receive( { opcode: 0xb06d, payload: wrong }, 1 ) );
	assert.equal( owner.take().cosRecords[0].inventory.length, 2 );
	const reply = { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...f.payload ] ) };
	owner.receive( reply, 1 );
	assert.equal( owner.take().inventoryPending, false );
	assert.throws( () => owner.receive( reply, 2 ) );
});
test("behavior commands preserve server-owned state until native acknowledgement", () => {
	const { owner, sent } = fixture();
	const f = owner.command( { kind: "cos-behavior", gid: 7, mode: 0xc7 }, 0, entity );
	assert.equal( f.opcode, 0x705b );
	assert.equal( sent.length, 1 );
	assert.equal( owner.take().cosRecords[0].commandMode, 0x47 );
	owner.receive( { opcode: 0xb05b, payload: Uint8Array.from( [ 1, ...f.payload ] ) }, 1 );
	assert.equal( owner.take().cosRecords[0].commandMode, 0xc7 );
	assert.throws( () => owner.command( { kind: "cos-behavior", gid: 7, mode: 0x100 }, 0, entity ) );
});

test("player/COS whole transfers await matching authority and preserve both containers", () => {
	const { owner, initial } = fixture(), before = initial.cosRecords[0].inventory;
	const f = owner.command( { kind: "cos-transfer", gid: 7, toCos: false, source: 0, destination: 13 }, 0, entity );
	assert.deepEqual( [ ...f.payload ], [ 0x1a, 7, 0, 0, 0, 0, 13 ] );
	assert.deepEqual( owner.take().inventory, [] );
	const bad = Uint8Array.from( [ 1, ...f.payload ] );
	bad[7] = 14;
	assert.throws( () => owner.receive( { opcode: 0xb06d, payload: bad }, 1 ) );
	assert.equal( owner.take(), null );
	owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...f.payload ] ) }, 1 );
	let state = owner.take();
	assert.equal( state.inventory[0].quantity, 30 );
	assert.equal( state.cosRecords[0].inventory.length, 1 );
	assert.equal( state.inventoryPending, false );
	// Occupied destination transfers are exercised separately below.
	const back = owner.command( { kind: "cos-transfer", gid: 7, toCos: true, source: 13, destination: 0 }, 2, entity );
	assert.deepEqual( [ ...back.payload ], [ 0x1b, 7, 0, 0, 0, 13, 0 ] );
	assert.equal( owner.take().inventory.length, 1 );
	const reply = { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...back.payload ] ) };
	owner.receive( reply, 3 );
	state = owner.take();
	assert.deepEqual( state.inventory, [] );
	assert.deepEqual( state.cosRecords[0].inventory, before );
	assert.throws( () => owner.receive( reply, 4 ) );
});

test("failed or timed out transfers cannot consume items or cross the inventory barrier", () => {
	const { owner } = fixture();
	owner.command( { kind: "cos-transfer", gid: 7, toCos: false, source: 0, destination: 13 }, 0, entity );
	owner.receive( { opcode: 0xb06d, payload: Uint8Array.of( 2, 1 ) }, 1 );
	let state = owner.take();
	assert.deepEqual( state.inventory, [] );
	assert.equal( state.cosRecords[0].inventory[0].quantity, 30 );
	assert.equal( state.inventoryPending, false );
	const f = owner.command( { kind: "cos-transfer", gid: 7, toCos: false, source: 0, destination: 13 }, 2, entity );
	assert.throws( () => owner.step( 10002 ), /reconnect/ );
	assert.throws(
		() => owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...f.payload ] ) }, 10003 ),
		/reconnect/
	);
});

for (
	const [label, destCount, wantSource, wantDest] of [ [ "merge", 10, 0, 40 ], [ "overflow", 40, 20, 50 ], [
		"full destination",
		50,
		50,
		30
	] ]
) {
	test( "COS cross-container " + label, () => {
		const { owner } = fixture();
		const out = owner.command(
			{ kind: "cos-transfer", gid: 7, toCos: false, source: 0, destination: 13 },
			0,
			entity
		);
		owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...out.payload ] ) }, 1 );
		owner.take();
		// Publish the destination through its existing ground-receipt owner.
		owner.command( { kind: "cos-pickup", gid: 7, target: 99 }, 2, entity );
		owner.receive( {
			opcode: 0xb06d,
			payload: Uint8Array.from( [ 1, 0x11, ...u32( 7 ), 1, ...u32( 8 ), destCount, 0 ] )
		}, 3 );
		owner.take();
		const back = owner.command(
			{ kind: "cos-transfer", gid: 7, toCos: true, source: 13, destination: 1 },
			4,
			entity
		);
		assert.equal( owner.take().inventory[0].quantity, 30 );
		owner.receive( { opcode: 0xb06d, payload: Uint8Array.from( [ 1, ...back.payload ] ) }, 5 );
		const state = owner.take();
		assert.equal( state.cosRecords[0].inventory.find( r => r.slot === 1 ).quantity, wantDest );
		assert.equal( state.inventory.find( r => r.slot === 13 )?.quantity ?? 0, wantSource );
	} );
}

test("cross-container swaps preserve both bodies and missing caps cannot mutate input", async () => {
	const { planCosTransfer } = await import( sourceFileUrl( "src/engine/foundation/gameplay/cos-transfer.ts" ).href );
	const { initial } = fixture(), record = initial.cosRecords[0];
	const player = [ { ...record.inventory[0], slot: 13, refObjId: 99, plus: 7, magic: [ "123" ] } ],
		before = structuredClone( { record, player } );
	const next = planCosTransfer( record, player, true, 13, 1, 45, 13, new Map( [ [ 8, 50 ] ] ) );
	assert.equal( next.player[0].refObjId, 8 );
	assert.equal( next.player[0].quantity, 40 );
	assert.equal( next.cos.inventory.find( r => r.slot === 1 ).refObjId, 99 );
	assert.deepEqual( next.cos.inventory.find( r => r.slot === 1 ).magic, [ "123" ] );
	assert.deepEqual( { record, player }, before );
	assert.throws(
		() => planCosTransfer( record, [ { ...record.inventory[0], slot: 13 } ], true, 13, 1, 45, 13, new Map() ),
		/stack limit/
	);
	assert.deepEqual( { record, player }, before );
});

/*
================
tradeOwnerContainerTransfers
================
*/
test("trade cargo merges only stacks carrying the same original owner", async () => {
	const { planContainerMove, planWholeTransfer } = await import(
		"../../src/engine/foundation/gameplay/container-transfer.ts"
	);
	const a = {
		slot: 0,
		refObjId: 2151,
		typeFlags: 0xc6c,
		quantity: 3,
		label: "A",
		plus: 0,
		durability: 0,
		variance: "0",
		magic: []
	};
	const b = { ...a, slot: 1, quantity: 4, label: "B" };
	const caps = new Map( [ [ 2151, 40 ] ] );
	const moved = planContainerMove( [ a, b ], { source: 0, destination: 1, quantity: 3 }, caps, "cargo" );
	assert.deepEqual( moved.map( i => [ i.slot, i.quantity, i.label ] ), [ [ 0, 4, "B" ], [ 1, 3, "A" ] ] );
	const merged = planContainerMove(
		[ a, { ...b, label: "A" } ],
		{ source: 0, destination: 1, quantity: 3 },
		caps,
		"cargo"
	);
	assert.deepEqual( merged.map( i => [ i.quantity, i.label ] ), [ [ 7, "A" ] ] );
	const transferred = planWholeTransfer( [ a ], [ b ], 0, 1, caps );
	assert.equal( transferred.to[0].label, "A" );
	assert.equal( transferred.from[0].label, "B" );
});
