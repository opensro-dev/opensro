/*
===========================================================================

magic-cube.test.mjs - Magic Cube TID gate, linked apply, and 0xB06D 0x27/0x28

Port-only (#3906). Pins the deposit / withdraw wire apply so a success
answer never hits the unappliable-resync path, and the linked-storage
helpers stay aligned with the server's GrantStack / DropQuantity shape.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	isMagicCube,
	magicCubeElixirFilter,
	magicCubeMatchesFilter,
	encodeMagicCubePush,
	encodeMagicCubePop,
	applyMagicCubePushLocal,
	applyMagicCubePopLocal,
	applyMagicCubeConsumeLocal,
	MAGIC_CUBE_COPY,
	MAGIC_CUBE_MOVE_PUSH,
	MAGIC_CUBE_MOVE_POP,
	MAGIC_CUBE_MOVE_CONSUME
} = await import( "../../src/engine/foundation/gameplay/magic-cube.ts" );
const { alchemySelection } = await import( "../../src/engine/foundation/ui/alchemy-selection.ts" );
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);

// TID 3/2/3/1: item class + TID1 3 + TID2 2 + TID3 3 + TID4 1.
const CUBE_TYPE = 0x9cc;
// TID 3/3/10/1 etc elixir stack body (u16 count).
const ELIXIR_TYPE = 0xd6c;

/*
================
cubeBody

CSOItem [u32 RefObjID][u32 StoredItemCount] for a Magic Cube.
================
*/
function cubeBody( refObjId, stored ) {
	const p = new Uint8Array( 8 ), v = new DataView( p.buffer );
	v.setUint32( 0, refObjId, true );
	v.setUint32( 4, stored, true );
	return [ ...p ];
}

/*
================
elixirBody

Etc stack body [u32 RefObjID][u16 count].
================
*/
function elixirBody( refObjId, quantity ) {
	const p = new Uint8Array( 6 ), v = new DataView( p.buffer );
	v.setUint32( 0, refObjId, true );
	v.setUint16( 4, quantity, true );
	return [ ...p ];
}

test("isMagicCube admits TID 3/2/3 only", () => {
	assert.equal( isMagicCube( CUBE_TYPE ), true );
	assert.equal( isMagicCube( ELIXIR_TYPE ), false );
	assert.equal( isMagicCube( 0x132c ), false );
});

test("magicCubeElixirFilter maps reinforce recipe codenames", () => {
	assert.equal( magicCubeElixirFilter( "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A" ), 1 );
	assert.equal( magicCubeElixirFilter( "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B" ), 2 );
	assert.equal( magicCubeElixirFilter( "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A" ), 4 );
	assert.equal( magicCubeElixirFilter( "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B" ), 8 );
	assert.equal( magicCubeElixirFilter( "ITEM_ETC_HP_POTION_01" ), 0 );
});

test("magicCubeMatchesFilter follows Param10", () => {
	const cube = {
		slot: 13,
		refObjId: 1,
		typeFlags: CUBE_TYPE,
		quantity: 0,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		nativeFields: { itemParam10_2c0: 1, itemParam9_2bc: 10 }
	};
	const weapon = {
		...cube,
		slot: 14,
		refObjId: 2,
		typeFlags: ELIXIR_TYPE,
		codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"
	};
	const armor = { ...weapon, codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A" };
	assert.equal( magicCubeMatchesFilter( cube, weapon ), true );
	assert.equal( magicCubeMatchesFilter( cube, armor ), false );
});

test("encodeMagicCubePush and Pop match the v1.696 body readers", () => {
	assert.deepEqual( [ ...encodeMagicCubePush( 14, 13, 2 ) ], [ 0x27, 14, 13, 2, 0 ] );
	assert.deepEqual( [ ...encodeMagicCubePop( 0x867, 13, 15, 1 ) ], [
		0x28,
		0x67,
		0x08,
		0,
		0,
		13,
		15,
		1,
		0
	] );
});

test("applyMagicCubePushLocal merges and tracks StoredItemCount", () => {
	const cube = {
		slot: 13,
		refObjId: 100,
		typeFlags: CUBE_TYPE,
		quantity: 0,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		nativeFields: { itemParam9_2bc: 10 }
	};
	const elixir = {
		slot: 14,
		refObjId: 200,
		typeFlags: ELIXIR_TYPE,
		quantity: 5,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"
	};
	const first = applyMagicCubePushLocal( cube, elixir, 3, 50 );
	assert.ok( first );
	assert.equal( first.elixirLeft?.quantity, 2 );
	assert.equal( first.cube.quantity, 3 );
	assert.equal( first.cube.linkedStorage?.rows.length, 1 );
	const second = applyMagicCubePushLocal( first.cube, { ...elixir, quantity: 2 }, 2, 50 );
	assert.ok( second );
	assert.equal( second.elixirLeft, null );
	assert.equal( second.cube.quantity, 5 );
	assert.equal( second.cube.linkedStorage?.rows[0]?.quantity, 5 );
});

test("applyMagicCubePopLocal withdraws by RefObjID", () => {
	const cube = {
		slot: 13,
		refObjId: 100,
		typeFlags: CUBE_TYPE,
		quantity: 4,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		linkedStorage: {
			capacity: 10,
			rows: [ {
				slot: 0,
				refObjId: 200,
				typeFlags: ELIXIR_TYPE,
				quantity: 4,
				plus: 0,
				durability: 0,
				variance: "0",
				magic: []
			} ]
		}
	};
	const applied = applyMagicCubePopLocal( cube, 200, 2, null, 15, 50 );
	assert.ok( applied );
	assert.equal( applied.destination.slot, 15 );
	assert.equal( applied.destination.quantity, 2 );
	assert.equal( applied.cube.quantity, 2 );
	assert.equal( applied.cube.linkedStorage?.rows[0]?.quantity, 2 );
});

test("inventory applies 0xB06D 0x27 and 0x28 without resync", () => {
	const sent = [];
	const owner = createInventory( frame => sent.push( frame ) );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [
			{
				refObjId: 100,
				typeFlags: CUBE_TYPE,
				name: "Magic Cube",
				codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON",
				nativeFields: { itemParam9_2bc: 10, itemParam10_2c0: 1, maxStack: 1 }
			},
			{
				refObjId: 200,
				typeFlags: ELIXIR_TYPE,
				name: "Weapon elixir",
				codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
				nativeFields: { maxStack: 50 }
			}
		],
		equipItems: [
			{
				slot: 13,
				refObjId: 100,
				body: cubeBody( 100, 0 ),
				linkedStorage: { capacity: 10, rows: [] }
			},
			{ slot: 14, refObjId: 200, body: elixirBody( 200, 5 ) }
		]
	} );
	const cube = owner.state().inventory.find( row => row.slot === 13 );
	assert.ok( cube );
	assert.equal( isMagicCube( cube.typeFlags ), true );
	assert.equal( cube.linkedStorage?.capacity, 10 );

	owner.magicCubePush( 14, 13, 3, 0 );
	assert.equal( sent.at( -1 )?.opcode, 0x706d );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ MAGIC_CUBE_MOVE_PUSH, 14, 13, 3, 0 ] );
	assert.equal(
		owner.receive( 0xb06d, Uint8Array.of( 1, MAGIC_CUBE_MOVE_PUSH, 14, 13, 3, 0 ) ),
		true
	);
	const afterPush = owner.state().inventory;
	assert.equal( afterPush.find( row => row.slot === 14 )?.quantity, 2 );
	assert.equal( afterPush.find( row => row.slot === 13 )?.quantity, 3 );
	assert.equal( afterPush.find( row => row.slot === 13 )?.linkedStorage?.rows[0]?.quantity, 3 );

	owner.magicCubePop( 13, 15, 200, 2, 1 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [
		MAGIC_CUBE_MOVE_POP,
		200 & 0xff,
		200 >> 8 & 0xff,
		0,
		0,
		13,
		15,
		2,
		0
	] );
	const popAnswer = new Uint8Array( 10 );
	popAnswer[0] = 1;
	popAnswer[1] = MAGIC_CUBE_MOVE_POP;
	new DataView( popAnswer.buffer ).setUint32( 2, 200, true );
	popAnswer[6] = 13;
	popAnswer[7] = 15;
	new DataView( popAnswer.buffer ).setUint16( 8, 2, true );
	assert.equal( owner.receive( 0xb06d, popAnswer ), true );
	const afterPop = owner.state().inventory;
	assert.equal( afterPop.find( row => row.slot === 15 )?.quantity, 2 );
	assert.equal( afterPop.find( row => row.slot === 13 )?.quantity, 1 );
});

test("applyMagicCubePushLocal allocates fresh slots for maxStack 1 and full stacks", () => {
	const cube = {
		slot: 13,
		refObjId: 100,
		typeFlags: CUBE_TYPE,
		quantity: 0,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		nativeFields: { itemParam9_2bc: 10 }
	};
	const elixir = {
		slot: 14,
		refObjId: 3679,
		typeFlags: ELIXIR_TYPE,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B"
	};

	// v1.150 elixirs have maxStack 1: repeated deposits of the same RefObjID
	// must land in consecutive linked-storage slots rather than failing merge.
	const one = applyMagicCubePushLocal( cube, elixir, 1, 1 );
	assert.ok( one );
	assert.equal( one.elixirLeft, null );
	assert.deepEqual(
		one.cube.linkedStorage?.rows.map( r => ({ slot: r.slot, qty: r.quantity }) ),
		[ { slot: 0, qty: 1 } ]
	);

	const two = applyMagicCubePushLocal( one.cube, elixir, 1, 1 );
	assert.ok( two );
	assert.equal( two.cube.quantity, 2 );
	assert.deepEqual(
		two.cube.linkedStorage?.rows.map( r => ({ slot: r.slot, qty: r.quantity }) ),
		[ { slot: 0, qty: 1 }, { slot: 1, qty: 1 } ]
	);

	// Full stack with maxStack > 1 also spills into the next free slot.
	const fullStackCube = {
		...cube,
		quantity: 50,
		linkedStorage: {
			capacity: 10,
			rows: [ { ...elixir, slot: 0, quantity: 50 } ]
		}
	};
	const spilled = applyMagicCubePushLocal( fullStackCube, { ...elixir, quantity: 5 }, 5, 50 );
	assert.ok( spilled );
	assert.equal( spilled.cube.quantity, 55 );
	assert.deepEqual(
		spilled.cube.linkedStorage?.rows.map( r => ({ slot: r.slot, qty: r.quantity }) ),
		[ { slot: 0, qty: 50 }, { slot: 1, qty: 5 } ]
	);
});

test("inventory applies 0xB06D 0x27 and 0x28 for maxStack 1 elixirs without resync", () => {
	const sent = [];
	const owner = createInventory( frame => sent.push( frame ) );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [
			{
				refObjId: 100,
				typeFlags: CUBE_TYPE,
				name: "Magic Cube",
				codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON",
				nativeFields: { itemParam9_2bc: 10, itemParam10_2c0: 1, maxStack: 1 }
			},
			{
				refObjId: 3679,
				typeFlags: ELIXIR_TYPE,
				name: "Elixir (Weapon)",
				codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
				nativeFields: { maxStack: 1 }
			}
		],
		equipItems: [
			{
				slot: 13,
				refObjId: 100,
				body: cubeBody( 100, 1 ),
				linkedStorage: {
					capacity: 10,
					rows: [ { slot: 0, refObjId: 3679, body: elixirBody( 3679, 1 ) } ]
				}
			},
			{ slot: 14, refObjId: 3679, body: elixirBody( 3679, 1 ) },
			{ slot: 15, refObjId: 3679, body: elixirBody( 3679, 1 ) }
		]
	} );

	owner.magicCubePush( 14, 13, 1, 0 );
	assert.equal(
		owner.receive( 0xb06d, Uint8Array.of( 1, MAGIC_CUBE_MOVE_PUSH, 14, 13, 1, 0 ) ),
		true
	);
	owner.magicCubePush( 15, 13, 1, 1 );
	assert.equal(
		owner.receive( 0xb06d, Uint8Array.of( 1, MAGIC_CUBE_MOVE_PUSH, 15, 13, 1, 0 ) ),
		true
	);

	const afterPush = owner.state().inventory;
	assert.equal( afterPush.find( row => row.slot === 14 ), undefined );
	assert.equal( afterPush.find( row => row.slot === 15 ), undefined );
	const pushedCube = afterPush.find( row => row.slot === 13 );
	assert.equal( pushedCube?.quantity, 3 );
	assert.deepEqual(
		pushedCube?.linkedStorage?.rows.map( row => ({ slot: row.slot, qty: row.quantity }) ),
		[ { slot: 0, qty: 1 }, { slot: 1, qty: 1 }, { slot: 2, qty: 1 } ]
	);

	owner.magicCubePop( 13, 14, 3679, 1, 2 );
	const popAnswer = new Uint8Array( 10 );
	popAnswer[0] = 1;
	popAnswer[1] = MAGIC_CUBE_MOVE_POP;
	new DataView( popAnswer.buffer ).setUint32( 2, 3679, true );
	popAnswer[6] = 13;
	popAnswer[7] = 14;
	new DataView( popAnswer.buffer ).setUint16( 8, 1, true );
	assert.equal( owner.receive( 0xb06d, popAnswer ), true );

	const afterPop = owner.state().inventory;
	assert.equal( afterPop.find( row => row.slot === 14 )?.quantity, 1 );
	const poppedCube = afterPop.find( row => row.slot === 13 );
	assert.equal( poppedCube?.quantity, 2 );
	assert.deepEqual(
		poppedCube?.linkedStorage?.rows.map( row => ({ slot: row.slot, qty: row.quantity }) ),
		[ { slot: 1, qty: 1 }, { slot: 2, qty: 1 } ]
	);
});

test("inventory applies first 0xB06D 0x27 push into a brand-new cube without linkedStorage", () => {
	const sent = [];
	const owner = createInventory( frame => sent.push( frame ) );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [
			{
				refObjId: 100,
				typeFlags: CUBE_TYPE,
				name: "Magic Cube",
				codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON",
				nativeFields: { itemParam9_2bc: 10, itemParam10_2c0: 1, maxStack: 1 }
			},
			{
				refObjId: 3679,
				typeFlags: ELIXIR_TYPE,
				name: "Elixir (Weapon)",
				codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
				nativeFields: { maxStack: 1 }
			}
		],
		equipItems: [
			// Brand-new cube from mall/GM: no linkedStorage yet on the raw row.
			{ slot: 13, refObjId: 100, body: cubeBody( 100, 0 ) },
			{ slot: 14, refObjId: 3679, body: elixirBody( 3679, 1 ) }
		]
	} );

	owner.magicCubePush( 14, 13, 1, 0 );
	assert.equal(
		owner.receive( 0xb06d, Uint8Array.of( 1, MAGIC_CUBE_MOVE_PUSH, 14, 13, 1, 0 ) ),
		true
	);

	const afterPush = owner.state().inventory;
	assert.equal( afterPush.find( row => row.slot === 14 ), undefined );
	const cube = afterPush.find( row => row.slot === 13 );
	assert.equal( cube?.quantity, 1 );
	assert.equal( cube?.linkedStorage?.capacity, 10 );
	assert.deepEqual(
		cube?.linkedStorage?.rows.map( row => ({ slot: row.slot, qty: row.quantity }) ),
		[ { slot: 0, qty: 1 } ]
	);
});

test("applyMagicCubeConsumeLocal and 0xB06D 0x29 deduct a stored elixir for Alchemy Reinforce", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [
			{
				refObjId: 100,
				typeFlags: CUBE_TYPE,
				name: "Magic Cube",
				codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
				nativeFields: { itemParam9_2bc: 10, itemParam10_2c0: 1, maxStack: 1 }
			},
			{
				refObjId: 3679,
				typeFlags: ELIXIR_TYPE,
				name: "Elixir (Weapon)",
				codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
				nativeFields: { maxStack: 1 }
			}
		],
		equipItems: [
			{
				slot: 13,
				refObjId: 100,
				body: cubeBody( 100, 2 ),
				linkedStorage: {
					capacity: 10,
					rows: [
						{ slot: 0, refObjId: 3679, body: elixirBody( 3679, 1 ) },
						{ slot: 1, refObjId: 3679, body: elixirBody( 3679, 1 ) }
					]
				}
			}
		]
	} );

	const consume = new Uint8Array( 9 );
	consume[0] = 1;
	consume[1] = MAGIC_CUBE_MOVE_CONSUME;
	consume[2] = 13;
	new DataView( consume.buffer ).setUint32( 3, 3679, true );
	new DataView( consume.buffer ).setUint16( 7, 1, true );
	assert.equal( owner.receive( 0xb06d, consume ), true );

	const afterFirst = owner.state().inventory.find( row => row.slot === 13 );
	assert.equal( afterFirst?.quantity, 1 );
	assert.deepEqual(
		afterFirst?.linkedStorage?.rows.map( row => ({ slot: row.slot, qty: row.quantity }) ),
		[ { slot: 1, qty: 1 } ]
	);

	// Direct helper check on the remaining elixir empties the cube without dropping it.
	const emptied = applyMagicCubeConsumeLocal( afterFirst, 3679, 1 );
	assert.ok( emptied );
	assert.equal( emptied.quantity, 0 );
	assert.deepEqual( emptied.linkedStorage?.rows, [] );
});

test("alchemySelection admits a non-empty Magic Cube in reinforce slot 1 and refuses empty", () => {
	const weapon = {
		slot: 13,
		refObjId: 1,
		typeFlags: 0x32c, // TID 3/1/6/0 weapon
		quantity: 1,
		plus: 0,
		durability: 100,
		variance: "0",
		magic: []
	};
	const filledCube = {
		slot: 14,
		refObjId: 100,
		typeFlags: CUBE_TYPE,
		quantity: 2,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: []
	};
	const emptyCube = { ...filledCube, quantity: 0 };

	const ok = alchemySelection( "reinforce", [ 13 ], filledCube, weapon );
	assert.deepEqual( ok, { mode: "reinforce", slots: [ 13, 14 ] } );

	const refused = alchemySelection( "reinforce", [ 13 ], emptyCube, weapon );
	assert.ok( refused?.refusal );
	assert.deepEqual( refused.slots, [ 13 ] );
});

test("MAGIC_CUBE_COPY covers all 16 Item Mall names, descriptions, and UI notices", () => {
	for ( const kind of [ "WEAPON", "ARMOR", "ACCESSARY", "SHIELD" ] ) {
		for ( const suffix of [ "", "_10", "_20", "_30" ] ) {
			const nameKey = `SN_ITEM_MALL_MAGIC_CUBE_${kind}${suffix}`;
			const descKey = `${nameKey}_TT_DESC`;
			assert.ok( MAGIC_CUBE_COPY[nameKey]?.startsWith( "Magic Cube (" ), `missing ${nameKey}` );
			assert.ok( (MAGIC_CUBE_COPY[descKey]?.length ?? 0) > 0, `missing ${descKey}` );
		}
	}
	assert.ok( MAGIC_CUBE_COPY.UIIT_MSG_MAGIC_CUBE_USE );
	assert.ok( MAGIC_CUBE_COPY.UIIT_STT_MAGIC_CUBE_INVENTORY_FULL );
	assert.ok( MAGIC_CUBE_COPY.UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR );
});
