/*
===========================================================================

cos-pickup.test.mjs - native pet pickup filters, wire and retry lifecycle

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCosPickup, cosPickupRequest } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/cos-pickup.ts"
);
const {
	PICKUP_KIND_ALL,
	PICKUP_KIND_ALCHEMY,
	PICKUP_KIND_EQUIPMENT,
	PICKUP_KIND_GOLD,
	PICKUP_KIND_OTHER,
	PICKUP_KIND_POTIONS
} = await import( "../../src/engine/foundation/gameplay/ground-item.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

test("gameplay frame loop sends automatic pickup and consumes native completion", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ), undefined, undefined, f.frame.read );
	gameplay.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x21c6 } ]
	} );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 29 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	view.setUint32( 16, 0xc7, true );
	record[22] = 28;
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 10, 20 ) } );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 11, 30 ) } );
	gameplay.step( 0, f.frame.local );
	assert.deepEqual( sent, [ cosPickupRequest( 2, 10 ) ] );
	gameplay.step( 1, f.frame.local );
	assert.equal( sent.length, 1 );
	gameplay.command( { kind: "cos-pickup", gid: 2, target: 12 }, 1, f.pet, f.frame.local );
	const receipt = { opcode: 0xb06d, payload: Uint8Array.of( 1, 0x11, 2, 0, 0, 0, 254, 10, 0, 0, 0 ) };
	gameplay.receive( receipt, 1 );
	assert.equal( gameplay.take()?.inventoryPending, true );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 1, 8, 2, 0, 0, 0, 10, 0, 0, 0 ) }, 2 );
	gameplay.receive( { opcode: 0xb06d, payload: Uint8Array.of( 2, 1 ) }, 2 );
	assert.equal( gameplay.take()?.inventoryPending, false );
	// A pet pickup holds no lock: a stray receipt applies and arms nothing.
	gameplay.receive( receipt, 2 );
	assert.equal( gameplay.take()?.inventoryPending ?? false, false );
	gameplay.experimental( {
		predictedRunUp: false,
		webTransport: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true,
		homingProjectiles: false,
		smartPetPickup: false
	} );
	gameplay.step( 2, f.frame.local );
	assert.deepEqual( sent[2], cosPickupRequest( 2, 11 ) );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 2, 8, 0xb4, 2, 0, 0, 0, 11, 0, 0, 0 ) }, 3 );
	assert.deepEqual( sent[3], { opcode: 0x705b, payload: Uint8Array.of( 2, 0, 0, 0, 2, 0x47, 0, 0, 0 ) } );
	const fullBag = gameplay.take();
	assert.equal( fullBag?.cosRecords?.[0]?.commandMode, 0xc7 );
	assert.equal( fullBag?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_CANT_PICKITEM1" );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 2, 8, 0xb5, 2, 0, 0, 0, 11, 0, 0, 0 ) }, 3 );
	assert.equal( gameplay.take()?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_CANT_PICKITEM2" );
	gameplay.receive( { opcode: 0xb05b, payload: Uint8Array.of( 1, 2, 0, 0, 0, 2, 0x47, 0, 0, 0 ) }, 3 );
	assert.equal( gameplay.take()?.cosRecords?.[0]?.commandMode, 0x47 );
	const follow = gameplay.command( { kind: "cos-follow", gid: 2 }, 4, f.pet, f.frame.local );
	assert.deepEqual( follow, { opcode: 0x769e, payload: Uint8Array.of( 2, 0, 0, 0, 9 ) } );
	assert.throws( () =>
		gameplay.command( { kind: "cos-follow", gid: 2 }, 5, { ...f.pet, ownerGid: 99 }, f.frame.local )
	);
	assert.throws( () =>
		gameplay.command( { kind: "cos-follow", gid: 2 }, 5, f.pet, { ...f.frame.local, mountedOn: 2 } )
	);
	gameplay.resetWorld();
	gameplay.step( 6, f.frame.local );
	assert.equal( sent.length, 6 );
});

/*
================
a pet pickup waits out the pet's approach

The server allows the approach 9000 ms (cosground.go) and answers on the
pet's next tick; the shared 10 s inventory wait left one second for that
tick and the network, and prod sessions ended on "(0xB06D move 17)". An
answer that never comes still resumes the session (world.ts).
================
*/
test("a pet pickup waits out the pet's approach before it times out", () => {
	const f = fixture();
	const gameplay = createGameplay( () => {}, undefined, undefined, f.frame.read );
	gameplay.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x21c6 } ]
	} );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 29 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	view.setUint32( 16, 0x47, true );
	record[22] = 28;
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.command( { kind: "cos-pickup", gid: 2, target: 12 }, 1000, f.pet, f.frame.local );
	assert.equal( gameplay.take()?.inventoryPending, true );
	// The approach budget, the pet's next tick and a slow round trip.
	assert.doesNotThrow( () => gameplay.step( 1000 + 9000 + 2500, f.frame.local ) );
	assert.throws(
		() => gameplay.step( 1000 + 15000, f.frame.local ),
		/** @param {Error} error */ error =>
			error.cause === "inventory_transaction_timeout" && /0xB06D move 17/.test( error.message )
	);
});

test("pet cancellation retains the record until its removal notice and rejects foreign ownership", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x21c6 } ] } );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 28 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.command( { kind: "cos-cancel", gid: 2 }, 1, f.pet, f.frame.local );
	assert.deepEqual( sent, [ { opcode: 0x756c, payload: Uint8Array.of( 2, 0, 0, 0 ) } ] );
	gameplay.receive( { opcode: 0xb56c, payload: Uint8Array.of( 1 ) }, 2 );
	assert.equal( gameplay.take()?.cosRecords?.length, 1 );
	for ( const payload of [ new Uint8Array(), Uint8Array.of( 2 ), Uint8Array.of( 1, 0 ), Uint8Array.of( 3 ) ] ) {
		assert.throws( () => gameplay.receive( { opcode: 0xb56c, payload }, 3 ), /cancellation/ );
	}
	assert.throws( () =>
		gameplay.command( { kind: "cos-cancel", gid: 2 }, 4, { ...f.pet, ownerGid: 99 }, f.frame.local )
	);
	assert.equal( sent.length, 1 );
	// The server's removal notice (0x3508 case 1, 4D2140) precedes the despawn.
	gameplay.receive( { opcode: 0x3508, payload: Uint8Array.of( 2, 0, 0, 0, 1 ) }, 5 );
	gameplay.receive( { opcode: 0x36ab, payload: Uint8Array.of( 2, 0, 0, 0 ) }, 5 );
	assert.equal( gameplay.take()?.cosRecords?.length, 0 );
	view.setUint32( 8, 0, true );
	record[23] = 1;
	gameplay.receive( { opcode: 0x3158, payload: record }, 6 );
	assert.throws( () => gameplay.command( { kind: "cos-follow", gid: 2 }, 7, f.pet, f.frame.local ) );
	gameplay.command( { kind: "cos-cancel", gid: 2 }, 7, f.pet, f.frame.local );
	assert.equal( sent.length, 2 );
	assert.equal( sent[1].opcode, 0x756c );
	gameplay.dispose();
});

test("ride toggle keeps native state-byte ordering and waits for server authority", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x11c6 } ] } );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 21 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	const command = { kind: "cos-ride", gid: 2, mounted: true };
	gameplay.command( { ...command, kind: "cos-ride" }, 0, f.pet, f.frame.local );
	assert.deepEqual( sent, [ { opcode: 0x74b5, payload: Uint8Array.of( 1, 2, 0, 0, 0 ) } ] );
	assert.equal( f.frame.local.mountedOn, undefined );
	assert.equal( gameplay.receive( { opcode: 0xb4b5, payload: Uint8Array.of( 2, 4 ) }, 1 ), true );
	assert.equal( gameplay.take()?.cosRecords?.[0]?.gid, 2 );
	for ( const payload of [ Uint8Array.of( 2 ), Uint8Array.of( 2, 4, 0 ), Uint8Array.of( 3, 4 ) ] ) {
		assert.throws( () => gameplay.receive( { opcode: 0xb4b5, payload }, 1 ), /ride result/ );
	}
	assert.throws( () => gameplay.command( { kind: "cos-ride", gid: 2, mounted: false }, 0, f.pet, f.frame.local ) );
	gameplay.command( { kind: "cos-ride", gid: 2, mounted: false }, 1, f.pet, { ...f.frame.local, mountedOn: 2 } );
	assert.deepEqual( sent[1], { opcode: 0x74b5, payload: Uint8Array.of( 0, 2, 0, 0, 0 ) } );
	assert.throws( () =>
		gameplay.command( { kind: "cos-ride", gid: 2, mounted: true }, 2, { ...f.pet, ownerGid: 99 }, f.frame.local )
	);
});

/*
================
fixture
================
*/
function fixture( mode = 0xc7 ) {
	const owner = createCosPickup();
	/** @type {import('../../src/engine/contracts/world').EntityState} */
	const local = {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 0x6b4f,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Owner"
	};
	/** @type {import('../../src/engine/contracts/world').EntityState} */
	const pet = { ...local, gid: 2, refObjId: 9, kind: "cos", ownerGid: 1 };
	/** @type {import('../../src/engine/contracts/gameplay').CosRecord} */
	const record = { gid: 2, refObjId: 9, band: 4, hp: 100, mp: 0, status: 28, dead: false, commandMode: mode };
	const entities = new Map( [ [ local.gid, local ], [ pet.gid, pet ] ] );
	const frame = {
		now: 0,
		local,
		records: [ record ],
		sharedOwners: new Set( [ 3 ] ),
		read: gid => entities.get( gid )
	};
	/*
================
drop
================
	*/
	function drop( gid, x, typeFlags = 0x2ec, ownerJid = 0 ) {
		/** @type {import('../../src/engine/contracts/world').EntityState} */
		const entity = {
			...local,
			gid,
			kind: "ground-item",
			x,
			groundItem: { typeFlags, goldAmount: 10, ownerJid, tint: 0 }
		};
		entities.set( gid, entity );
		owner.track( { kind: "spawn", entity } );
		return entity;
	}
	return { owner, frame, entities, drop, pet };
}

test("native tag 8 request and nearest candidate preserve ordered-map tie breaks", () => {
	assert.deepEqual( [ ...cosPickupRequest( 2, 9 ).payload ], [ 2, 0, 0, 0, 8, 9, 0, 0, 0 ] );
	const f = fixture();
	f.drop( 11, 20 );
	f.drop( 10, 20 );
	f.drop( 9, 500 );
	assert.deepEqual( f.owner.step( f.frame ), [ cosPickupRequest( 2, 10 ) ] );
	assert.deepEqual( f.owner.step( { ...f.frame, now: 100 } ), [] );
	f.owner.result( { subtype: 1, selector: 8, gid: 2, itemGid: 10 }, 100 );
	assert.deepEqual( f.owner.step( { ...f.frame, now: 100 } ), [ cosPickupRequest( 2, 11 ) ] );
});

test("native category and reservation filters cover own, party, public and quest drops", () => {
	const f = fixture( 0x81 );
	f.drop( 4, 1, 0x2ec, 99 );
	f.drop( 5, 2, 0x2ec );
	f.drop( 6, 3, 0x2ec, 3 );
	f.drop( 7, 1, 0x82c, 1 );
	f.drop( 8, 1, 0x46c, 1 );
	assert.deepEqual( f.owner.step( f.frame ), [ cosPickupRequest( 2, 6 ) ] );
	f.owner.result( { subtype: 1, selector: 8, gid: 2, itemGid: 6 }, 0 );
	assert.deepEqual( f.owner.step( f.frame ), [] );
});

/*
================
the experimental item filter narrows the native loop by kind

Port-only (#841): with a kind mask the loop proposes only drops whose
kind (ground-item's pickupItemKind, from the documented type words) is
in the mask. It never widens: the native quest-special gate still wins
over a full mask, and no mask at all is the native loop.
================
*/
test("the experimental item filter narrows the native loop by kind and never widens it", () => {
	// Documented type words: gold heap 0x2ec, equipment 0x82c, HP potion
	// 0x08ec, elixir 0x0d6c, tablet 0x1dec, plain etc 0x06c.
	const cases = [
		[ 0x2ec, PICKUP_KIND_GOLD ],
		[ 0x82c, PICKUP_KIND_EQUIPMENT ],
		[ 0x08ec, PICKUP_KIND_POTIONS ],
		[ 0x0d6c, PICKUP_KIND_ALCHEMY ],
		[ 0x1dec, PICKUP_KIND_ALCHEMY ],
		[ 0x06c, PICKUP_KIND_OTHER ]
	];
	for ( const [typeFlags, kind] of cases ) {
		const admitted = fixture();
		admitted.drop( 10, 1, typeFlags );
		assert.deepEqual(
			admitted.owner.step( { ...admitted.frame, pickupKinds: kind } ),
			[ cosPickupRequest( 2, 10 ) ],
			`mask ${kind} admits its own drop 0x${typeFlags.toString( 16 )}`
		);
		const refused = fixture();
		refused.drop( 10, 1, typeFlags );
		assert.deepEqual(
			refused.owner.step( { ...refused.frame, pickupKinds: PICKUP_KIND_ALL & ~kind } ),
			[],
			`mask without ${kind} refuses 0x${typeFlags.toString( 16 )}`
		);
	}
	const empty = fixture();
	empty.drop( 10, 1, 0x2ec );
	assert.deepEqual( empty.owner.step( { ...empty.frame, pickupKinds: 0 } ), [] );
	const quest = fixture();
	quest.drop( 10, 1, 0x46c );
	assert.deepEqual( quest.owner.step( { ...quest.frame, pickupKinds: PICKUP_KIND_ALL } ), [] );
	const native = fixture();
	native.drop( 10, 1, 0x0d6c );
	assert.deepEqual( native.owner.step( native.frame ), [ cosPickupRequest( 2, 10 ) ] );
});

test("native retry classes expire on simulation time and stale results cannot release a new command", () => {
	const f = fixture();
	f.drop( 4, 1 );
	f.owner.step( f.frame );
	f.owner.result( { subtype: 2, selector: 8, result: 0x13, gid: 2, itemGid: 4 }, 0 );
	assert.deepEqual( f.owner.step( { ...f.frame, now: 4999 } ), [] );
	assert.equal( f.owner.step( { ...f.frame, now: 5000 } ).length, 1 );
	f.owner.result( { subtype: 1, selector: 8, gid: 2, itemGid: 99 }, 5000 );
	assert.deepEqual( f.owner.step( { ...f.frame, now: 6000 } ), [] );
	f.owner.result( { subtype: 2, selector: 8, result: 0x14, gid: 2, itemGid: 4 }, 6000 );
	assert.deepEqual( f.owner.step( { ...f.frame, now: 8999 } ), [] );
	assert.equal( f.owner.step( { ...f.frame, now: 9000 } ).length, 1 );
	assert.equal( f.owner.result( { subtype: 2, selector: 8, result: 0xb4, gid: 2, itemGid: 4 }, 9000 ), true );
});

test("smartPetPickup keeps 0x80 enabled on 0xB4/0xB5, pauses non-gold items without spam, and resumes when bag space changes", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ), undefined, undefined, f.frame.read );
	gameplay.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x21c6 } ]
	} );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 29 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	view.setUint32( 16, 0xc7, true );
	record[22] = 28;
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	// Spawn an equipment drop (20, typeFlags 0x82c), another non-gold drop (21, 0x06c), and a gold drop (22, 0x2ec).
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 20, 10, 0x82c ) } );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 21, 20, 0x06c ) } );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 22, 30, 0x2ec ) } );
	gameplay.step( 0, f.frame.local );
	assert.deepEqual( sent, [ cosPickupRequest( 2, 20 ) ] );
	// Server rejects equipment drop 20 with 0xB4 (pet bag full).
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 2, 8, 0xb4, 2, 0, 0, 0, 20, 0, 0, 0 ) }, 1 );
	// Default smartPetPickup ON: no 0x705B disable sent; bit 0x80 remains set.
	assert.equal( sent.length, 1 );
	const afterFull = gameplay.take();
	assert.equal( afterFull?.cosRecords?.[0]?.commandMode, 0xc7 );
	assert.equal( afterFull?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_CANT_PICKITEM1" );
	// Next step skips non-gold drops 20 and 21 (even past the 5000ms retry window) and picks up gold drop 22.
	gameplay.step( 6000, f.frame.local );
	assert.deepEqual( sent[1], cosPickupRequest( 2, 22 ) );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 1, 8, 2, 0, 0, 0, 22, 0, 0, 0 ) }, 6001 );
	gameplay.step( 12000, f.frame.local );
	assert.equal( sent.length, 2 );
	// Freeing space in the pet bag (0xB06D 0x12 drop or 0xB05B behavior receipt) clears the non-gold pause.
	gameplay.receive( { opcode: 0xb05b, payload: Uint8Array.of( 1, 2, 0, 0, 0, 2, 0xc7, 0, 0, 0 ) }, 12002 );
	gameplay.step( 12003, f.frame.local );
	assert.deepEqual( sent[2], cosPickupRequest( 2, 20 ) );
	gameplay.dispose();
});

/*
================
the item filter reaches the pickup loop through the experimental slice

Port-only (#841): no mask (the row off) is the native loop; a gold-only
mask holds an elixir drop the native loop would take; clearing the mask
restores the native proposal.
================
*/
test("the item filter reaches the pickup loop through the experimental slice", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ), undefined, undefined, f.frame.read );
	gameplay.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x21c6 } ]
	} );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const record = new Uint8Array( 29 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	view.setUint32( 16, 0xc7, true );
	record[22] = 28;
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 20, 10, 0x0d6c ) } );
	gameplay.entityLifecycle( { kind: "spawn", entity: f.drop( 21, 20, 0x0d6c ) } );
	gameplay.step( 0, f.frame.local );
	assert.deepEqual( sent, [ cosPickupRequest( 2, 20 ) ] );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 1, 8, 2, 0, 0, 0, 20, 0, 0, 0 ) }, 1 );
	gameplay.experimental( {
		predictedRunUp: false,
		webTransport: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true,
		petPickupKinds: PICKUP_KIND_GOLD
	} );
	gameplay.step( 2, f.frame.local );
	assert.equal( sent.length, 1 );
	gameplay.experimental( {
		predictedRunUp: false,
		webTransport: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true,
		petPickupKinds: undefined
	} );
	gameplay.step( 3, f.frame.local );
	assert.deepEqual( sent[1], cosPickupRequest( 2, 21 ) );
	gameplay.dispose();
});

test("dead, moving, disabled and foreign pets do not issue pickup; reset retires all work", () => {
	for ( const patch of [ { dead: true }, { hp: 0 }, { band: 3 }, { commandMode: 7 } ] ) {
		const f = fixture();
		f.drop( 4, 1 );
		assert.deepEqual( f.owner.step( { ...f.frame, records: [ { ...f.frame.records[0], ...patch } ] } ), [] );
	}
	const f = fixture();
	f.drop( 4, 1 );
	f.entities.set( 2, { ...f.pet, moving: true } );
	assert.deepEqual( f.owner.step( f.frame ), [] );
	f.entities.set( 2, { ...f.pet, moving: false, ownerGid: 99 } );
	assert.deepEqual( f.owner.step( f.frame ), [] );
	f.owner.clear();
	f.entities.set( 2, { ...f.pet, ownerGid: 1 } );
	assert.deepEqual( f.owner.step( f.frame ), [] );
});

test("ordinary riding horse uses its private record without a public owner tail", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x9c6 } ] } );
	gameplay.worldReady();
	gameplay.seed( f.frame.local );
	const horse = { ...f.pet, ownerGid: undefined };
	const record = new Uint8Array( 17 );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	assert.throws( () =>
		gameplay.command( { kind: "cos-ride", gid: 2, mounted: false }, 0, horse, { ...f.frame.local, mountedOn: 2 } )
	);
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.command( { kind: "cos-ride", gid: 2, mounted: false }, 1, horse, { ...f.frame.local, mountedOn: 2 } );
	assert.deepEqual( sent, [ { opcode: 0x74b5, payload: Uint8Array.of( 0, 2, 0, 0, 0 ) } ] );
	assert.equal( gameplay.take()?.activeCos?.gid, 2 );
	// The server's removal notice (0x3508 case 1, 4D2140) precedes the despawn.
	gameplay.receive( { opcode: 0x3508, payload: Uint8Array.of( 2, 0, 0, 0, 1 ) }, 2 );
	gameplay.receive( { opcode: 0x36ab, payload: Uint8Array.of( 2, 0, 0, 0 ) }, 2 );
	assert.equal( gameplay.take()?.cosRecords?.length, 0 );
	assert.throws( () => gameplay.command( { kind: "cos-ride", gid: 2, mounted: true }, 3, horse, f.frame.local ) );
	gameplay.dispose();
});

test("a refused target stays retired through a republication until it despawns", () => {
	const f = fixture();
	const entity = f.drop( 901, 10 );
	f.owner.result( { selector: 8, subtype: 2, itemGid: 901, gid: 7, result: 2 }, 0 );
	f.owner.track( { kind: "spawn", entity } );
	assert.equal( f.owner.isRetired( 901 ), true );
	f.owner.track( { kind: "despawn", gid: 901 } );
	assert.equal( f.owner.isRetired( 901 ), false );
});
