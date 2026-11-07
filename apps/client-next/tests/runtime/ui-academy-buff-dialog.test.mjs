/*
===========================================================================

ui-academy-buff-dialog.test.mjs - Academy pass confirmation hit targets

Exercises the production HUD so replacing the modal arrays must preserve
the Yes/No controls and their pointer blocks through the frame primitives.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { topmostControlAt } = await import( "../../src/engine/foundation/ui/hit-test.ts" );

const VIEWPORTS = [ [ 1600, 900 ], [ 390, 844 ] ];
const SETTLE_FRAMES = 25, FRAME_MS = 10;
const SAVED_BUFF = "academy-buff-saved";
const CONFIRM = "academy-buff-pass-confirm", CANCEL = "academy-buff-pass-cancel";

for ( const [width, height] of VIEWPORTS ) {
	/*
	================
	academyPassDialog
	================
	*/
	test(`Academy pass dialog publishes clickable Yes/No controls at ${width}x${height}`, () => {
		/** @type {unknown[]} */
		const sent = [];
		const fixture = uiFixture( command => sent.push( command ) );
		fixture.state.width = width;
		fixture.state.height = height;
		Object.assign( fixture.state.gameplay, {
			academy: {
				member: true,
				members: [],
				savedBuff: { minutes: 1300, value: 25, receivedAtMs: 0 }
			}
		} );
		let nowMs = 0;
		/*
		================
		settle
		================
		*/
		function settle() {
			let semantics = null;
			for ( let frame = 0; frame < SETTLE_FRAMES; frame++ ) {
				semantics = fixture.ui.step( fixture.state, nowMs ) ?? semantics;
				nowMs += FRAME_MS;
			}
			return defined( semantics, "HUD publishes the changed dialog state" );
		}
		try {
			const initial = settle();
			assert.equal( defined( initial.controls.find( control => control.id === SAVED_BUFF ) ).kind, "button" );
			fixture.ui.event( { kind: "activate", id: SAVED_BUFF } );
			const dialog = settle();
			assert.deepEqual( dialog.controls.map( control => control.id ), [ CONFIRM, CANCEL ] );
			assert.equal( fixture.ui.blocks( 0, 0 ), true, "the modal blocks the world" );
			assert.equal( fixture.ui.blocks( width - 1, height - 1 ), true );
			for ( const control of dialog.controls ) {
				const [x, y, w, h] = control.rect;
				assert.equal( topmostControlAt( dialog.controls, x + w / 2, y + h / 2 )?.id, control.id );
				assert.ok( !control.disabled );
				assert.equal( fixture.ui.blocks( x + w / 2, y + h / 2 ), true );
			}
			assert.deepEqual( sent, [], "opening the question does not pass the buff" );
			fixture.ui.event( { kind: "activate", id: CANCEL } );
			assert.ok( settle().controls.some( control => control.id === SAVED_BUFF ) );
			assert.deepEqual( sent, [], "No cancels without sending a command" );
			fixture.ui.event( { kind: "activate", id: SAVED_BUFF } );
			assert.ok( settle().controls.some( control => control.id === CONFIRM ) );
			fixture.ui.event( { kind: "activate", id: CONFIRM } );
			assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "academy-pass-buff" } } ] );
			assert.ok( !settle().controls.some( control => control.id === CONFIRM || control.id === CANCEL ) );
		} finally {
			fixture.dispose();
		}
	});
}
