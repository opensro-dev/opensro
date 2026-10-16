/*
===========================================================================
pet-info-layout.test.mjs - training pages must not mask the native pet stats page.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

test("trained attack pets retain HP labels and hide pickup-only info", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		cosRecords: [ { gid: 7, refObjId: 100, band: 3, hp: 100, mp: 0, status: 0, dead: false } ]
	} );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { attackPetTraining: true } ) } );
	f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "activate", id: "cos-tab:0" } );
	for ( let i = 1; i <= 4; i++ ) f.ui.step( f.state, i * 100 );
	assert.equal( f.ui.stats().error, null );
	assert.ok( f.hasText( "HP" ), "attack pet HP label must remain visible" );
	assert.ok( !f.hasText( "Rental period" ), "pickup pet rental label must stay hidden" );
	assert.ok( !f.hasText( "<Stat does not exist>" ), "attack pet stats must not show the no-ability placeholder" );
});
