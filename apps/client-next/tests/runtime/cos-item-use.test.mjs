/*
===========================================================================

cos-item-use.test.mjs - pet item wire targets and satiety publication

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const {
	cosItemUseTail,
	companionItemUseNotice,
	companionItemTargetCommand,
	autoPotionTarget,
	autoPotionTargetNotice,
	createCosSelection
} = await import(
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
		assert.deepEqual(
			cosItemUseTail( type, [], { records: [ { ...pet, dead: true, hp: 0 } ] } ),
			Uint8Array.of( 41, 35, 0, 0 ),
			"native client lets server decide dead-target admission"
		);
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
		typeFlags: 0x08cc,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		summon: { state: 4, rentals: [] }
	};
	assert.throws( () => cosItemUseTail( flags( 1, 6 ), [ item ] ), /not admitted/ );
	assert.throws( () =>
		cosItemUseTail( flags( 1, 6 ), [ { ...item, summon: { state: 3, rentals: [] } } ], {
			records: [],
			revivalSlot: 21,
			summonedCharacterTypeFlags: 0x19c6
		} )
	);
	assert.throws( () => cosItemUseTail( flags( 1, 6 ), [ item, { ...item, slot: 22 } ] ) );
	assert.deepEqual(
		cosItemUseTail( flags( 1, 6 ), [ item, { ...item, slot: 22 } ], {
			records: [],
			revivalSlot: 22,
			summonedCharacterTypeFlags: 0x19c6
		} ),
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

test("companion drags retain explicit targets even when they are incompatible", () => {
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
	for (
		const target of [
			{ ...source, slot: 24, typeFlags: 0x10cc, summon: { state: 4, rentals: [] } },
			{ ...source, slot: 24, typeFlags: 0x08cc, summon: { state: 1, rentals: [] } },
			{ ...source, slot: 1 }
		]
	) {
		assert.deepEqual( companionItemTargetCommand( source, target ), {
			kind: "item-use",
			slot: 25,
			summonerSlot: target.slot
		} );
		assert.deepEqual( companionItemTargetCommand( { ...source, typeFlags: flags( 1, 6 ) }, target ), {
			kind: "item-use",
			slot: 25,
			revivalSlot: target.slot
		} );
	}
});

/*
================
companionUseFixture

Real item bodies resolve retained character references independently of the
summoner item's subtype. Fresh bodies have no retained character reference.
================
*/
function companionUseFixture( t, input ) {
	const sent = [], gameplay = createGameplay( frame => sent.push( frame ) );
	t.after( () => gameplay.dispose() );
	const target = [ 2, 0, 0, 0, input.state ];
	if ( input.state !== 1 ) {
		target.push( 3, 0, 0, 0, 0, 0 );
		if ( input.character === 0x21c6 ) target.push( 0, 0, 0, 0 );
		target.push( 0 );
	}
	gameplay.bootstrap( {
		refObjSnapshot: [ { kind: "cos", refObjId: 3, tidWord: input.character } ],
		refItemSnapshot: [
			{ refObjId: 1, typeFlags: input.source },
			{ refObjId: 2, typeFlags: input.item }
		],
		equipItems: [
			{ refObjId: 1, slot: 13, body: [ 1, 0, 0, 0, 1, 0 ] },
			{ refObjId: 2, slot: 14, body: target }
		]
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
	gameplay.take();
	return { gameplay, sent };
}

for ( const family of [ "grass", "clock" ] ) {
	for ( const character of [ 0x19c6, 0x21c6 ] ) {
		for ( const state of [ 1, 2, 3, 4 ] ) {
			test(`${family} resolves character ${character} in rental state ${state}`, t => {
				const grass = family === "grass", source = grass ? flags( 1, 6 ) : flags( 13, 12 );
				const { gameplay, sent } = companionUseFixture( t, {
					source,
					character,
					state,
					// Deliberately opposite: item subtype must not classify the pet.
					item: character === 0x19c6 ? 0x10cc : 0x08cc
				} );
				gameplay.command(
					{
						kind: "item-use",
						slot: 13,
						...(grass ? { revivalSlot: 14 } : { summonerSlot: 14 })
					},
					1,
					undefined
				);
				const wrong = state === 1 ? !grass : character !== (grass ? 0x19c6 : 0x21c6);
				const refused = wrong || grass && state !== 1 && state !== 4;
				const published = gameplay.take();
				assert.ok( published );
				if ( refused ) {
					assert.deepEqual( sent, [] );
					assert.equal( published.inventoryPending, false );
					const notice = published.notices?.at( -1 );
					assert.equal(
						notice?.key,
						wrong ? "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT" : "UIIT_MSG_COSPETERR_CANT_USEITEM"
					);
					assert.equal( notice?.nativeType, 5 );
				} else {
					assert.deepEqual( sent, [ {
						opcode: 0x75bd,
						payload: Uint8Array.of( 13, source & 255, source >>> 8, 14 )
					} ] );
					assert.equal( published.inventoryPending, true );
				}
			});
		}
	}
}

test("Grass requires explicit targeting and missing Clock references refuse without sending", t => {
	const { gameplay, sent } = companionUseFixture( t, {
		source: flags( 1, 6 ),
		character: 0x19c6,
		state: 4,
		item: 0x08cc
	} );
	gameplay.command( { kind: "item-use", slot: 13 }, 1, undefined );
	assert.deepEqual( sent, [] );
	assert.equal( gameplay.take()?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT" );
	const target = {
		slot: 14,
		refObjId: 2,
		typeFlags: 0x10cc,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		summon: { state: 1, rentals: [] }
	};
	assert.equal(
		companionItemUseNotice( flags( 13, 12 ), [ target ], { records: [], summonerSlot: 14 } )?.key,
		"UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT"
	);
	assert.throws(
		() => companionItemUseNotice( flags( 13, 12 ), [ target ], { records: [], summonerSlot: 256 } ),
		/Invalid companion target slot/
	);
});

test("Grass passes a missing character reference while Clock refuses it", () => {
	const target = {
		slot: 14,
		refObjId: 2,
		typeFlags: 0x10cc,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		summon: { state: 4, refObjId: 999, rentals: [] }
	};
	assert.equal( companionItemUseNotice( flags( 1, 6 ), [ target ], { records: [], revivalSlot: 14 } ), null );
	assert.deepEqual(
		cosItemUseTail( flags( 1, 6 ), [ target ], { records: [], revivalSlot: 14 } ),
		Uint8Array.of( 14 )
	);
	assert.equal(
		companionItemUseNotice( flags( 13, 12 ), [ target ], { records: [], summonerSlot: 14 } )?.key,
		"UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT"
	);
	for ( const state of [ 1, 2, 3, 4 ] ) {
		assert.equal(
			companionItemUseNotice( flags( 13, 12 ), [ { ...target, summon: { ...target.summon, state } } ], {
				records: [],
				summonerSlot: 14,
				summonedCharacterTypeFlags: 0x21c6
			} ),
			null,
			"a resolved pickup character is accepted independently of rental state"
		);
	}
	assert.equal(
		companionItemUseNotice( flags( 1, 6 ), [ target ], { records: [], revivalSlot: 99 } )?.key,
		"UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT"
	);
});

test("wrong occupied targets publish refusal without taking the inventory lane", t => {
	const { gameplay, sent } = companionUseFixture( t, {
		source: flags( 13, 12 ),
		character: 0x21c6,
		state: 4,
		item: 0x10cc
	} );
	gameplay.command( { kind: "item-use", slot: 13, summonerSlot: 13 }, 1, undefined );
	assert.deepEqual( sent, [] );
	assert.equal( gameplay.take()?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT" );
	gameplay.command( { kind: "item-use", slot: 13, summonerSlot: 14 }, 2, undefined );
	assert.equal( sent.length, 1 );
	assert.throws( () => gameplay.receive( { opcode: 0x3645, payload: Uint8Array.of( 14, 64 ) }, 3 ), /Truncated/ );
});

test("automatic pet use requires the selected compatible companion and preserves retries", () => {
	for ( const group of [ 1, 2 ] ) {
		for ( const subtype of group === 1 ? [ 4, 5, 7, 9 ] : [ 7 ] ) {
			const tid = flags( group, subtype );
			assert.equal( autoPotionTarget( tid, [], 9001 ), null );
			assert.equal( autoPotionTarget( tid, [ pet ], 9999 ), null );
			for ( const band of [ 4, 5 ] ) assert.equal( autoPotionTarget( tid, [ { ...pet, band } ], 9001 ), null );
			assert.equal( autoPotionTarget( tid, [ pet, { ...pet, gid: 9002 } ], 9002 )?.selectedGid, 9002 );
		}
	}
	assert.equal(
		autoPotionTarget( flags( 1, 6 ), [ pet ], 9001 ),
		null,
		"quickslot revival has no dragged summoner slot"
	);
	for ( const satiety of [ 9900, 9999, 10000 ] ) {
		assert.equal( autoPotionTarget( flags( 1, 9 ), [ { ...pet, satiety } ], 9001 ), null );
	}
	assert.ok( autoPotionTarget( flags( 1, 9 ), [ { ...pet, satiety: 9899 } ], 9001 ) );
});

test("structure repair carries the current target window GID, including no target", () => {
	assert.deepEqual( cosItemUseTail( flags( 1, 10 ), [] ), Uint8Array.of( 0, 0, 0, 0 ) );
	assert.deepEqual(
		cosItemUseTail( flags( 1, 10 ), [], autoPotionTarget( flags( 1, 10 ), [], 0, 0x12345678 ) ?? undefined ),
		Uint8Array.of( 0x78, 0x56, 0x34, 0x12 )
	);
});

test("native guild representative selection survives additional soldiers and removal", () => {
	const selection = createCosSelection(), records = new Map();
	const first = { ...pet, gid: 100, band: 5 }, second = { ...first, gid: 101 };
	for ( const record of [ first, pet, second ] ) {
		records.set( record.gid, record );
		selection.add( record, records );
	}
	assert.equal( selection.selected(), pet.gid, "additional soldier does not steal the selected pet" );
	assert.deepEqual( selection.statusRecords( records ).map( record => record.gid ), [ first.gid, pet.gid ] );
	selection.remove( first.gid, records );
	records.delete( first.gid );
	assert.equal( selection.selected(), pet.gid, "removing the representative preserves selection" );
	assert.deepEqual( selection.statusRecords( records ).map( record => record.gid ), [ first.gid, pet.gid ] );
	selection.remove( pet.gid, records );
	records.delete( pet.gid );
	assert.equal( selection.selected(), pet.gid, "native first-tab selection refuses its now missing representative" );
	assert.equal( autoPotionTarget( flags( 1, 4 ), [ ...records.values() ], selection.selected() ), null );
	selection.remove( second.gid, records );
	records.delete( second.gid );
	assert.equal( selection.selected(), 0 );
	const next = { ...first, gid: 102 };
	records.set( next.gid, next );
	selection.add( next, records );
	assert.equal( selection.selected(), next.gid );
	assert.deepEqual( selection.statusRecords( records ).map( record => record.gid ), [ next.gid ] );
	selection.reset();
	assert.equal( selection.selected(), 0 );
});

test("quest companions select their native default class while unknown removals preserve selection", () => {
	const selection = createCosSelection(), records = new Map();
	for ( const record of [ pet, { ...pet, gid: 9002 }, { ...pet, gid: 9003, band: 6 } ] ) {
		records.set( record.gid, record );
		selection.add( record, records );
	}
	assert.equal( selection.selected(), 9003 );
	assert.equal( autoPotionTarget( flags( 1, 4 ), [ ...records.values() ], selection.selected() )?.selectedGid, 9003 );
	selection.remove( 9999, records );
	assert.equal( selection.selected(), 9003 );
	selection.remove( 9003, records );
	records.delete( 9003 );
	assert.equal( selection.selected(), 9001, "ordinary removal selects the first status tab" );
	selection.select( 9002, records );
	selection.select( 9999, records );
	assert.equal( selection.selected(), 9002 );
});

test("owned COS packet lifecycle publishes the same selection used by automatic items", () => {
	const gameplay = createGameplay( () => {} );
	gameplay.bootstrap( {
		refObjSnapshot: [
			{ kind: "cos", refObjId: 6106, tidWord: 0x19c6 },
			{ kind: "cos", refObjId: 8000, tidWord: 0x29c6 }
		]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		name: "Owner",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	} );
	/*
================
add
================
	*/
	function add( gid, guild ) {
		const payload = new Uint8Array( guild ? 17 : 39 ), v = new DataView( payload.buffer );
		v.setUint32( 0, gid, true );
		v.setUint32( 4, guild ? 8000 : 6106, true );
		v.setUint32( 8, 10, true );
		if ( !guild ) payload[24] = 1;
		gameplay.receive( { opcode: 0x3158, payload }, 0 );
		return gameplay.take()?.selectedCosGid;
	}
	/*
================
remove
================
	*/
	function remove( gid ) {
		const payload = new Uint8Array( 4 );
		new DataView( payload.buffer ).setUint32( 0, gid, true );
		gameplay.receive( { opcode: 0x36ab, payload }, 1 );
		return gameplay.take()?.selectedCosGid;
	}
	assert.equal( add( 100, true ), 100 );
	assert.equal( add( 9001, false ), 9001 );
	assert.equal( add( 101, true ), 9001 );
	assert.equal( remove( 9999 ), 9001 );
	assert.equal( remove( 100 ), 9001 );
	assert.equal( remove( 9001 ), 9001 );
	assert.equal( remove( 101 ), 0 );
	assert.equal( add( 9002, false ), 9002 );
	gameplay.resetWorld();
	assert.equal( gameplay.take()?.selectedCosGid, 0 );
	gameplay.dispose();
});

test("automatic companion refusal feedback distinguishes absent, incompatible, revival and full food", () => {
	assert.deepEqual( autoPotionTargetNotice( flags( 1, 4 ), [], 0 ), {
		key: "UIIT_MSG_COSPETERR_CANT_USEITEM",
		value: 0,
		nativeType: 5
	} );
	for ( const band of [ 4, 5 ] ) {
		assert.equal(
			autoPotionTargetNotice( flags( 1, 4 ), [ { ...pet, band } ], pet.gid )?.key,
			"UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT"
		);
	}
	assert.equal( autoPotionTargetNotice( flags( 1, 6 ), [], 0 )?.key, "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT" );
	assert.equal(
		autoPotionTargetNotice( flags( 1, 9 ), [ { ...pet, satiety: 9900 } ], pet.gid )?.key,
		"UIIT_MSG_COSPETERR_HGPFULL_NODRINK"
	);
	assert.equal( autoPotionTargetNotice( flags( 1, 9 ), [ { ...pet, satiety: 9899 } ], pet.gid ), null );
	assert.equal( autoPotionTargetNotice( flags( 1, 1 ), [], 0 ), null );
});

test("the reverse return scroll carries its chosen point as one byte (6971B0 case 0x1E)", () => {
	const scroll = flags( 3, 3 );
	assert.deepEqual( cosItemUseTail( scroll, [], { records: [], reverseChoice: 2 } ), Uint8Array.of( 2 ) );
	assert.deepEqual( cosItemUseTail( scroll, [], { records: [], reverseChoice: 3 } ), Uint8Array.of( 3 ) );
	for ( const reverseChoice of [ undefined, 0, 7 ] ) {
		assert.throws( () => cosItemUseTail( scroll, [], { records: [], reverseChoice } ), /reverse return/ );
	}
	// Port-only (reverse-return-map.ts): choice 7 carries its map point id.
	assert.deepEqual(
		cosItemUseTail( scroll, [], { records: [], reverseChoice: 7, reverseMapPoint: 258 } ),
		Uint8Array.of( 7, 2, 1, 0, 0 )
	);
});
