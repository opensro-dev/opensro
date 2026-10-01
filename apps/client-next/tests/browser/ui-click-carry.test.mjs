/*
===========================================================================

ui-click-carry.test.mjs - lift an item with one click, place it with the next

The retail inventory lifts an item onto the cursor on a click and places it
where the next press lands. The bridge reports the carry as the source's
drag events and the placing press as its drag-end; that press is consumed
whole, so it neither presses a control nor reaches the world. Exercised
through real browser input over the production bridge.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "a click lifts an item and the next press places it", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const path = "/src/engine/runtime/platform/ui/ui.ts";
			const { createUiBridge } = await import( path );
			const events = [], canvasPresses = [];
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Fixture page has no canvas" );
			canvas.addEventListener( "mousedown", () => canvasPresses.push( "mousedown" ) );
			const bridge = createUiBridge( canvas, event => events.push( event ), () => {} );
			const slot = ( id, x ) => ({
				id,
				label: id,
				kind: "button",
				rect: [ x, 20, 32, 32 ],
				draggable: true,
				carry: true
			});
			const controls = [ slot( "slot:13", 20 ), slot( "slot:14", 80 ) ];
			bridge.present( { title: "Inventory", message: "", controls } );
			return { bridge, controls, events, canvasPresses };
		} );
		const events = () => fixture.evaluate( f => f.events.filter( e => e.kind !== "hover" ) );
		const reset = () => fixture.evaluate( f => (f.events.length = 0, f.canvasPresses.length = 0) );

		// Lift, follow, place on another slot.
		await page.mouse.click( 36, 36 );
		await page.mouse.move( 70, 40 );
		await page.mouse.move( 96, 36 );
		let seen = await events();
		assert.ok( seen.some( e => e.kind === "activate" && e.id === "slot:13" ), "the lifting click still activates" );
		assert.deepEqual( seen.filter( e => e.kind === "drag" ).at( 0 ), {
			kind: "drag",
			id: "slot:13",
			dx: 0,
			dy: 0
		} );
		assert.ok( seen.filter( e => e.kind === "drag" ).length >= 2, "pointer moves follow the carry" );
		await reset();
		await page.mouse.click( 96, 36 );
		seen = await events();
		assert.deepEqual( seen.filter( e => e.kind === "drag-end" ), [ {
			kind: "drag-end",
			id: "slot:13",
			x: 96,
			y: 36
		} ] );
		assert.equal(
			seen.filter( e => e.kind === "activate" || e.kind === "press" && e.id ).length,
			0,
			"the placing press is consumed"
		);

		// A placing press on the world never reaches the canvas.
		await reset();
		await page.mouse.click( 36, 36 );
		await page.mouse.click( 400, 300 );
		seen = await events();
		assert.deepEqual( seen.filter( e => e.kind === "drag-end" ), [ {
			kind: "drag-end",
			id: "slot:13",
			x: 400,
			y: 300
		} ] );
		assert.deepEqual(
			await fixture.evaluate( f => f.canvasPresses ),
			[],
			"the world never sees the placing press"
		);

		// A press back on the source puts the item down; its double-click still fires.
		await reset();
		// A double-click from rest: the first click lifts, the second puts back.
		await page.mouse.dblclick( 36, 36 );
		seen = await events();
		assert.equal( seen.filter( e => e.kind === "drag-end" ).length, 0 );
		assert.ok( seen.some( e => e.kind === "double-activate" && e.id === "slot:13" ) );
		await reset();
		await page.mouse.move( 200, 200 );
		assert.equal(
			(await events()).filter( e => e.kind === "drag" ).length,
			0,
			"the put-back click did not lift again"
		);

		// Crossing other controls and releasing an ordinary key keep the carry:
		// the icon used to vanish and reappear at its slot on each boundary.
		await reset();
		await page.mouse.click( 36, 36 );
		await page.mouse.move( 96, 36, { steps: 6 } );
		await page.mouse.move( 300, 200, { steps: 6 } );
		await page.keyboard.press( "KeyA" );
		await page.mouse.move( 320, 220 );
		seen = await events();
		assert.equal( seen.filter( e => e.kind === "drag-cancel" ).length, 0, "the carry survives hover and keys" );
		assert.ok( seen.filter( e => e.kind === "drag" ).length >= 10, "every move reports" );
		await page.mouse.click( 400, 300 );
		assert.equal( (await events()).filter( e => e.kind === "drag-end" ).length, 1 );

		// A click off the slot centre starts the icon on the cursor.
		await reset();
		await page.mouse.click( 26, 30 );
		assert.deepEqual( (await events()).filter( e => e.kind === "drag" ).at( 0 ), {
			kind: "drag",
			id: "slot:13",
			dx: -10,
			dy: -6
		} );
		await page.keyboard.press( "Escape" );

		// Right press and Escape cancel without placing.
		for ( const cancel of [ "right", "Escape" ] ) {
			await reset();
			await page.mouse.click( 36, 36 );
			if ( cancel === "right" ) await page.mouse.click( 300, 300, { button: "right" } );
			else await page.keyboard.press( "Escape" );
			await page.mouse.move( 250, 250 );
			seen = await events();
			assert.equal( seen.filter( e => e.kind === "drag-end" ).length, 0, cancel );
			assert.ok( seen.some( e => e.kind === "drag-cancel" && e.id === "slot:13" ), cancel + " clears the carry" );
		}

		// A control that stops carrying (its item gone) drops the carry.
		await reset();
		await page.mouse.click( 36, 36 );
		await fixture.evaluate( f =>
			f.bridge.present( {
				title: "Inventory",
				message: "",
				controls: f.controls.map( c => ({ ...c, carry: false, draggable: false }) )
			} )
		);
		await page.mouse.click( 96, 36 );
		seen = await events();
		assert.equal( seen.filter( e => e.kind === "drag-end" ).length, 0 );
		await fixture.evaluate( f => f.bridge.dispose() );
	} finally {
		await browser.close();
	}
} );
