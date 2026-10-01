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
	assert.throws( () => gameplay.receive( receipt, 2 ), /Unmatched COS ground result/ );
	gameplay.step( 2, f.frame.local );
	assert.deepEqual( sent[2], cosPickupRequest( 2, 11 ) );
	gameplay.receive( { opcode: 0xb69e, payload: Uint8Array.of( 2, 8, 0xb4, 2, 0, 0, 0, 11, 0, 0, 0 ) }, 3 );
	assert.deepEqual( sent[3], { opcode: 0x705b, payload: Uint8Array.of( 2, 0, 0, 0, 2, 0x47, 0, 0, 0 ) } );
	assert.equal( gameplay.take()?.cosRecords?.[0]?.commandMode, 0xc7 );
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
	assert.equal( sent.length, 5 );
});

test("ride toggle keeps native state-byte ordering and waits for server authority", () => {
	const f = fixture();
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x11c6 } ] } );
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
