/*
===========================================================================

transport-auto-potion.test.mjs - opt-in companion recovery through the real UI

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const COS_HP_POTION = 0x6c | 1 << 7 | 4 << 11;
/*
================
bagItem
================
*/
const bagItem = ( slot, typeFlags, quantity, refObjId ) => ({
	slot,
	typeFlags,
	quantity,
	refObjId,
	plus: 0,
	durability: 0,
	variance: "0",
	magic: []
});
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

/*
================
uiRun

Drives the real UI for a second with one transport (refObjId 3914, a
published reference) at hp and a transport potion in slot 30; returns the
gameplay commands it sent.
================
*/
function uiRun( t, enabled, hp, readiness = {} ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	Object.assign( f.state, readiness );
	Object.assign( f.state.gameplay, {
		cosRecords: [ { gid: 8, refObjId: 3914, band: 2, hp, mp: 0, status: 0, dead: false } ],
		inventory: [ ...f.state.gameplay.inventory, bagItem( 30, COS_HP_POTION, 2, 9 ) ],
		inventoryPending: false,
		quickSlots: [ { slot: 1, kind: 0x46, payload: 17 } ]
	} );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { transportAutoPotion: enabled } ) } );
	for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, i * 100 );
	assert.equal( f.ui.stats().error, null );
	return sent.filter( command => command.kind === "item-use" );
}

test("the UI heals a low transport only with the row on, and never a healthy one", t => {
	assert.deepEqual( uiRun( t, false, 1 ), [], "off is the native client" );
	const uses = uiRun( t, true, 1 );
	assert.ok( uses.length >= 1, "a transport at 1 HP is healed" );
	assert.deepEqual( uses[0], { kind: "item-use", slot: 30, companionGid: 8 } );
	assert.deepEqual( uiRun( t, true, 87_829 ), [], "a full transport is left alone" );
});

test("companion recovery waits for world assets and frontend admission", t => {
	assert.deepEqual( uiRun( t, true, 1, { worldReady: false } ), [] );
	assert.deepEqual( uiRun( t, true, 1, { frontend: { phase: "loading-world" } } ), [] );
});

test("a fully admitted restored session continues companion recovery", t => {
	assert.ok(
		uiRun( t, true, 1, { session: { phase: "world", revision: 1, character: "Player", restoringWorld: true } } )
			.length > 0
	);
});
