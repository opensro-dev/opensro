/*
===========================================================================

login-autofill.test.mjs - password-manager eligibility without DOM paint

Exercises the shipped credential bridge and page styles in Chromium. The
fixture isolates DOM paint from the canvas so focus, selection and autofill
cannot silently add a second visible editor over native login art.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "credential overlays accept autofill without painting over native controls", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		await page.evaluate( async () => {
			// Use the application's current styles, including its accessibility fallback.
			const html = new DOMParser().parseFromString( await (await fetch( "/" )).text(), "text/html" );
			for ( const node of html.querySelectorAll( "style,link[rel='stylesheet']" ) ) {
				document.head.append( node.cloneNode( true ) );
			}
			await document.fonts.ready;
		} );
		await page.waitForLoadState( "networkidle" );
		const clip = { x: 80, y: 80, width: 280, height: 130 };
		const background = await page.screenshot( { clip } );
		await page.evaluate( async () => {
			const modulePath = "/src/engine/runtime/platform/ui/ui.ts";
			const { createUiBridge } = await import( modulePath );
			const events = [];
			const canvas = /** @type {HTMLCanvasElement} */ (document.querySelector( "canvas" ));
			const bridge = createUiBridge( canvas, event => {
				events.push( event );
				document.body.dataset.events = JSON.stringify( events );
			}, () => {} );
			bridge.present( {
				title: "Login",
				message: "",
				controls: [
					{ id: "account", label: "Account", kind: "text", value: "Example", rect: [ 100, 100, 240, 30 ] },
					{
						id: "password",
						label: "Password",
						kind: "password",
						value: "example-secret",
						rect: [ 100, 160, 240, 30 ]
					},
					{ id: "chat", label: "Chat", kind: "text", value: "", rect: [ 100, 240, 240, 30 ] }
				]
			} );
		} );
		const account = page.locator( '[data-ui-id="account"]' );
		const password = page.locator( '[data-ui-id="password"]' );
		assert.equal( await account.getAttribute( "autocomplete" ), "username" );
		assert.equal( await password.getAttribute( "autocomplete" ), "current-password" );
		assert.equal( await password.getAttribute( "type" ), "password" );
		for ( const edit of [ account, password ] ) {
			assert.equal(
				await edit.evaluate( element => {
					const box = element.getBoundingClientRect();
					for (
						let parent = /** @type {Element | null} */ (element);
						parent;
						parent = parent.parentElement
					) {
						const style = getComputedStyle( parent );
						if (
							Number( style.opacity ) < 0.1 || style.visibility === "hidden" || style.display === "none"
						) return false;
					}
					return box.width >= 10 && box.height >= 10 &&
						document.elementFromPoint( box.x + box.width / 2, box.y + box.height / 2 ) === element;
				} ),
				true,
				"credential must pass password-manager visibility and hit testing"
			);
			await edit.focus();
			await edit.evaluate( element => /** @type {HTMLInputElement} */ (element).select() );
			assert.deepEqual(
				await page.screenshot( { clip } ),
				background,
				"focus and selection must not paint a second editor"
			);
		}
		const chat = page.locator( '[data-ui-id="chat"]' );
		assert.equal( await chat.getAttribute( "autocomplete" ), "off" );
		for ( const name of [ "data-bwignore", "data-1p-ignore", "data-lpignore" ] ) {
			assert.equal( await chat.getAttribute( name ), "true" );
		}
		assert.equal( await chat.evaluate( element => getComputedStyle( element ).opacity ), "0" );

		const cdp = await page.context().newCDPSession( page );
		await cdp.send( "DOM.enable" );
		await cdp.send( "CSS.enable" );
		const { root } = await cdp.send( "DOM.getDocument" );
		for ( const id of [ "account", "password" ] ) {
			const { nodeId } = await cdp.send( "DOM.querySelector", {
				nodeId: root.nodeId,
				selector: `[data-ui-id="${id}"]`
			} );
			await cdp.send( "CSS.forcePseudoState", { nodeId, forcedPseudoClasses: [ "autofill" ] } );
		}
		assert.equal( await account.evaluate( element => element.matches( ":autofill" ) ), true );
		assert.deepEqual(
			await page.screenshot( { clip } ),
			background,
			"Chromium autofill must not tint native login art"
		);
		await password.evaluate( element => {
			const edit = /** @type {HTMLInputElement} */ (element);
			edit.value = "filled-secret";
			edit.dispatchEvent( new Event( "input", { bubbles: true } ) );
			edit.dispatchEvent( new Event( "change", { bubbles: true } ) );
		} );
		const events = await page.evaluate( () => JSON.parse( document.body.dataset.events ?? "[]" ) );
		assert.ok(
			events.some( event => event.kind === "edit" && event.id === "password" && event.value === "filled-secret" )
		);

		await page.emulateMedia( { forcedColors: "active" } );
		for ( const edit of [ account, password, chat ] ) {
			const style = await edit.evaluate( element => {
				const s = getComputedStyle( element );
				return {
					opacity: s.opacity,
					color: s.color,
					fill: s.webkitTextFillColor,
					outline: s.outlineStyle,
					delay: s.transitionDelay
				};
			} );
			assert.equal( style.opacity, "1" );
			assert.notEqual( style.color, "rgba(0, 0, 0, 0)" );
			assert.equal( style.fill, style.color );
			assert.equal( style.outline, "solid" );
			assert.equal( style.delay, "0s" );
		}
	} finally {
		await browser.close();
	}
} );
