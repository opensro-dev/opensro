/*
===========================================================================

ground-items.test.mjs - tests for ground-item.ts, core.ts, entities.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { decodeGroundItem, nearestGroundItem } = await import( "../../src/engine/foundation/gameplay/ground-item.ts" );
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
// Independent literal gold/equipment/label goldens from server item/wire/grounditemrow_test.go.
const gold = Buffer.from( "e20e000060220000e19304004f6b00a096440000a0420000c6430000000001", "hex" );
const equipment = Buffer.from( "c32c000000e29304004f6b00000000000000000000000000000000", "hex" );
const label = Buffer.from( "d20400000000e39304004f6b00000000000000000000000000000000", "hex" );

test("unseeded higher-level loot receives public metadata before single spawn and visibility replay", () => {
	const core = createWorldCore( () => {} ),
		flush = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b;
		};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	const row = Buffer.from( equipment );
	row.writeUInt32LE( 50080 );
	const single = Buffer.concat( [ row, Buffer.from( [ 1 ] ) ] );
	core.receive( { opcode: 0x30d7, payload: single }, 0 );
	assert.ok( !defined( flush() ).events.some( e => e.kind === "spawn" ) );
	const reference = {
		opcode: 14,
		payload: Buffer.from(
			JSON.stringify( {
				version: 1,
				items: [ {
					refObjId: 50080,
					typeFlags: 0x8ac,
					name: "Higher-level equipment",
					maxStack: 1,
					nativeFields: { itemClass: 26 }
				} ]
			} )
		)
	};
	core.receive( reference, 1 );
	core.receive( { opcode: 0x30d7, payload: single }, 1 );
	let entity = defined( defined( flush() ).events.find( e => e.kind === "spawn" ) ).entity;
	assert.equal( entity.refObjId, 50080 );
	assert.equal( entity.name, "Higher-level equipment" );
	assert.equal( defined( entity.groundItem ).typeFlags, 0x8ac );
	core.receive( { opcode: 0x36ab, payload: row.subarray( 5, 9 ) }, 2 );
	flush();
	core.receive( reference, 3 ); // Another drop may repeat immutable metadata.
	core.receive( { opcode: 0x30cb, payload: Buffer.from( [ 1, 1, 0 ] ) }, 3 );
	core.receive( { opcode: 0x3417, payload: row }, 3 );
	core.receive( { opcode: 0x330a, payload: Buffer.alloc( 0 ) }, 3 );
	entity = defined( defined( flush() ).events.find( e => e.kind === "spawn" ) ).entity;
	assert.equal( entity.name, "Higher-level equipment" );
	assert.equal( defined( entity.groundItem ).appear, undefined );
	core.dispose();
});
test("single drops request native ITEM audio once; visibility lists do not replay it", () => {
	const e = createEntities();
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b;
	};
	flush();
	e.receive( { opcode: 0x30d7, payload: gold }, 100 );
	assert.deepEqual( defined( flush() ).events.filter( e => e.kind === "item-sound" ), [ {
		kind: "item-sound",
		cue: { handle: "SND_DROPITEM", typeFlags: 0x2ec },
		at: 100
	} ] );
	e.receive( { opcode: 0x36ab, payload: gold.subarray( 8, 12 ) }, 101 );
	flush();
	e.receive( { opcode: 0x30cb, payload: Buffer.from( [ 1, 1, 0 ] ) }, 102 );
	e.receive( { opcode: 0x3417, payload: gold.subarray( 0, -1 ) }, 102 );
	e.receive( { opcode: 0x330a, payload: Buffer.alloc( 0 ) }, 102 );
	assert.equal( defined( flush() ).events.filter( e => e.kind === "item-sound" || e.kind === "ui-sound" ).length, 0 );
	e.dispose();
});

test("non-gold single drops reach the category resolver without inventing a gold cue", () => {
	const e = createEntities();
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 11459, typeFlags: 0x8ac } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b;
	};
	flush();
	e.receive( { opcode: 0x30d7, payload: Buffer.concat( [ equipment, Buffer.from( [ 1 ] ) ] ) }, 100 );
	assert.deepEqual( defined( flush() ).events.filter( e => e.kind === "item-sound" || e.kind === "ui-sound" ), [ {
		kind: "item-sound",
		cue: { handle: "SND_DROPITEM", typeFlags: 0x8ac },
		at: 100
	} ] );
	e.dispose();
});

test("ownership expiry preserves the ground item and publishes its released ownership", () => {
	const e = createEntities();
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b;
	};
	flush();
	const owned = Buffer.concat( [ gold.subarray( 0, -3 ), Buffer.from( [ 1, 0xa3, 0x86, 1, 0, 7, 1 ] ) ] );
	e.receive( { opcode: 0x30d7, payload: owned } );
	const original = defined( defined( flush() ).events.find( row => row.kind === "spawn" ) ).entity;
	const payload = Buffer.alloc( 4 );
	payload.writeUInt32LE( original.gid );
	for ( let n = 0; n < 4; n++ ) {
		assert.throws( () => e.receive( { opcode: 0x31e2, payload: payload.subarray( 0, n ) } ) );
	}
	assert.equal( defined( defined( e.read( original.gid ) ).groundItem ).ownerJid, 100003 );
	e.receive( { opcode: 0x31e2, payload } );
	const updated = defined( defined( flush() ).events.find( row => row.kind === "state" ) ).entity;
	assert.equal( defined( updated.groundItem ).ownerJid, undefined );
	assert.equal( defined( updated.groundItem ).goldAmount, 8800 );
	assert.equal( defined( updated.groundItem ).tint, 7 );
	assert.equal( e.count(), 1 );
	e.dispose();
});
test("native item bands, ownership and single/list framing decode without offset guessing", () => {
	const decoded = decodeGroundItem( gold, 0x2ec, true, "Gold" );
	assert.equal( decoded.gid, 300001 );
	assert.equal( decoded.refObjId, 3810 );
	assert.equal( decoded.x, 1205 );
	assert.equal( decoded.y, 80 );
	assert.equal( decoded.z, 396 );
	assert.equal( defined( decoded.groundItem ).goldAmount, 8800 );
	assert.equal( defined( decoded.groundItem ).appear, 1 );
	assert.equal(
		defined( decodeGroundItem( gold.subarray( 0, -1 ), 0x2ec, false, "Gold" ).groundItem ).appear,
		undefined
	);
	assert.equal( decodeGroundItem( equipment, 0x8ac, false, "Hat" ).gid, 300002 );
	assert.equal( decodeGroundItem( label, 0x46c, false, "Label" ).name, "Label" );
	const owned = Buffer.concat( [ gold.subarray( 0, -3 ), Buffer.from( [ 1, 0xa3, 0x86, 1, 0, 7, 1 ] ) ] );
	assert.equal( defined( decodeGroundItem( owned, 0x2ec, true, "Gold" ).groundItem ).ownerJid, 100003 );
	for (
		const [row, type, single] of [ [ gold, 0x2ec, true ], [ equipment, 0x8ac, false ], [ label, 0x46c, false ], [
			owned,
			0x2ec,
			true
		] ]
	) {
		for ( let n = 0; n < row.length; n++ ) {
			assert.throws( () => decodeGroundItem( row.subarray( 0, n ), type, single, "" ) );
		}
		assert.throws( () => decodeGroundItem( Buffer.concat( [ row, Buffer.from( [ 0 ] ) ] ), type, single, "" ) );
	}
	assert.throws( () => decodeGroundItem( gold, 2, true, "" ), /type/ );
	const invalid = Buffer.from( gold );
	invalid.writeFloatLE( NaN, 14 );
	assert.throws( () => decodeGroundItem( invalid, 0x2ec, true, "" ), /position/ );
});
test("item selection/pickup is wire-driven and despawn clears the selected item", () => {
	const sent = [],
		core = createWorldCore( f => sent.push( f ) ),
		flush = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b;
		};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	core.receive( { opcode: 0x3369, payload: Buffer.from( [ 0x4f, 0x6b ] ) }, 0 );
	flush();
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	core.receive( { opcode: 0x30d7, payload: gold }, 0 );
	flush();
	core.command( { kind: "select", gid: 300001 }, 1 );
	core.command( { kind: "pickup", gid: 300001 }, 2 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 1, 2, 1, 0xe1, 0x93, 4, 0 ] );
	assert.equal( sent.at( -1 ).opcode, 0x72cd );
	assert.equal( core.count(), 2 );
	core.step( 2 );
	assert.equal( defined( defined( flush() ).events.find( e => e.kind === "gameplay" ) ).state.target, 300001 );
	core.receive( { opcode: 0x35c7, payload: Buffer.from( [ 7, 0, 0, 0, 64 ] ) }, 3 );
	const pose = defined( defined( flush() ).events.find( e => e.kind === "state" ) ).entity;
	assert.equal( pose.pickupRevision, 1 );
	assert.equal( pose.heading, 64 * 257 );
	core.step( 3 );
	assert.equal(
		defined( defined( defined( flush() ).events.find( e => e.kind === "gameplay" ) ).state.pose ).angle,
		64 * 257
	);
	core.receive( { opcode: 0x36ab, payload: gold.subarray( 8, 12 ) }, 4 );
	core.step( 4 );
	const result = flush();
	assert.ok( defined( result ).events.some( e => e.kind === "despawn" ) );
	assert.equal( defined( defined( result ).events.find( e => e.kind === "gameplay" ) ).state.target, 0 );
	assert.equal( core.count(), 1 );
	assert.throws( () => core.command( { kind: "pickup", gid: 300001 }, 5 ), /absent/ );
	core.dispose();
});

test("gold grant and balance refresh reach gameplay without an unhandled packet or duplicate pickup cue", () => {
	const core = createWorldCore( () => {} ),
		flush = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b;
		};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	core.receive( { opcode: 0xb06d, payload: Buffer.from( [ 1, 6, 254, 0xdc, 5, 0, 0 ] ) }, 1 );
	// 30B3 type 1: [u8 1][u64 balance][u8 notify] (0x3126 is the warehouse).
	const balance = Buffer.alloc( 10 );
	balance[0] = 1;
	balance.writeBigUInt64LE( 9007199254740993n, 1 );
	core.receive( { opcode: 0x30b3, payload: balance }, 2 );
	core.step( 2 );
	const events = defined( flush() ).events;
	const gameplay = defined( events.find( e => e.kind === "gameplay" ) ).state;
	assert.equal( defined( gameplay.progression ).gold, "9007199254740993" );
	// The 0xFE receipt announces the heap once; the silent refresh adds nothing.
	const gains = defined( gameplay.notices ).filter( n => n.key === "UIIT_MSG_STATE_GAIN_GOLD" );
	assert.deepEqual( gains.map( n => n.value ), [ 1500 ] );
	assert.ok( !events.some( e => e.kind === "native" || e.kind === "ui-sound" ) );
	for ( let n = 1; n < 10; n++ ) {
		assert.throws( () => core.receive( { opcode: 0x30b3, payload: balance.subarray( 0, n ) }, 3 ), /gold/ );
	}
	core.dispose();
});

test("an item pickup prints the native gain line: the stack it lands as, then only what a merge adds", () => {
	// 756CF0 prints UIIT_MSG_STATE_GET_ITEM_* for 0xB06D type 6 into a bag slot;
	// a consumable already in the slot reports the gained count.
	const core = createWorldCore( () => {} ),
		flush = () => {
			const b = core.take();
			if ( b ) core.ack( b.sequence );
			return b;
		};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c, nativeFields: { maxStack: 50 } } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	const stack = ( ref, quantity ) => {
		const body = Buffer.alloc( 6 );
		body.writeUInt32LE( ref );
		body.writeUInt16LE( quantity, 4 );
		return [ ...body ];
	};
	core.receive( { opcode: 0xb06d, payload: Buffer.from( [ 1, 6, 13, ...stack( 1, 3 ) ] ) }, 1 );
	core.receive( { opcode: 0xb06d, payload: Buffer.from( [ 1, 6, 13, ...stack( 1, 5 ) ] ) }, 2 );
	core.step( 2 );
	const gameplay = defined( defined( flush() ).events.find( e => e.kind === "gameplay" ) ).state;
	const gains = defined( gameplay.notices ).filter( n => n.key === "UIIT_MSG_STATE_GET_ITEM_EXPENDABLE" );
	assert.deepEqual( gains.map( n => n.value ), [ 3, 2 ] );
	core.dispose();
});

test("COS record gates mounting; result feedback never invents a ride and despawn clears capability", () => {
	const frames = [], core = createWorldCore( f => frames.push( f ) );
	const flush = () => {
		core.step( 0, false );
		const b = core.take();
		if ( b ) core.ack( b.sequence );
		return b;
	};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos" } ],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	core.receive( { opcode: 0x3369, payload: Buffer.from( [ 1, 1 ] ) }, 0 );
	flush();
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	const spawn = Buffer.alloc( 57 );
	spawn.writeUInt32LE( 3914 );
	spawn.writeUInt32LE( 8, 4 );
	spawn.writeUInt16LE( 257, 8 );
	spawn[25] = 1;
	spawn[45] = 1;
	spawn.writeFloatLE( 100, 40 );
	spawn[51] = 3;
	spawn.writeUInt32LE( 7, 52 );
	core.receive( { opcode: 0x30d7, payload: spawn }, 0 );
	let b = flush(), cos = defined( defined( b ).events.find( e => e.kind === "spawn" && e.entity.gid === 8 ) ).entity;
	assert.equal( cos.ownerGid, 7 );
	assert.equal( cos.pvpState, 3 );
	assert.throws( () => core.command( { kind: "mount", gid: 8 }, 0 ), /active COS/ );
	const record = Buffer.alloc( 21 );
	record.writeUInt32LE( 8 );
	record.writeUInt32LE( 3914, 4 );
	record.writeUInt32LE( 87829, 8 );
	core.receive( { opcode: 0x3158, payload: record }, 0 );
	core.command( { kind: "mount", gid: 8 }, 0 );
	assert.equal( frames.at( -1 ).opcode, 0x769e );
	assert.deepEqual( [ ...frames.at( -1 ).payload ], [ 8, 0, 0, 0, 11 ] );
	core.receive( { opcode: 0xb69e, payload: Buffer.from( [ 1, 11, 8, 0, 0, 0 ] ) }, 0 );
	b = flush();
	const state = defined( defined( b ).events.find( e => e.kind === "gameplay" ) ).state;
	assert.equal( defined( state.activeCos ).hp, 87829 );
	assert.equal( defined( state.cosResult ).selector, 11 );
	assert.ok( !defined( b ).events.some( e => e.kind === "state" ) );
	for ( let n = 0; n < record.length; n++ ) {
		assert.throws( () => core.receive( { opcode: 0x3158, payload: record.subarray( 0, n ) }, 0 ) );
	}
	const ride = Buffer.from( [ 1, 7, 0, 0, 0, 1, 8, 0, 0, 0 ] );
	core.receive( { opcode: 0xb4b5, payload: ride }, 0 );
	assert.equal( defined( defined( flush() ).events.find( e => e.kind === "state" ) ).entity.mountedOn, 8 );
	core.receive( { opcode: 0x35c7, payload: Buffer.from( [ 7, 0, 0, 0, 64 ] ) }, 0 );
	assert.equal( flush(), null, "mounted pickup does not change rider pose" );
	core.receive( { opcode: 0x36ab, payload: Buffer.from( [ 8, 0, 0, 0 ] ) }, 0 );
	b = flush();
	assert.equal( defined( defined( b ).events.find( e => e.kind === "gameplay" ) ).state.activeCos, undefined );
	assert.equal( defined( defined( b ).events.find( e => e.kind === "state" ) ).entity.mountedOn, undefined );
	core.dispose();
});

test("a mounted rider's position correction moves the local pose to the mount, not where it mounted", () => {
	const core = createWorldCore( () => {} );
	const flush = () => {
		core.step( 0, false );
		const b = core.take();
		if ( b ) core.ack( b.sequence );
		return b;
	};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos" } ],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	core.receive( { opcode: 0x3369, payload: Buffer.from( [ 1, 1 ] ) }, 0 );
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	const spawn = Buffer.alloc( 57 );
	spawn.writeUInt32LE( 3914 );
	spawn.writeUInt32LE( 8, 4 );
	spawn.writeUInt16LE( 257, 8 );
	spawn[25] = 1;
	spawn[45] = 1;
	spawn.writeFloatLE( 100, 40 );
	spawn.writeUInt32LE( 7, 52 );
	core.receive( { opcode: 0x30d7, payload: spawn }, 0 );
	core.receive( { opcode: 0xb4b5, payload: Buffer.from( [ 1, 7, 0, 0, 0, 1, 8, 0, 0, 0 ] ) }, 0 );
	flush();
	// The server's attack approach settles the rider (B2F5 names the rider's
	// GID). Entities apply it to the mount; the local pose must follow it.
	const position = Buffer.alloc( 20 );
	position.writeUInt32LE( 7 );
	position.writeUInt16LE( 257, 4 );
	position.writeFloatLE( 500, 6 );
	position.writeFloatLE( 2, 10 );
	position.writeFloatLE( 600, 14 );
	core.receive( { opcode: 0xb2f5, payload: position }, 10 );
	const events = defined( flush() ).events.filter( e => e.kind === "gameplay" );
	const pose = defined( defined( events[events.length - 1] ).state.pose );
	assert.equal( pose.x, 500 );
	assert.equal( pose.z, 600 );
	core.dispose();
});

test("all ground spawn tail branches consume claimant bytes; special names append without replacing the item name", () => {
	const base = gold.subarray( 0, -1 );
	for ( const state of [ 0, 1, 2, 3, 4, 5, 6, 7 ] ) {
		const tail = state === 5 || state === 6 ? Buffer.from( [ state, 7, 0, 0, 0 ] ) : Buffer.from( [ state ] );
		const decoded = decodeGroundItem( Buffer.concat( [ base, tail ] ), 0x2ec, true, "Gold" );
		assert.equal( defined( decoded.groundItem ).claimantGid, state === 5 ? 7 : undefined );
		if ( state === 5 || state === 6 ) {
			for ( let i = 1; i < 5; i++ ) {
				assert.throws(
					() => decodeGroundItem( Buffer.concat( [ base, tail.subarray( 0, i ) ] ), 0x2ec, true, "Gold" ),
					/Truncated/
				);
			}
		}
	}
	const suffix = Buffer.concat( [
		label.subarray( 0, 4 ),
		Buffer.from( [ 3, 0, 65, 66, 67 ] ),
		label.subarray( 6 )
	] );
	assert.equal( decodeGroundItem( suffix, 0x46c, false, "Quest" ).name, "Quest(*ABC)" );
	assert.equal( decodeGroundItem( suffix, 0x4ec, false, "Trade" ).name, "Trade(ABC)" );
});

test("pickup shortcut uses native strict 50-unit 3D distance, adjacent sectors and claim exclusion", () => {
	const origin = { regionId: 257, x: 1910, y: 0, z: 0, angle: 0 },
		base = {
			kind: "ground-item",
			name: "Item",
			refObjId: 1,
			heading: 0,
			groundItem: { typeFlags: 0x2ec, goldAmount: 1, tint: 0 }
		};
	const across = { ...base, gid: 2, regionId: 258, x: 0, y: 0, z: 0 }, above = { ...across, gid: 3, y: 51 };
	assert.equal( nearestGroundItem( [ above, across ], origin ), 2 );
	assert.equal( nearestGroundItem( [ { ...across, x: 40 } ], origin ), 0, "exactly 50 units excluded" );
	assert.equal( nearestGroundItem( [ { ...across, x: 39.9 } ], origin ), 2 );
	assert.equal(
		nearestGroundItem( [ { ...across, groundItem: { ...across.groundItem, claimantGid: 7 } } ], origin ),
		0
	);
});

test("pickup claims persist for a live character and release when the claimant leaves", () => {
	const e = createEntities();
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b;
	};
	flush();
	e.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	flush();
	e.receive( {
		opcode: 0x30d7,
		payload: Buffer.concat( [ gold.subarray( 0, -1 ), Buffer.from( [ 5, 7, 0, 0, 0 ] ) ] )
	}, 0 );
	flush();
	e.step( 1 );
	assert.equal( defined( defined( e.read( 300001 ) ).groundItem ).claimantGid, 7 );
	flush();
	e.receive( { opcode: 0x36ab, payload: Buffer.from( [ 7, 0, 0, 0 ] ) }, 2 );
	flush();
	e.step( 2 );
	assert.equal( defined( defined( e.read( 300001 ) ).groundItem ).claimantGid, undefined );
	e.dispose();
});

test("every published drop material uses the shared native cutoff while opaque materials stay opaque", async () => {
	const { readFile } = await import( "node:fs/promises" ),
		manifest = JSON.parse(
			await readFile( CLIENT_PUBLIC_ROOT + "/assets/itemdrop/manifest.json", "utf8" )
		);
	let masked = 0, opaque = 0;
	for ( const [name, row] of Object.entries( manifest.models ) ) {
		assert.ok( row.glb, name + " must have its authored model" );
		const b = await readFile( CLIENT_PUBLIC_ROOT + row.glb ),
			j = JSON.parse( b.subarray( 20, 20 + b.readUInt32LE( 12 ) ) );
		for ( const material of j.materials ?? [] ) {
			if ( material.alphaMode === "MASK" ) {
				assert.equal( material.alphaCutoff, 128 / 255, name );
				masked++;
			} else {
				assert.equal( material.alphaMode, undefined, name );
				opaque++;
			}
		}
	}
	assert.ok( masked > 25, "covers the complete drop catalogue, including all fanfare parts" );
	assert.ok( opaque > 0, "opaque materials were not converted into cutouts" );
});

test("ground item spawn and late navigation resolve surface height before model publication", () => {
	let height = 125;
	const e = createEntities( p => ({ ...p, y: height }) );
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b;
	};
	flush();
	e.receive( { opcode: 0x30d7, payload: gold }, 100 );
	const item = defined( defined( flush() ).events.find( x => x.kind === "spawn" ) ).entity;
	assert.equal( item.y, 125 );
	assert.equal( item.x, 1205 );
	assert.equal( defined( item.groundItem ).goldAmount, 8800 );
	height = 140;
	e.groundSpawns();
	assert.equal( defined( defined( flush() ).events.find( x => x.kind === "state" ) ).entity.y, 140 );
	e.groundSpawns();
	assert.equal( flush(), null, "unchanged surface publishes nothing" );
	e.dispose();
});
