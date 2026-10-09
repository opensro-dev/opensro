/*
===========================================================================

reverse-return-scroll.test.mjs - the reverse return scroll's choice box

A use of ITEM_MALL_REVERSE_RETURN_SCROLL (3/3/3/3) from the bag opens
message box 0x1E; CGInterface_OnMsgBoxResult (6971B0) runs the use with
the picked point as one byte after the type word. Driven through the
production HUD and the worker's gameplay owner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

// An etc item (3/3) of type 3, type 4 = 3.
const REVERSE_SCROLL = 3 << 2 | 3 << 5 | 3 << 7 | 3 << 11;

/*
================
open

A fixture holding a reverse return scroll in bag slot 13, inventory open.
================
*/
function open( sent ) {
	const f = uiFixture( command => sent.push( command ) );
	// The fixture's state types its inventory empty; this one holds a row.
	f.state.gameplay = /** @type {any} */ ({
		...f.state.gameplay,
		inventorySlotCount: 58,
		equipmentSlotCount: 13,
		inventory: [ {
			slot: 13,
			refObjId: 3795,
			typeFlags: REVERSE_SCROLL,
			quantity: 2,
			name: "Reverse return scroll",
			icon: "item/etc/hp_potion_01.ddj"
		} ]
	});
	for ( let time = 0; time < 1200; time += 100 ) f.ui.step( f.state, time );
	f.ui.event( { kind: "key", code: "KeyI" } );
	f.ui.step( f.state, 1200 );
	return f;
}

/*
================
boxIds
================
*/
function boxIds( result ) {
	return result.controls.map( c => c.id ).filter( id => id.startsWith( "reverse-scroll" ) );
}

test("a bag use asks for the point, and a row sends the use with it", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.equal( sent.length, 0, "the use waited for its point" );
		const shown = f.ui.step( f.state, 1300 );
		assert.deepEqual( boxIds( shown ), [ "reverse-scroll:2", "reverse-scroll:3", "reverse-scroll-cancel" ] );
		assert.ok( f.hasText( "Select the location used by the reverse return scroll." ) );
		f.ui.event( { kind: "activate", id: "reverse-scroll:3" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 3 }
		} );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1400 ) ), [] );
	} finally {
		f.dispose();
	}
});

test("cancel and Escape close the box without a use", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-cancel" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1400 ) ), [] );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1500 );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1600 ) ), [] );
		assert.equal( sent.length, 0 );
	} finally {
		f.dispose();
	}
});

test("the worker sends the point as the byte after the type word", () => {
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( {
		refItemSnapshot: [ { refObjId: 3795, typeFlags: REVERSE_SCROLL } ],
		equipItems: [ { refObjId: 3795, slot: 21, body: [ 0xd3, 0x0e, 0, 0, 2, 0 ] } ]
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
		name: "Author"
	} );
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 2 }, 1, undefined );
	assert.deepEqual( sent, [ {
		opcode: 0x75bd,
		payload: Uint8Array.of( 21, REVERSE_SCROLL & 255, REVERSE_SCROLL >>> 8, 2 )
	} ] );
	gameplay.dispose();
});
