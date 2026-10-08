/*
===========================================================================

inventory-page-navigation.test.mjs - inventory navigation respects bag capacity

Exercise the UI events as well as the rendered page. Rendering clamps an
invalid page, which alone can hide a navigation state beyond the last tab.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
inventory page navigation excludes equipment sockets
================
*/
test("inventory next stops at the last bag page before previous is pressed", () => {
	const fixture = uiFixture();
	try {
		fixture.state.gameplay = {
			...fixture.state.gameplay,
			inventorySlotCount: 77,
			equipmentSlotCount: 13
		};
		for ( let time = 0; time < 1200; time += 100 ) fixture.ui.step( fixture.state, time );
		fixture.ui.event( { kind: "key", code: "KeyI" } );
		fixture.ui.step( fixture.state, 1300 );
		fixture.ui.event( { kind: "activate", id: "inventory-next" } );
		fixture.ui.step( fixture.state, 1400 );
		fixture.ui.event( { kind: "activate", id: "inventory-next" } );
		fixture.ui.step( fixture.state, 1500 );
		fixture.ui.event( { kind: "activate", id: "inventory-prev" } );
		const scene = fixture.ui.step( fixture.state, 1600 );
		assert.ok( scene, "previous must render the first bag page" );
		const bag = scene.controls.filter( control =>
			control.id.startsWith( "slot:" ) && !control.disabled && Number( control.id.slice( 5 ) ) >= 13
		);
		assert.deepEqual(
			bag.map( control => Number( control.id.slice( 5 ) ) ),
			Array.from( { length: 32 }, ( _, index ) => 13 + index )
		);
	} finally {
		fixture.dispose();
	}
});
