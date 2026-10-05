/*
===========================================================================

cos-item-use.test.mjs - pet item wire targets and satiety publication

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { cosItemUseTail, companionItemTargetCommand } = await import(
	"../../src/engine/foundation/gameplay/cos-item-use.ts"
);

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
flags
================
*/
function flags( group, subtype ) {
	return 3 << 2 | 3 << 5 | group << 7 | subtype << 11;
}

const pet = { gid: 9001, refObjId: 6106, band: 3, hp: 10, mp: 0, dead: false, status: 0 };

test("all pet recovery and cure families carry the selected owned GID", () => {
	for ( const type of [ flags( 1, 4 ), flags( 1, 5 ), flags( 1, 7 ), flags( 1, 9 ), flags( 2, 7 ) ] ) {
		assert.deepEqual( cosItemUseTail( type, [], { records: [ pet ] } ), Uint8Array.of( 41, 35, 0, 0 ) );
		assert.throws( () => cosItemUseTail( type, [], { records: [ pet ], selectedGid: 99 } ), /owned companion/ );
		assert.throws( () => cosItemUseTail( type, [], { records: [ { ...pet, dead: true } ] } ), /owned companion/ );
	}
	assert.deepEqual( cosItemUseTail( flags( 1, 1 ), [] ), new Uint8Array() );
	assert.throws( () => cosItemUseTail( flags( 1, 9 ), [], { records: [ { ...pet, band: 4 } ] } ) );
	assert.throws( () => cosItemUseTail( flags( 1, 4 ), [], { records: [ pet, { ...pet, gid: 9002 } ] } ) );
	assert.deepEqual(
		cosItemUseTail( flags( 1, 4 ), [], { records: [ pet, { ...pet, gid: 9002 } ], selectedGid: 9001 } ),
		Uint8Array.of( 41, 35, 0, 0 )
	);
});

test("revival identifies the dead summoner slot and never appends a live GID", () => {
	const item = {
		slot: 21,
		refObjId: 1,
		typeFlags: 0xcc,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		summon: { state: 4, rentals: [] }
	};
	assert.deepEqual( cosItemUseTail( flags( 1, 6 ), [ item ] ), Uint8Array.of( 21 ) );
	assert.throws( () => cosItemUseTail( flags( 1, 6 ), [ { ...item, summon: { state: 3, rentals: [] } } ] ) );
	assert.throws( () => cosItemUseTail( flags( 1, 6 ), [ item, { ...item, slot: 22 } ] ) );
	assert.deepEqual(
		cosItemUseTail( flags( 1, 6 ), [ item, { ...item, slot: 22 } ], { records: [], revivalSlot: 22 } ),
		Uint8Array.of( 22 )
	);
});

test("native satiety publication updates only the named attack-pet record", () => {
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	const typeFlags = flags( 1, 9 );
	gameplay.bootstrap( {
		refObjSnapshot: [ { kind: "cos", refObjId: 6106, tidWord: 0x19c6 } ],
		refItemSnapshot: [ { refObjId: 7553, typeFlags } ],
		equipItems: [ { refObjId: 7553, slot: 21, body: [ 129, 29, 0, 0, 1, 0 ] } ]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Owner"
	} );
	const record = new Uint8Array( 39 ), v = new DataView( record.buffer );
	v.setUint32( 0, 9001, true );
	v.setUint32( 4, 6106, true );
	v.setUint32( 8, 10, true );
	record[24] = 1;
	v.setUint16( 25, 2999, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.command( { kind: "item-use", slot: 21 }, 1, undefined );
	assert.deepEqual( sent, [ {
		opcode: 0x75bd,
		payload: Uint8Array.of( 21, typeFlags & 255, typeFlags >>> 8, 41, 35, 0, 0 )
	} ] );
	gameplay.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 1, 21, 0, 0, typeFlags & 255, typeFlags >>> 8 ) }, 1 );
	const payload = Uint8Array.of( 41, 35, 0, 0, 4, 160, 15 );
	gameplay.receive( { opcode: 0x3508, payload }, 1 );
	assert.equal( gameplay.take()?.cosRecords?.[0]?.satiety, 4000 );
	assert.throws( () => gameplay.receive( { opcode: 0x3508, payload: payload.slice( 0, 6 ) }, 2 ), /satiety update/ );
	const invalid = Uint8Array.from( payload );
	new DataView( invalid.buffer ).setUint16( 5, 10001, true );
	assert.throws( () => gameplay.receive( { opcode: 0x3508, payload: invalid }, 2 ), /satiety value/ );
	assert.equal( gameplay.take(), null, "invalid packets do not publish a changed record" );
	gameplay.dispose();
});

test("a growing pet's next form replaces its record reference and fills satiety", () => {
	const gameplay = createGameplay( () => {} );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 6106, tidWord: 0x19c6 } ] } );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Owner"
	} );
	const record = new Uint8Array( 39 ), v = new DataView( record.buffer );
	v.setUint32( 0, 9001, true );
	v.setUint32( 4, 6106, true );
	v.setUint32( 8, 10, true );
	record[24] = 1;
	v.setUint16( 25, 2999, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	gameplay.take();
	const change = Uint8Array.of( 41, 35, 0, 0, 7, 0xdb, 0x17, 0, 0 );
	// The entity lane still needs the frame: it swaps the model.
	assert.equal( gameplay.receive( { opcode: 0x3508, payload: change }, 1 ), false );
	const grown = gameplay.take()?.cosRecords?.[0];
	assert.equal( grown?.refObjId, 6107 );
	assert.equal( grown?.satiety, 10000 );
	assert.throws( () => gameplay.receive( { opcode: 0x3508, payload: change.slice( 0, 8 ) }, 2 ), /reference change/ );
	gameplay.dispose();
});

test("renewal and revival drag targets use owned summoner slots", () => {
	const source = {
		slot: 25,
		refObjId: 998,
		typeFlags: flags( 13, 12 ),
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: []
	};
	const target = { ...source, slot: 24, typeFlags: 0x10cc, summon: { state: 4, rentals: [] } };
	assert.deepEqual( companionItemTargetCommand( source, target ), { kind: "item-use", slot: 25, summonerSlot: 24 } );
	assert.deepEqual( cosItemUseTail( source.typeFlags, [ target ] ), Uint8Array.of( 24 ) );
	assert.throws( () => cosItemUseTail( source.typeFlags, [ target, { ...target, slot: 26 } ] ) );
	assert.deepEqual(
		cosItemUseTail( source.typeFlags, [ target, { ...target, slot: 26 } ], { records: [], summonerSlot: 26 } ),
		Uint8Array.of( 26 )
	);
	assert.equal( companionItemTargetCommand( source, { ...target, typeFlags: 0x08cc } ), null );
	assert.equal( companionItemTargetCommand( source, { ...target, summon: { state: 1, rentals: [] } } ), null );
	assert.deepEqual( companionItemTargetCommand( { ...source, typeFlags: flags( 1, 6 ) }, target ), {
		kind: "item-use",
		slot: 25,
		revivalSlot: 24
	} );
});
