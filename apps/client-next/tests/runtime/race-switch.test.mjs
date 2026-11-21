/*
===========================================================================

race-switch.test.mjs - the armor and accessory switch scrolls' client half

Port-only: the armor scroll (ref 3791) and the accessory scroll (ref 3793)
arm the item-target cursor; a bag item the scroll switches opens a Yes/No
confirmation, Yes sends [target slot] after the type word, and the
server's 0xF5/0xF6/0xF8 refusals read as the scroll's own lines only
while its use is in flight.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const {
	raceSwitchKindOf,
	raceSwitchAccepts,
	raceSwitchTail,
	raceSwitchFailureText
} = await import( "../../src/engine/foundation/gameplay/race-switch.ts" );
const { createCosHud } = await import( "../../src/engine/runtime/ui/hud/cos-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { defined } = await import( "../helpers/defined.mjs" );

// Type words: the scrolls 3/3/13/11, a Chinese light piece 3/1/2/3, a
// European protector 3/1/10/3, a European shield 3/1/4/2, a Chinese ring
// 3/1/5/3, a European earring 3/1/12/1 and a Chinese sword 3/1/6/2.
const SCROLL_TYPE = 0x5eec;
const CH_LIGHT_TYPE = 0x192c;
const EU_PROTECTOR_TYPE = 0x1d2c;
const EU_SHIELD_TYPE = 0x122c;
const CH_RING_TYPE = 0x1aac;
const EU_EARRING_TYPE = 0x0e2c;
const CH_SWORD_TYPE = 0x132c;

/*
================
bagItem
================
*/
function bagItem( slot, refObjId, typeFlags ) {
	return { slot, refObjId, typeFlags, quantity: 1, plus: 0, durability: 0, variance: "0", magic: [] };
}

test("the scrolls are refs 3791 and 3793 alone", () => {
	assert.equal( raceSwitchKindOf( 3791 ), "armor" );
	assert.equal( raceSwitchKindOf( 3793 ), "accessory" );
	assert.equal( raceSwitchKindOf( 3790 ), null );
	assert.equal( raceSwitchKindOf( 3792 ), null );
});

test("the armor scroll takes armor and shields, the accessory scroll accessories", () => {
	assert.equal( raceSwitchAccepts( "armor", CH_LIGHT_TYPE ), true );
	assert.equal( raceSwitchAccepts( "armor", EU_PROTECTOR_TYPE ), true );
	assert.equal( raceSwitchAccepts( "armor", EU_SHIELD_TYPE ), true );
	assert.equal( raceSwitchAccepts( "armor", CH_SWORD_TYPE ), false );
	assert.equal( raceSwitchAccepts( "armor", CH_RING_TYPE ), false );
	assert.equal( raceSwitchAccepts( "accessory", CH_RING_TYPE ), true );
	assert.equal( raceSwitchAccepts( "accessory", EU_EARRING_TYPE ), true );
	assert.equal( raceSwitchAccepts( "accessory", CH_LIGHT_TYPE ), false );
	assert.equal( raceSwitchAccepts( "accessory", SCROLL_TYPE ), false );
});

test("the tail is the target slot", () => {
	assert.deepEqual( [ ...raceSwitchTail( 20 ) ], [ 20 ] );
	assert.throws( () => raceSwitchTail( undefined ) );
	assert.throws( () => raceSwitchTail( 6 ) );
});

test("the cursor picks a bag item the scroll switches, then asks Yes or No", () => {
	const hud = createCosHud(), scroll = bagItem( 25, 3791, SCROLL_TYPE );
	hud.armItemTarget( scroll );
	// A weapon, a ring or an equipped piece keeps the cursor armed.
	assert.equal( hud.chooseItemTarget( bagItem( 23, 9205, CH_SWORD_TYPE ) ), false );
	assert.equal( hud.chooseItemTarget( bagItem( 26, 9207, CH_RING_TYPE ) ), false );
	assert.equal( hud.chooseItemTarget( bagItem( 1, 9201, CH_LIGHT_TYPE ) ), false );
	assert.equal( hud.itemTargetCursor(), 0xa6 );
	assert.equal( hud.chooseItemTarget( bagItem( 21, 9201, CH_LIGHT_TYPE ) ), true );
	assert.equal( hud.targetUseBox(), "race-switch" );
	assert.deepEqual( hud.takeTargetUse(), { kind: "item-use", slot: 25, targetSlot: 21 } );
});

test("Yes on the confirmation sends one use with the target", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const scroll = bagItem( 13, 3793, SCROLL_TYPE ), ring = bagItem( 14, 9207, CH_RING_TYPE );
		const state = {
			...f.state,
			gameplay: { ...f.state.gameplay, inventory: [ scroll, ring ], inventorySlotCount: 45 }
		};
		let scene, now = 100;
		const settle = () => {
			for ( const end = now + 500; now <= end; now += 100 ) scene = f.ui.step( state, now ) ?? scene;
			return scene;
		};
		f.ui.step( state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		settle();
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		settle();
		f.ui.event( { kind: "activate", id: "slot:14" } );
		assert.ok( settle().controls.some( row => row.id === "race-switch-confirm" ) );
		f.ui.event( { kind: "activate", id: "race-switch-confirm" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "item-use", slot: 13, targetSlot: 14 } } ] );
	} finally {
		f.dispose();
	}
});

test("the use carries its tail and only its own answer reads as its lines", () => {
	const scrollBody = Buffer.alloc( 6 );
	scrollBody.writeUInt32LE( 3791 );
	scrollBody.writeUInt16LE( 1, 4 );
	const sent = [];
	const g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( {
		refItemSnapshot: [ { refObjId: 3791, typeFlags: SCROLL_TYPE } ],
		equipItems: [ { slot: 25, refObjId: 3791, body: [ ...scrollBody ] } ]
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
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF8 ) }, 0 );
	assert.deepEqual( defined( defined( g.take() ).notices ), [] );
	g.command( { kind: "item-use", slot: 25, targetSlot: 21 }, 0, undefined );
	const use = defined( sent.find( f => f.opcode === 0x75bd ) );
	assert.deepEqual( [ ...use.payload ], [ 25, SCROLL_TYPE & 0xff, SCROLL_TYPE >>> 8, 21 ] );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF8 ) }, 0 );
	const notice = defined( defined( defined( g.take() ).notices ).at( -1 ) );
	assert.equal( notice.text, raceSwitchFailureText( "armor", 0xF8 ) );
	g.dispose();
});

test("each scroll's refusals read as its own lines", () => {
	assert.equal(
		raceSwitchFailureText( "armor", 0xF5 ),
		"The armor switch scroll works only on armor or a shield in your bag."
	);
	assert.equal(
		raceSwitchFailureText( "accessory", 0xF5 ),
		"The accessory switch scroll works only on an earring, necklace or ring in your bag."
	);
	assert.equal(
		raceSwitchFailureText( "armor", 0xF6 ),
		"This item's blue options cannot follow it to the other race."
	);
	assert.equal( raceSwitchFailureText( "accessory", 0xF8 ), "No item of the other race matches this one." );
	assert.equal( raceSwitchFailureText( "armor", 0x10 ), undefined );
});
