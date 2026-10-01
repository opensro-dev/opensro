/*
===========================================================================

inventory-retail.test.mjs - tests for inventory.ts, inventory-layout.ts,
portrait.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { inventorySlots } = await import( "../../src/engine/foundation/ui/inventory-layout.ts" );
const { createPortrait } = await import( "../../src/engine/runtime/renderer/characters/portrait.ts" );
test("native bag control origin is x18/y13, independent of window position and page", () => {
	const a = inventorySlots( 100, 200, 77, 13, 1 );
	assert.deepEqual( a.slots[0].rect, [ 118, 213, 32, 32 ] );
	assert.equal( a.slots[0].slot, 45 );
	assert.deepEqual( a.slots[31].rect, [ 226, 465, 32, 32 ] );
});
test("avatar transfer uses separate storage, native three-byte request, and preserves item body", () => {
	const sent = [],
		owner = createInventory( f => sent.push( f ) ),
		body = [ 1, 0, 0, 0, 2, 123, 0, 0, 0, 0, 0, 0, 0, 30, 0, 0, 0, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0xeac, name: "Hat" } ],
		equipItems: [ { slot: 20, refObjId: 1, body } ],
		avatarItems: []
	} );
	const before = owner.state().inventory[0];
	owner.avatarMove( true, 20, 0, 0 );
	assert.deepEqual( [ ...sent[0].payload ], [ 0x24, 20, 0 ] );
	assert.equal( owner.state().inventory.length, 1 );
	assert.throws( () => owner.receive( 0xb06d, Uint8Array.of( 1, 0x24, 20, 1, 1, 0, 0 ) ), /Unmatched/ );
	assert.equal( owner.state().inventory.length, 1 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0x24, 20, 0, 1, 0, 0 ) );
	assert.equal( owner.state().inventory.length, 0 );
	assert.equal( owner.state().avatarInventory[0].variance, "123" );
	owner.avatarMove( false, 0, 20, 1 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0x23, 0, 20, 1, 0, 0 ) );
	assert.deepEqual( owner.state().inventory[0], before );
	assert.deepEqual( owner.state().avatarInventory, [] );
	owner.clear();
	assert.deepEqual( owner.state().avatarInventory, [] );
});
test("inventory doll advances its own idle and resets on replacement without inheriting combat or opacity", () => {
	let actors = [];
	const model = { clips: [ { name: "stand" } ] },
		source = {
			actor: {
				gid: 1,
				model: "a",
				clip: "attack",
				previewClip: "preview-state0-sword",
				time: 8,
				loop: false,
				opacity: 0,
				layers: [ { clip: "attack" } ],
				pose: {},
				scale: 1
			},
			model,
			images: []
		};
	const p = createPortrait( {
		retain() {},
		actors( a ) {
			actors = a;
		},
		prepare() {
			return [];
		},
		borrowModel() {},
		socket() {
			return { x: 0, y: 10, z: 0 };
		},
		invalidate() {},
		dispose() {}
	} );
	p.prepare( source, {}, {}, { yaw: .1, seconds: 10 } );
	p.prepare( source, {}, {}, { yaw: .2, seconds: 11 } );
	assert.equal( actors[0].time, 1 );
	assert.equal( actors[0].clip, "preview-state0-sword" );
	assert.equal( actors[0].loop, true );
	assert.equal( actors[0].opacity, 1 );
	assert.equal( actors[0].layers, undefined );
	p.prepare( { ...source, model: { clips: [ { name: "stand" } ] } }, {}, {}, { yaw: .2, seconds: 12 } );
	assert.equal( actors[0].time, 0 );
	p.prepare( null, {}, {}, { yaw: undefined, seconds: 20 } );
	p.prepare( source, {}, {}, { yaw: .1, seconds: 30 } );
	assert.equal( actors[0].time, 0 );
	p.prepare( source, {}, {}, { yaw: undefined, seconds: 31 } );
	assert.equal( actors[0].time, 0 );
	assert.equal( actors[0].clip, "stand" );
});

test("avatar attachment submoves commit together, accept server-selected bag slots and fail atomically", () => {
	const owner = createInventory( () => {} ),
		body = id => [ id, 0, 0, 0, 2, 123, 0, 0, 0, 0, 0, 0, 0, 30, 0, 0, 0, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x16ac, name: "Dress" }, {
			refObjId: 2,
			typeFlags: 0x1eac,
			name: "Attachment"
		} ],
		equipItems: [],
		avatarItems: [ { slot: 3, refObjId: 1, body: body( 1 ) }, { slot: 0, refObjId: 2, body: body( 2 ) } ]
	} );
	owner.avatarMove( false, 3, 20, 0 );
	const before = owner.state();
	const packet = Uint8Array.of( 1, 0x23, 3, 13, 1, 0, 1, 0x23, 0, 14, 1, 0 );
	for (
		const bad of [
			packet.slice( 0, -1 ),
			Uint8Array.of( 1, 0x23, 3, 13, 1, 0, 1, 0x23, 0, 13, 1, 0 ),
			Uint8Array.of( 1, 0x23, 3, 13, 1, 0, 1, 0x23, 0, 6, 1, 0 )
		]
	) {
		assert.throws( () => owner.receive( 0xb06d, bad ) );
		assert.deepEqual( owner.state(), before );
	}
	owner.receive( 0xb06d, packet );
	assert.equal( owner.state().inventoryPending, false );
	assert.deepEqual( owner.state().avatarInventory, [] );
	assert.deepEqual( owner.state().inventory.map( i => [ i.slot, i.refObjId, i.variance ] ), [ [ 13, 1, "123" ], [
		14,
		2,
		"123"
	] ] );
	assert.throws( () => owner.receive( 0xb06d, packet ), /Unmatched/ );
});

test("quest grants use absolute stack bodies and do not acknowledge a pending bag move", () => {
	const sent = [], owner = createInventory( f => sent.push( f ) );
	const stack = n => [ 1, 0, 0, 0, n, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x8ec, name: "Herb" } ],
		equipItems: [ { slot: 20, refObjId: 1, body: stack( 10 ) } ]
	} );
	owner.move( 20, 21, 1, 0 );
	owner.receive( 0xb06d, Uint8Array.from( [ 1, 14, 20, 0, ...stack( 38 ) ] ) );
	assert.equal( owner.state().inventory[0].quantity, 38 );
	assert.throws( () => owner.move( 20, 22, 1, 1 ), /pending|unavailable|busy/i );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0, 20, 21, 1, 0, 0 ) );
	assert.equal( defined( owner.state().inventory.find( x => x.slot === 20 ) ).quantity, 37 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 15, 21, 0 ) );
	assert.equal( owner.state().inventory.some( x => x.slot === 21 ), false );
});

test("malformed quest inventory packets preserve the last valid inventory", () => {
	const owner = createInventory( () => {} ), stack = [ 1, 0, 0, 0, 10, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x8ec, name: "Herb" } ],
		equipItems: [ { slot: 20, refObjId: 1, body: stack } ]
	} );
	const before = owner.state().inventory;
	for (
		const packet of [
			[ 1, 14, 20 ],
			[ 1, 14, 12, 0, ...stack ],
			[ 1, 14, 45, 0, ...stack ],
			[ 1, 14, 20, 0, ...stack, 0 ],
			[ 1, 15, 20, 0, 1 ],
			[ 1, 15, 21, 0 ]
		]
	) {
		assert.throws( () => owner.receive( 0xb06d, Uint8Array.from( packet ) ) );
		assert.deepEqual( owner.state().inventory, before );
	}
});

test("weapon swap and companion ammo move commit as one inventory result", () => {
	const owner = createInventory( () => {} ), body = id => [ id, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 30, 0, 0, 0, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x132c, name: "Sword" }, {
			refObjId: 2,
			typeFlags: 0x332c,
			name: "Bow"
		}, { refObjId: 3, typeFlags: 0xa6c, name: "Arrow" } ],
		equipItems: [ { slot: 6, refObjId: 2, body: body( 2 ) }, { slot: 20, refObjId: 1, body: body( 1 ) }, {
			slot: 7,
			refObjId: 3,
			body: [ 3, 0, 0, 0, 50, 0 ]
		} ]
	} );
	owner.move( 20, 6, 1, 0 );
	const before = owner.state();
	assert.throws( () => owner.receive( 0xb06d, Uint8Array.of( 1, 0, 20, 6, 1, 0, 1, 0, 8, 13, 0, 0 ) ) );
	assert.deepEqual( owner.state(), before );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0, 20, 6, 1, 0, 1, 0, 7, 13, 0, 0 ) );
	assert.deepEqual( owner.state().inventory.map( i => [ i.slot, i.refObjId ] ).sort( ( a, b ) => a[0] - b[0] ), [
		[ 6, 1 ],
		[ 13, 3 ],
		[ 20, 2 ]
	] );
	assert.equal( defined( owner.state().inventory.find( i => i.slot === 13 ) ).quantity, 50 );
});
