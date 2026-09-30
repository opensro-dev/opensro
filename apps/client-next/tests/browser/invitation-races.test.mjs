/*
===========================================================================

invitation-races.test.mjs - real pointer replies against expired gameplay state.

An isolated document imports the production bridge and gameplay modules.
Retain the old button until a real click to reproduce presentation lag without
rewriting served source, network responses or a live player session.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "expired invitation clicks are safe through the browser input bridge", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		for ( const matching of [ false, true ] ) {
			const fixture = await page.evaluateHandle( async matching => {
				const bridgePath = "/src/engine/runtime/platform/ui/ui.ts";
				const gamePath = "/src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts";
				const { createUiBridge } = await import( bridgePath );
				const { createGameplay } = await import( gamePath );
				const frames = [], errors = [];
				const game = createGameplay( frame => frames.push( frame ) );
				game.bootstrap( { character: { name: "Owner" } } );
				game.seed( {
					gid: 1,
					refObjId: 1907,
					kind: "local-player",
					name: "Owner",
					regionId: 257,
					x: 0,
					y: 0,
					z: 0,
					heading: 0
				} );
				if ( matching ) {
					const payload = new Uint8Array( 26 ), view = new DataView( payload.buffer );
					view.setUint32( 0, 123, true );
					view.setUint32( 4, 42, true );
					payload[21] = 16;
					view.setUint32( 22, 77, true );
					game.receive( { opcode: 0x75bf, payload }, 0 );
				} else {
					game.receive( { opcode: 0x3393, payload: Uint8Array.of( 2, 7, 0, 0, 0, 0 ) }, 0 );
				}
				const bridge = createUiBridge( document.querySelector( "canvas" ), event => {
					if ( event.kind !== "activate" || event.id !== "invite-accept" ) return;
					try {
						game.command(
							matching ?
								{ kind: "party-match-answer", a: 123, b: 42, answer: 1 } :
								{ kind: "social-consent", accept: true },
							60000,
							undefined
						);
					} catch ( error ) {
						errors.push( String( error ) );
					}
					bridge.present( { title: "Expired proposal", message: "", controls: [] } );
				}, () => {} );
				bridge.present( {
					title: "Proposal",
					message: "",
					controls: [ { id: "invite-accept", label: "Yes", kind: "button", rect: [ 20, 20, 100, 40 ] } ]
				} );
				return { game, bridge, frames, errors };
			}, matching );
			await page.mouse.move( 50, 40 );
			await page.mouse.down();
			if ( !matching ) {
				await fixture.evaluate( f =>
					f.game.receive( { opcode: 0xb452, payload: Uint8Array.of( 2, 16 ) }, 31001 )
				);
			}
			await page.mouse.up();
			const result = await fixture.evaluate( f => ({
				frames: f.frames.map( frame => ({ opcode: frame.opcode, payload: [ ...frame.payload ] }) ),
				errors: f.errors
			}) );
			assert.deepEqual( result.errors, [] );
			assert.deepEqual(
				result.frames,
				matching ? [ { opcode: 0x30fa, payload: [ 123, 0, 0, 0, 42, 0, 0, 0, 2 ] } ] : []
			);
			assert.equal( await page.locator( '[data-ui-id="invite-accept"]' ).count(), 0 );
			await fixture.evaluate( f => {
				f.bridge.dispose();
				f.game.dispose();
			} );
		}
	} finally {
		await browser.close();
	}
} );
