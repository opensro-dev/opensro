/*
===========================================================================

weapon-switch-ui.test.mjs - the weapon switch scroll through the production HUD

Port-only: right-click arms the item-target cursor, a bag weapon opens the
box's race step, a race row its kind step, and a kind row sends one use
with the weapon and the kind.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
weaponSwitchFixture

The scroll in bag slot 13 and a Chinese sword (3/1/6/2) in slot 14.
================
*/
function weaponSwitchFixture() {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	const base = { quantity: 1, plus: 0, durability: 0, variance: "0", magic: [] };
	const scroll = { ...base, slot: 13, refObjId: 3790, typeFlags: 0x5eec };
	const sword = { ...base, slot: 14, refObjId: 9101, typeFlags: 0x132c };
	const state = {
		...f.state,
		gameplay: { ...f.state.gameplay, inventory: [ scroll, sword ], inventorySlotCount: 45 }
	};
	f.ui.step( state, 0 );
	f.ui.event( { kind: "key", code: "KeyI" } );
	let scene, now = 100;
	/*
	================
	settle
	================
	*/
	function settle() {
		for ( const end = now + 500; now <= end; now += 100 ) scene = f.ui.step( state, now ) ?? scene;
		assert.ok( scene );
		return scene;
	}
	settle();
	return { ...f, state, sent, settle };
}

/*
================
controlIds
================
*/
function controlIds( scene ) {
	return scene.controls.map( row => row.id ).filter( id => id.startsWith( "weapon-switch" ) );
}

test("a picked race then kind sends the scroll's use with the weapon and the kind", () => {
	const f = weaponSwitchFixture();
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.equal( f.ui.cursor(), 0xa6 );
		f.settle();
		f.ui.event( { kind: "activate", id: "slot:14" } );
		assert.equal( f.sent.length, 0, "the weapon opens the box, it does not use" );
		assert.deepEqual( controlIds( f.settle() ), [
			"weapon-switch-race:0",
			"weapon-switch-race:1",
			"weapon-switch-cancel"
		] );
		f.ui.event( { kind: "activate", id: "weapon-switch-race:1" } );
		const kinds = controlIds( f.settle() );
		assert.equal( kinds.length, 10, "the nine European kinds and Back" );
		assert.ok( kinds.includes( "weapon-switch:13" ) );
		f.ui.event( { kind: "activate", id: "weapon-switch:13" } );
		assert.deepEqual( f.sent, [ {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, targetSlot: 14, weaponKind: 13 }
		} ] );
		assert.equal( f.ui.cursor(), null );
	} finally {
		f.dispose();
	}
});

test("Back returns to the race step and Cancel there sends nothing", () => {
	const f = weaponSwitchFixture();
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		f.settle();
		f.ui.event( { kind: "activate", id: "weapon-switch-race:0" } );
		assert.ok( controlIds( f.settle() ).includes( "weapon-switch:4" ) );
		f.ui.event( { kind: "activate", id: "weapon-switch-cancel" } );
		assert.ok( controlIds( f.settle() ).includes( "weapon-switch-race:1" ) );
		f.ui.event( { kind: "activate", id: "weapon-switch-cancel" } );
		assert.equal( f.sent.length, 0 );
		assert.ok( !f.settle().controls.some( row => row.id === "weapon-switch-cancel" ) );
	} finally {
		f.dispose();
	}
});
