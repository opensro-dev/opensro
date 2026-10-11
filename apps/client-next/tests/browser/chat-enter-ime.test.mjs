/*
===========================================================================

chat-enter-ime.test.mjs - Enter submits a text box under any input method

Firefox on Linux with IBus or Fcitx marks plain typing as composing, so the
Enter keydown arrives with isComposing set and used to be dropped: the
player could type but never send. Its keyup now submits, unless a
compositionend in between shows the Enter only committed IME text.

Drives the platform layer alone (platform/ui/ui.ts): one presented text box,
no game state or assets, and counts the submit events it emits.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

/*
================
presentTextBox

Boot the platform with an event recorder and present one focused text box.
The page keeps the recorder on window.imeFixture.
================
*/
async function presentTextBox( page ) {
	await page.goto( CLIENT_NEXT_BASE_URL );
	await page.evaluate( async () => {
		const entry = Array.from( document.scripts ).find( s =>
			s.src && new URL( s.src ).pathname === "/src/bootstrap.ts"
		);
		if ( !entry ) throw Error( "bootstrap script not found" );
		const { runtime } = await import( entry.src );
		runtime.dispose();
		const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
		const events = [];
		const platform = createPlatform(
			document.querySelector( "canvas" ),
			document.querySelector( "output" ),
			() => {},
			() => {},
			() => {},
			event => events.push( event ),
			() => false
		);
		platform.presentUi( {
			title: "",
			message: "",
			controls: [ {
				id: "chat-text",
				label: "Chat message",
				rect: [ 10, 10, 240, 24 ],
				kind: "text",
				value: "hello"
			} ],
			focusRequest: { id: "chat-text", revision: 1, caret: 5 }
		} );
		/** @type {any} */ (window).imeFixture = {
			submits: () => events.filter( e => e.kind === "activate" && e.id === "submit" ).length,
			reset() {
				events.length = 0;
			}
		};
	} );
}

/*
================
submits
================
*/
function submits( page ) {
	return page.evaluate( () => /** @type {any} */ (window).imeFixture.submits() );
}

test( "Enter submits a text box when the keydown reports a composition", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await presentTextBox( page );
		const input = page.getByLabel( "Chat message", { exact: true } );

		// Linux Firefox with IBus: no composition events, the keydown says
		// isComposing, the keyup does not.
		await input.evaluate( el => {
			el.dispatchEvent(
				new KeyboardEvent( "keydown", { bubbles: true, key: "Enter", code: "Enter", isComposing: true } )
			);
			el.dispatchEvent( new KeyboardEvent( "keyup", { bubbles: true, key: "Enter", code: "Enter" } ) );
		} );
		assert.equal( await submits( page ), 1, "the keyup submitted" );

		// A CJK input method: Enter commits the composition, which must not submit.
		await page.evaluate( () => /** @type {any} */ (window).imeFixture.reset() );
		await input.evaluate( el => {
			el.dispatchEvent( new CompositionEvent( "compositionstart", { bubbles: true } ) );
			el.dispatchEvent(
				new KeyboardEvent( "keydown", { bubbles: true, key: "Enter", code: "Enter", isComposing: true } )
			);
			el.dispatchEvent( new CompositionEvent( "compositionend", { bubbles: true, data: "hello" } ) );
			el.dispatchEvent( new KeyboardEvent( "keyup", { bubbles: true, key: "Enter", code: "Enter" } ) );
		} );
		assert.equal( await submits( page ), 0, "committing IME text did not submit" );

		// A compositionstart whose end was lost: the next committed edit proves
		// no composition is open, so a plain Enter submits again.
		await input.evaluate( el => {
			el.dispatchEvent( new CompositionEvent( "compositionstart", { bubbles: true } ) );
			el.dispatchEvent( new InputEvent( "input", { bubbles: true, isComposing: false } ) );
		} );
		await input.press( "Enter" );
		assert.equal( await submits( page ), 1, "a stuck composition no longer blocks Enter" );
	} finally {
		await browser.close();
	}
} );
