/*
===========================================================================

chat-link-focus.test.mjs - Alt+click on an item keeps the chat editor's focus

Port-only, not native (SRO_CHAT_ITEM_LINKS). A press elsewhere blurs the
focused editor, so the UI would no longer know a chat line was being
typed when the click arrives. An editor that keeps its focus on Alt
presses (keepFocusOnAltPress) stays focused through an Alt+click on an
item and still receives the activation; a plain click still blurs it.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "an Alt+click on an item keeps the chat editor focused", { timeout: 60000 }, async t => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 800, height: 600 } } );
	t.after( () => browser.close() );
	await page.route(
		new URL( CLIENT_NEXT_BASE_URL ).href,
		route =>
			route.fulfill( {
				contentType: "text/html",
				body: '<canvas style="width:800px;height:600px"></canvas>'
			} )
	);
	await page.goto( CLIENT_NEXT_BASE_URL, { timeout: 20000 } );
	await page.evaluate( async () => {
		const page = /** @type {any} */ (window);
		const { createUiBridge } = await import( "/src/engine/runtime/platform/ui/ui.ts" );
		page.events = [];
		page.bridge = createUiBridge(
			document.querySelector( "canvas" ),
			event => page.events.push( event ),
			() => {}
		);
		page.bridge.present( {
			title: "test",
			message: "",
			controls: [
				{
					id: "chat-text",
					kind: "text",
					label: "Chat",
					rect: [ 20, 500, 300, 20 ],
					value: "",
					keepFocusOnAltPress: true
				},
				{ id: "slot:13", kind: "button", label: "Potion", rect: [ 400, 200, 32, 32 ] }
			]
		} );
	} );
	const editor = page.locator( '[data-ui-id="chat-text"]' ), item = page.locator( '[data-ui-id="slot:13"]' );
	await editor.click();
	await item.click( { modifiers: [ "Alt" ] } );
	assert.equal(
		await page.evaluate( () => /** @type {HTMLElement | null} */ (document.activeElement)?.dataset?.uiId ),
		"chat-text"
	);
	assert.ok(
		await page.evaluate( () =>
			/** @type {any} */ (window).events.some( e => e.kind === "activate" && e.id === "slot:13" && e.alt )
		),
		"the Alt+click still activates the item"
	);
	await item.click();
	assert.notEqual(
		await page.evaluate( () => /** @type {HTMLElement | null} */ (document.activeElement)?.dataset?.uiId ),
		"chat-text"
	);
} );
