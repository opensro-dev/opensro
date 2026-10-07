/*
===========================================================================

playable-session.mjs - authenticated scratch-character browser admission

The optional pre-login hook admits diagnostic transport faults before any
world socket exists. Ordinary probes use the same unmodified login path.

===========================================================================
*/
import assert from "node:assert/strict";
import { CLIENT_NEXT_BASE_URL } from "../../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials, resolveProbeDivisionId } from "../../../../../scripts/lib/probeSession.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";

/*
================
bindPlayableRuntime

The page's own entry module owns the runtime. On the dev server that is
/src/bootstrap.ts; a bench bundle (pnpm build:bench) serves it as its
hashed entry chunk and keeps the export. Importing the URL the page already
loaded returns that same module instance, never a second runtime. A release
bundle drops the export on purpose, so binding to one fails loudly.
================
*/
export async function bindPlayableRuntime( page ) {
	await page.evaluate( async () => {
		const scripts = [ ...document.querySelectorAll( 'script[type="module"][src]' ) ]
			.map( script => new URL( script.getAttribute( "src" ) ?? "", location.href ) );
		const entry = scripts.find( url => url.pathname === "/src/bootstrap.ts" ) ??
			scripts.find( url => /^\/assets\/index-[^/]+\.js$/.test( url.pathname ) );
		if ( !entry ) throw Error( "No client entry module on the page" );
		const runtime = (await import( entry.href )).runtime;
		if ( !runtime ) {
			throw Error( `${entry.pathname} exports no runtime: benchmark a bench bundle (pnpm build:bench)` );
		}
		globalThis.__playableRuntime = runtime;
	} );
}
/*
================
waitPlayableWorld
================
*/
export async function waitPlayableWorld( page, character ) {
	await page.waitForFunction(
		character => {
			const state = __playableRuntime.sessionState();
			if (
				state?.phase === "failed" || state?.phase === "disconnected" ||
				state?.phase === "character-select" && state.error
			) throw Error( state.error ?? "World admission failed" );
			return state?.phase === "world" && state.character === character &&
				/Frontend: world\n/.test( document.querySelector( "output" )?.textContent );
		},
		character,
		{ timeout: 60000 }
	);
}
/*
================
bootPlayableSession
================
*/
/** @param {((page: import('playwright-core').Page) => Promise<void>) | undefined} [beforeLogin] */
export async function bootPlayableSession( page, name, beforeLogin = undefined ) {
	const character = assertCharacterAllowed( name, { context: "client-next playable skill acceptance" } );
	await page.setViewportSize( { width: 1024, height: 768 } );
	await page.route( "**/character/list", async route => {
		const response = await route.fetch(), body = await response.json();
		assert.ok( body.characters.some( row => row.name === character ), "scratch character must exist" );
		await route.fulfill( {
			response,
			json: { ...body, characters: body.characters.filter( row => row.name === character ) }
		} );
	} );
	await page.goto( CLIENT_NEXT_BASE_URL );
	await bindPlayableRuntime( page );
	await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "signed-out" );
	if ( beforeLogin ) await beforeLogin( page );
	const { loginId, loginPassword } = resolveProbeCredentials(), serverId = resolveProbeDivisionId();
	await page.evaluate(
		( { id, password, serverId } ) =>
			__playableRuntime.session( { kind: "login", apiBase: location.origin + "/api", id, password, serverId } ),
		{ id: loginId, password: loginPassword, serverId }
	);
	await page.locator( '[data-ui-id="frontend:create"]' ).waitFor( { timeout: 30000 } );
	await page.mouse.click( 505, 430 );
	await page.locator( '[data-ui-id="enter"]' ).waitFor();
	await page.waitForFunction( () => document.querySelector( '[data-ui-id="enter"]' )?.disabled === false );
	await page.locator( '[data-ui-id="enter"]' ).click();
	await waitPlayableWorld( page, character );
	return character;
}
