/*
===========================================================================

server-event-hud.test.mjs - the Server Mission gauge widget (port-only)

The later client's CNIFServerEvent at its nifserverevent.2dt rects,
anchored to the right edge; its gauge fill and "count/goal" text; shown
only with the Experimental row on, hidden by its close button until the
mission goes away; drawn by the production UI only when the v1.696 art
was published.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { test } from "node:test";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { serverEventLayout, gaugeRatio, gaugeText, createServerEventHud, SERVER_EVENT_ART } = await import(
	"../../src/engine/runtime/ui/hud/server-event-hud.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

const GAUGE = { id: 30001, count: 59, goal: 118 };

test("the widget sits at the native rects, 244 px from the right edge", () => {
	assert.deepEqual( serverEventLayout( 1024 ), {
		frame: [ 780, 56, 120, 24 ],
		gauge: [ 803, 59, 80, 4 ],
		text: [ 802, 67, 80, 13 ],
		snow: [ 779, 56, 24, 24 ],
		close: [ 885, 61, 16, 16 ]
	} );
	assert.deepEqual( serverEventLayout( 1280 ).frame, [ 1036, 56, 120, 24 ] );
});

test("the gauge fills count over goal, never past either end", () => {
	assert.equal( gaugeRatio( 59, 118 ), .5 );
	assert.equal( gaugeRatio( 5, 0 ), 0 );
	assert.equal( gaugeRatio( 150, 118 ), 1 );
	assert.equal( gaugeRatio( 0, 118 ), 0 );
	assert.equal( gaugeText( 7, 118 ), "7/118" );
});

test("the row decides; close hides it until the mission goes away", () => {
	const hud = createServerEventHud();
	hud.reconcile( GAUGE, false );
	assert.equal( hud.shown(), null );
	hud.reconcile( GAUGE, true );
	assert.deepEqual( hud.shown(), GAUGE );
	hud.close();
	assert.equal( hud.shown(), null );
	hud.reconcile( { ...GAUGE, count: 60 }, true );
	assert.equal( hud.shown(), null, "a new count keeps it closed" );
	hud.reconcile( undefined, true );
	hud.reconcile( GAUGE, true );
	assert.deepEqual( hud.shown(), GAUGE, "the next mission shows again" );
});

test("the option is off unless stored on", () => {
	assert.equal( experimentalOptions( {} ).serverEventGauge, false );
	assert.equal( experimentalOptions( { serverEventGauge: true } ).serverEventGauge, true );
});

test("the production UI draws the gauge with the row on and closes it", t => {
	const published = existsSync( CLIENT_PUBLIC_ROOT + SERVER_EVENT_ART + "wind.png" );
	for ( const enabled of [ false, true ] ) {
		const f = uiFixture();
		t.after( () => f.dispose() );
		f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { serverEventGauge: enabled } ) } );
		let now = 0, view;
		const settle = steps => {
			for ( let i = 0; i < steps; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
		};
		settle( 12 );
		Object.assign( f.state.gameplay, { serverEventGauge: GAUGE } );
		settle( 4 );
		const closable = () => defined( view ).controls.some( control => control.id === "server-event-close" );
		// Without the published v1.696 art the widget draws nothing.
		assert.equal( closable(), enabled && published );
		if ( !enabled || !published ) continue;
		assert.ok( f.hasText( "59/118" ) );
		f.ui.event( { kind: "activate", id: "server-event-close" } );
		settle( 2 );
		assert.equal( closable(), false );
	}
});
