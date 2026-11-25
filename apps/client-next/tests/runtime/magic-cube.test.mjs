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
	MAGIC_CUBE_MOVE_PUSH,
	MAGIC_CUBE_MOVE_POP
} = await import( "../../src/engine/foundation/gameplay/magic-cube.ts" );
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
