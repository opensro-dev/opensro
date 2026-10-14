/*
===========================================================================

starved-world-entry-live.test.mjs - world entry completes with no animation frames

A window the compositor stops drawing (an occluded or off-screen window the
browser does not report as hidden) gets no animation frames while its
document stays visible. The native client keeps updating and finishes its
loading screen while inactive (CPSMission_OnUpdate 728E10; only drawing
stops, A11650), so the entry must still reach game-ready. Windows and Linux
browsers cannot occlude a visible window on demand, so animation frames are
withheld by the page itself from the moment Enter is pressed.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveProbeCharacter } from "../../../../scripts/lib/probeCharacter.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { enterPlayableSession, waitPlayableWorld } from "./helpers/playable-session.mjs";

/*
================
withholdAnimationFrames

Installed before the page runs: once __starveFrames is set, every new
requestAnimationFrame callback is kept and never called, as a compositor
that stopped drawing the window would.
================
*/
function withholdAnimationFrames() {
	const request = globalThis.requestAnimationFrame.bind( globalThis );
	globalThis.__starveFrames = false;
	globalThis.__withheldFrames = 0;
	globalThis.requestAnimationFrame = callback => {
		if ( !globalThis.__starveFrames ) return request( callback );
		globalThis.__withheldFrames++;
		return 0;
	};
}

/*
================
starved world entry
================
*/
test( "a world entry with animation frames stopped in a visible window reaches game-ready", {
	timeout: 180000
}, async () => {
	const { browser, page } = await launchProbeBrowser( { headed: true } );
	try {
		await page.addInitScript( withholdAnimationFrames );
		const character = await enterPlayableSession( page, resolveProbeCharacter() );
		await page.evaluate( () => {
			globalThis.__starveFrames = true;
		} );
		await waitPlayableWorld( page, character );
		const state = await page.evaluate( () => ({
			visibility: document.visibilityState,
			withheld: globalThis.__withheldFrames,
			report: document.querySelector( "output" )?.textContent ?? ""
		}) );
		assert.equal( state.visibility, "visible" );
		assert.ok( state.withheld > 0, "the page asked for animation frames and got none" );
		assert.match( state.report, /World admission: \S+; 0 pending groups/ );
	} finally {
		await browser.close();
	}
} );
