/*
===========================================================================

ui-drag-gesture.test.mjs - a running drag owns every touch (GH #1242)

While a touch drag runs, a second finger must land on a touch-action:none
surface: on a plain control (touch-action auto) the browser can take the
pair over as a pinch-zoom and cancel the drag. The bridge raises a
full-screen gesture shield for exactly the drag's lifetime. A mouse drag
cannot pinch and is never shielded, so its chord presses keep reaching
the controls. Exercised through real browser input over the production
bridge.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"a second finger during a touch drag hits the gesture shield, never a plain control",
	{ timeout: 45000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( {
			executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE
		} );
		try {
			await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
			const fixture = await page.evaluateHandle( async () => {
				const path = "/src/engine/runtime/platform/ui/ui.ts";
				const { createUiBridge } = await import( path );
				const events = [];
				const canvas = document.querySelector( "canvas" );
				if ( !canvas ) throw Error( "Fixture page has no canvas" );
				const bridge = createUiBridge( canvas, event => events.push( event ), () => {} );
				const controls = [
					{ id: "hotbar:1", label: "Skill", kind: "button", rect: [ 20, 20, 32, 32 ], draggable: true },
					{ id: "plain", label: "Plain", kind: "button", rect: [ 200, 20, 37, 40 ] }
				];
				bridge.present( { title: "World", message: "", controls } );
				return { bridge, controls, events };
			} );
			const events = () => fixture.evaluate( f => f.events.filter( e => e.kind !== "hover" ) );
			const hit = ( x, y ) =>
				page.evaluate( ( [px, py] ) => {
					const el = document.elementFromPoint( px, py );
					if ( !(el instanceof HTMLElement) ) return null;
					return {
						tag: el.tagName,
						uiId: el.dataset.uiId ?? null,
						touchAction: getComputedStyle( el ).touchAction
					};
				}, [ x, y ] );
			const cdp = await page.context().newCDPSession( page );
			await cdp.send( "Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 } );
			const send = ( type, touchPoints ) => cdp.send( "Input.dispatchTouchEvent", { type, touchPoints } );

			// At rest the plain button is the hit target and stays gesture-eligible.
			assert.deepEqual( await hit( 218, 40 ), { tag: "BUTTON", uiId: "plain", touchAction: "auto" } );

			// One finger starts the skill drag; the shield owns the screen now.
			await send( "touchStart", [ { x: 36, y: 36, id: 1 } ] );
			await send( "touchMove", [ { x: 80, y: 36, id: 1 } ] );
			assert.ok( (await events()).some( e => e.kind === "drag" && e.id === "hotbar:1" ), "the drag runs" );
			const shielded = await hit( 218, 40 );
			assert.equal( shielded?.uiId, null, "the plain button is covered during the drag" );
			assert.equal( shielded?.touchAction, "none", "the second finger's surface refuses gestures" );

			// A second finger lands and moves: no drag-cancel, the drag still ends as a drop.
			await send( "touchStart", [ { x: 80, y: 36, id: 1 }, { x: 218, y: 40, id: 2 } ] );
			await send( "touchMove", [ { x: 120, y: 36, id: 1 }, { x: 218, y: 90, id: 2 } ] );
			await send( "touchEnd", [] );
			const seen = await events();
			assert.equal(
				seen.filter( e => e.kind === "drag-cancel" ).length,
				0,
				"the second finger cannot cancel the drag"
			);
			assert.deepEqual( seen.filter( e => e.kind === "drag-end" ), [ {
				kind: "drag-end",
				id: "hotbar:1",
				x: 120,
				y: 36
			} ] );

			// The shield lowers the moment the drag ends - no publication in
			// between - so the very next press (a carry placement, say) lands.
			assert.deepEqual( await hit( 218, 40 ), { tag: "BUTTON", uiId: "plain", touchAction: "auto" } );

			// A mouse drag cannot pinch, so it is never shielded: the plain
			// button stays the hit target and chords keep landing (the chord
			// itself is pinned by ui-right-activation).
			await fixture.evaluate( f => (f.events.length = 0) );
			await page.mouse.move( 36, 36 );
			await page.mouse.down();
			await page.mouse.move( 90, 36, { steps: 3 } );
			assert.deepEqual(
				await hit( 218, 40 ),
				{ tag: "BUTTON", uiId: "plain", touchAction: "auto" },
				"mouse drags are not shielded"
			);
			await page.mouse.up();
			assert.equal( (await events()).filter( e => e.kind === "drag-cancel" ).length, 0 );
			await fixture.evaluate( f => f.bridge.dispose() );
		} finally {
			await browser.close();
		}
	}
);
