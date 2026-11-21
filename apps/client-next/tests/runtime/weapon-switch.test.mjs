/*
===========================================================================

weapon-switch.test.mjs - the weapon switch scroll's client half (port-only)

The scroll is the shipped ref 3790 alone; it arms the item-target cursor,
a bag weapon opens a box that asks the race, then a kind of that race
(the weapon's own excepted), the pick goes out as
[target slot][kind] after the type word, and the server's 0xF5..0xF8
refusals read as the scroll's own lines only while its use is in flight.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const {
	isWeaponSwitchScrollItem,
	weaponKind,
	weaponSwitchKinds,
	weaponSwitchTail,
	weaponSwitchFailureText
} = await import( "../../src/engine/foundation/gameplay/weapon-switch.ts" );
const { createCosHud } = await import( "../../src/engine/runtime/ui/hud/cos-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { defined } = await import( "../helpers/defined.mjs" );

// Type words: the scroll 3/3/13/11, a Chinese sword 3/1/6/2, a European
// dagger 3/1/6/13, a shield 3/1/4/1 and the siege weapon 3/1/6/16.
const SCROLL_TYPE = 0x5eec;
const CH_SWORD_TYPE = 0x132c;
const EU_DAGGER_TYPE = 0x6b2c;
const SHIELD_TYPE = 0x0a2c;
const SIEGE_TYPE = 0x832c;

/*
================
bagItem
================
*/
function bagItem( slot, refObjId, typeFlags ) {
	return { slot, refObjId, typeFlags, quantity: 1, plus: 0, durability: 0, variance: "0", magic: [] };
}

test("the scroll is ref 3790 alone, never its shared type word", () => {
	assert.equal( isWeaponSwitchScrollItem( 3790 ), true );
	assert.equal( isWeaponSwitchScrollItem( 3789 ), false );
});

test("a weapon's kinds are a race's kinds but its own; a shield or siege weapon has none", () => {
	assert.equal( weaponKind( CH_SWORD_TYPE ), 2 );
	assert.deepEqual( weaponSwitchKinds( CH_SWORD_TYPE, 0 ), [ 3, 4, 5, 6 ] );
	assert.deepEqual( weaponSwitchKinds( CH_SWORD_TYPE, 1 ), [ 7, 8, 9, 10, 11, 12, 13, 14, 15 ] );
	assert.deepEqual( weaponSwitchKinds( EU_DAGGER_TYPE, 1 ), [ 7, 8, 9, 10, 11, 12, 14, 15 ] );
	assert.deepEqual( weaponSwitchKinds( EU_DAGGER_TYPE, 0 ), [ 2, 3, 4, 5, 6 ] );
	assert.deepEqual( weaponSwitchKinds( CH_SWORD_TYPE, 2 ), [] );
	assert.deepEqual( weaponSwitchKinds( SHIELD_TYPE, 0 ), [] );
	assert.deepEqual( weaponSwitchKinds( SIEGE_TYPE, 0 ), [] );
	assert.deepEqual( weaponSwitchKinds( SCROLL_TYPE, 1 ), [] );
});

test("the tail is the target slot then the kind", () => {
	assert.deepEqual( [ ...weaponSwitchTail( 21, 4 ) ], [ 21, 4 ] );
	assert.throws( () => weaponSwitchTail( undefined, 4 ) );
	assert.throws( () => weaponSwitchTail( 6, 4 ) );
	assert.throws( () => weaponSwitchTail( 21, 16 ) );
});

test("the cursor picks a bag weapon, then the box its race and kind once", () => {
	const hud = createCosHud(), scroll = bagItem( 25, 3790, SCROLL_TYPE );
	hud.armItemTarget( scroll );
	assert.equal( hud.itemTargetCursor(), 0xa6 );
	// A shield or an equipped weapon keeps the cursor armed.
	assert.equal( hud.chooseItemTarget( bagItem( 22, 9103, SHIELD_TYPE ) ), false );
	assert.equal( hud.chooseItemTarget( bagItem( 6, 9101, CH_SWORD_TYPE ) ), false );
	assert.equal( hud.itemTargetCursor(), 0xa6 );
	assert.equal( hud.chooseItemTarget( bagItem( 21, 9101, CH_SWORD_TYPE ) ), true );
	assert.equal( hud.targetUseBox(), "weapon-switch" );
	// The race step first: no kind is offered until a race is picked.
	assert.deepEqual( hud.weaponSwitchRaces(), [ 0, 1 ] );
	assert.deepEqual( hud.weaponSwitchKinds(), [] );
	assert.equal( hud.chooseWeaponKind( 4 ), null );
	assert.equal( hud.chooseWeaponRace( 2 ), false );
	assert.equal( hud.chooseWeaponRace( 0 ), true );
	assert.deepEqual( hud.weaponSwitchRaces(), [] );
	assert.deepEqual( hud.weaponSwitchKinds(), [ 3, 4, 5, 6 ] );
	// Back is the race step again, the weapon kept.
	assert.equal( hud.backWeaponRace(), true );
	assert.equal( hud.chooseWeaponRace( 1 ), true );
	assert.equal( hud.weaponSwitchKinds().length, 9 );
	// A kind the box does not offer picks nothing.
	assert.equal( hud.chooseWeaponKind( 4 ), null );
	assert.deepEqual( hud.chooseWeaponKind( 12 ), { kind: "item-use", slot: 25, targetSlot: 21, weaponKind: 12 } );
	assert.equal( hud.targetUse(), null );
	assert.deepEqual( hud.weaponSwitchRaces(), [] );
	assert.deepEqual( hud.weaponSwitchKinds(), [] );
	assert.equal( hud.chooseWeaponKind( 12 ), null );
});

test("the use carries its tail and only its own answer reads as its lines", () => {
	const scrollBody = Buffer.alloc( 6 );
	scrollBody.writeUInt32LE( 3790 );
	scrollBody.writeUInt16LE( 1, 4 );
	const sent = [];
	const g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( {
		refItemSnapshot: [ { refObjId: 3790, typeFlags: SCROLL_TYPE } ],
		equipItems: [ { slot: 25, refObjId: 3790, body: [ ...scrollBody ] } ]
	} );
	g.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 25000,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Author",
		countryByte9c: 0
	} );
	g.take();
	// No use in flight: the byte is the native mapper's, which is silent.
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF6 ) }, 0 );
	assert.deepEqual( defined( defined( g.take() ).notices ), [] );
	g.command( { kind: "item-use", slot: 25, targetSlot: 21, weaponKind: 4 }, 0, undefined );
	const use = defined( sent.find( f => f.opcode === 0x75bd ) );
	assert.deepEqual( [ ...use.payload ], [ 25, SCROLL_TYPE & 0xff, SCROLL_TYPE >>> 8, 21, 4 ] );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF6 ) }, 0 );
	const notice = defined( defined( defined( g.take() ).notices ).at( -1 ) );
	assert.equal( notice.text, weaponSwitchFailureText( 0xF6 ) );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF6 ) }, 0 );
	assert.deepEqual( defined( defined( g.take() ).notices ).map( n => n.sequence ), [ notice.sequence ] );
	g.dispose();
});

test("0xF6 reads as the blue options that cannot follow the weapon", () => {
	assert.equal( weaponSwitchFailureText( 0xF6 ), "This item's blue options cannot follow it to that kind." );
});
