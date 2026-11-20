/*
===========================================================================

server-event-hud-ui.test.mjs - the production UI draws the Server Mission gauge

Asset-backed (the production UI fixture reads the published client tree):
the widget is drawn whenever the server sends a mission (no Experimental
row: it is the v1.696 client's own window), only when the v1.696 art was
published, and its close button hides it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { test } from "node:test";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { SERVER_EVENT_ART } = await import( "../../src/engine/runtime/ui/hud/server-event-hud.ts" );

const GAUGE = { id: 30001, count: 59, goal: 118 };

test("the production UI draws the gauge a mission sends and closes it", t => {
	const published = existsSync( CLIENT_PUBLIC_ROOT + SERVER_EVENT_ART + "wind.png" );
	const f = uiFixture();
	t.after( () => f.dispose() );
	let now = 0, view;
	const settle = steps => {
		for ( let i = 0; i < steps; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	};
	settle( 12 );
	Object.assign( f.state.gameplay, { serverEventGauge: GAUGE } );
	settle( 4 );
	const closable = () => defined( view ).controls.some( control => control.id === "server-event-close" );
	// Without the published v1.696 art the widget draws nothing.
	assert.equal( closable(), published );
	if ( !published ) return;
	assert.ok( f.hasText( "59/118" ) );
	f.ui.event( { kind: "activate", id: "server-event-close" } );
	settle( 2 );
	assert.equal( closable(), false );
});
