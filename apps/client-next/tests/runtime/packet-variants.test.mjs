/*
===========================================================================

packet-variants.test.mjs - tests for spawn-skills.ts, cos-record.ts,
gameplay.ts, inventory-item.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { decodeSpawnSkills, spawnSkillReferences } = await import(
	"../../src/engine/foundation/gameplay/spawn-skills.ts"
);
const { decodeCosRecord } = await import( "../../src/engine/foundation/gameplay/cos-record.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const u32 = n => {
		const b = Buffer.alloc( 4 );
		b.writeUInt32LE( n );
		return b;
	},
	u16 = n => {
		const b = Buffer.alloc( 2 );
		b.writeUInt16LE( n );
		return b;
	},
	byte = n => Buffer.from( [ n ] );
test("all remote/local token and status skill variants retain exact packet cursor", () => {
	for ( const local of [ false, true ] ) {
		for ( const token of [ false, true ] ) {
			for ( const status of [ false, true ] ) {
				const refs = spawnSkillReferences( [ { id: 17, token, status } ] );
				const bytes = Buffer.concat( [
					byte( 1 ),
					u32( 17 ),
					...(token ? [ u32( 90 ), ...(local ? [ u32( 123 ) ] : []) ] : []),
					...(status ? [ byte( 1 ) ] : [])
				] );
				const row = decodeSpawnSkills( Buffer.concat( [ bytes, byte( 211 ) ] ), 0, refs, local );
				assert.equal( row.next, bytes.length );
				assert.deepEqual( row.skills, [ {
					id: 17,
					token: token ? 90 : undefined,
					remaining: token && local ? 123 : undefined,
					status: status ? 1 : 2
				} ] );
				for ( let n = 0; n < bytes.length; n++ ) {
					assert.throws( () => decodeSpawnSkills( bytes.subarray( 0, n ), 0, refs, local ) );
				}
				assert.throws( () => decodeSpawnSkills( bytes, 0, new Map(), local ), /authority/ );
			}
		}
	}
	assert.throws( () => spawnSkillReferences( [ { id: 1, token: 1, status: false } ] ), /reference/ );
});
test("COS admits mixed inventory atomically with full equipment u64 options and labels", () => {
	const refs = new Map( [ [ 102, 0x11c6 ] ] ), items = new Map( [ [ 1, 0x182c ], [ 2, 0x6c ], [ 3, 0x46c ] ] );
	const head = Buffer.concat( [ u32( 7 ), u32( 102 ), u32( 100 ), u32( 50 ), byte( 28 ), byte( 3 ) ] );
	const equipment = Buffer.concat( [
		byte( 0 ),
		u32( 1 ),
		byte( 7 ),
		Buffer.alloc( 8, 255 ),
		u32( 19 ),
		byte( 1 ),
		Buffer.alloc( 8, 255 )
	] );
	const stack = Buffer.concat( [ byte( 3 ), u32( 2 ), u16( 99 ) ] ),
		label = Buffer.concat( [ byte( 4 ), u32( 3 ), u16( 1 ), u16( 3 ), Buffer.from( "pet" ) ] );
	const packet = Buffer.concat( [ head, equipment, stack, label, u32( 0 ) ] ),
		row = decodeCosRecord( packet, refs, items );
	assert.equal( defined( defined( row ).inventory ).length, 3 );
	assert.equal( defined( defined( row ).inventory )[0].variance, "18446744073709551615" );
	assert.equal( defined( defined( row ).inventory )[0].magic[0], "18446744073709551615" );
	assert.equal( defined( defined( row ).inventory )[1].quantity, 99 );
	assert.equal( defined( defined( row ).inventory )[2].label, "pet" );
	for ( let n = 0; n < packet.length; n++ ) {
		assert.throws( () => decodeCosRecord( packet.subarray( 0, n ), refs, items ) );
	}
	const owner = createGameplay( () => {} );
	owner.bootstrap( {
		refObjSnapshot: [ { kind: "cos", refObjId: 102, tidWord: 0x11c6 } ],
		refItemSnapshot: [ ...items ].map( ( [refObjId, typeFlags] ) => ({ refObjId, typeFlags }) )
	} );
	owner.take();
	assert.equal( owner.receive( { opcode: 0x3158, payload: packet }, 0 ), true );
	assert.equal( defined( defined( defined( owner.take() ).cosRecords )[0].inventory )[1].quantity, 99 );
	assert.throws( () => owner.receive( { opcode: 0x3158, payload: packet.subarray( 0, -1 ) }, 1 ) );
	assert.equal( owner.take(), null );
});
test("skill upgrade, rejection and absolute point updates preserve authoritative state", () => {
	const owner = createGameplay( () => {} );
	owner.bootstrap( {
		character: {
			skills: [ 3 ],
			quickSlots: [ { slot: 0, kind: 0x49, payload: 3 } ],
			masteries: [ { id: 257, level: 1 } ]
		},
		refSkillSnapshot: [ { id: 3, group: 174, level: 1, status: false, effectRider: false }, {
			id: 4,
			group: 174,
			level: 2,
			status: false,
			effectRider: false
		} ]
	} );
	owner.take();
	owner.receive( { opcode: 0xb2cb, payload: Buffer.concat( [ byte( 1 ), u32( 4 ) ] ) }, 0 );
	let state = owner.take();
	assert.deepEqual( defined( state ).skills, [ 4 ] );
	assert.equal( defined( defined( state ).quickSlots )[0].payload, 4 );
	owner.receive( { opcode: 0xb2cb, payload: Buffer.from( [ 2, 10 ] ) }, 0 );
	state = owner.take();
	assert.deepEqual( defined( state ).skills, [ 4 ] );
	assert.equal( defined( state ).error, null );
	assert.equal( defined( defined( defined( state ).notices ).at( -1 ) ).key, "UIIT_STT_SKILL_POINT_INSUFFICIENCY" );
	owner.receive( { opcode: 0xb165, payload: Buffer.concat( [ byte( 1 ), u32( 257 ), byte( 2 ) ] ) }, 0 );
	assert.deepEqual( defined( defined( owner.take() ).progression ).masteries, [ { id: 257, level: 2 } ] );
	for ( const amount of [ 100, 7, 7 ] ) {
		owner.receive( { opcode: 0x30b3, payload: Buffer.concat( [ byte( 2 ), u32( amount ), byte( 0 ) ] ) }, 0 );
		assert.equal( defined( defined( owner.take() ).progression ).skillPoints, amount );
	}
	owner.receive( { opcode: 0x30b3, payload: Buffer.concat( [ byte( 1 ), Buffer.alloc( 8, 255 ), byte( 0 ) ] ) }, 0 );
	assert.equal( defined( defined( owner.take() ).progression ).gold, "18446744073709551615" );
	assert.throws( () => owner.receive( { opcode: 0x30b3, payload: Buffer.from( [ 2, 1 ] ) }, 0 ) );
	assert.equal( owner.take(), null );
});

const { decodeInventoryItem } = await import( "../../src/engine/foundation/gameplay/inventory-item.ts" );
test("summon rentals and transformation items preserve signed time and consume exact boundaries", () => {
	// 0xcc = TID 3.2.1.0 summoner, 0x14c = TID 3.2.2.0 capsule, 0x9cc = TID 3.2.3.1 Magic Cube.
	const refs = new Map( [ [ 8, 0xcc ], [ 9, 0x14c ], [ 24663, 0x9cc ] ] ), objects = new Map( [ [ 102, 0x21c6 ] ] );
	const variants = [
		Buffer.concat( [ u32( 8 ), byte( 1 ) ] ),
		Buffer.concat( [
			u32( 8 ),
			byte( 2 ),
			u32( 102 ),
			u16( 3 ),
			Buffer.from( "pet" ),
			Buffer.alloc( 4, 255 ),
			byte( 2 ),
			byte( 0 ),
			u32( 3 ),
			Buffer.alloc( 4, 255 ),
			byte( 5 ),
			u32( 4 ),
			u32( 5 ),
			u32( 6 ),
			byte( 7 )
		] ),
		Buffer.concat( [ u32( 9 ), u32( 102 ) ] ),
		Buffer.concat( [ u32( 24663 ), u32( 7 ) ] )
	];
	for ( const p of variants ) {
		assert.equal( decodeInventoryItem( Buffer.concat( [ p, byte( 211 ) ] ), 0, refs, objects ).next, p.length );
		for ( let n = 0; n < p.length; n++ ) {
			assert.throws( () => decodeInventoryItem( p.subarray( 0, n ), 0, refs, objects ) );
		}
	}
	const pet = defined( decodeInventoryItem( variants[1], 0, refs, objects ).item ).summon;
	assert.equal( defined( pet ).remainingSeconds, -1 );
	assert.equal( defined( pet ).rentals[0].seconds, -1 );
	assert.equal( defined( pet ).rentals[1].tag, 6 );
	assert.equal( defined( decodeInventoryItem( variants[2], 0, refs, objects ).item ).transformRefObjId, 102 );
	assert.equal( defined( decodeInventoryItem( variants[3], 0, refs, objects ).item ).quantity, 7 );
});

test("native zero item reference clears a slot without consulting reference metadata", () => {
	assert.deepEqual( decodeInventoryItem( Buffer.from( [ 0, 0, 0, 0, 211 ] ), 0, new Map() ), {
		item: null,
		next: 4
	} );
	const owner = createGameplay( () => {} ), itemRefs = [ { refObjId: 2, typeFlags: 0x6c } ];
	owner.bootstrap( { refItemSnapshot: itemRefs } );
	owner.take();
	owner.receive( {
		opcode: 0xb06d,
		payload: Buffer.concat( [ byte( 1 ), byte( 6 ), byte( 13 ), u32( 2 ), u16( 5 ) ] )
	}, 0 );
	assert.equal( defined( owner.take() ).inventory.length, 1 );
	owner.receive( { opcode: 0xb06d, payload: Buffer.concat( [ byte( 1 ), byte( 6 ), byte( 13 ), u32( 0 ) ] ) }, 0 );
	assert.equal( defined( owner.take() ).inventory.length, 0 );
	const cos = Buffer.concat( [
		u32( 7 ),
		u32( 102 ),
		u32( 10 ),
		u32( 20 ),
		byte( 28 ),
		byte( 1 ),
		byte( 3 ),
		u32( 0 )
	] );
	assert.equal( defined( defined( decodeCosRecord( cos, new Map( [ [ 102, 0x31c6 ] ] ) ) ).inventory ).length, 0 );
	for ( let n = 0; n < cos.length; n++ ) {
		assert.throws( () => decodeCosRecord( cos.subarray( 0, n ), new Map( [ [ 102, 0x31c6 ] ] ) ) );
	}
});
