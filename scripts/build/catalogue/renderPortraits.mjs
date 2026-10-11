/*
===========================================================================

renderPortraits.mjs - the catalogue's monster portraits, from the viewer

Port-only, not native. Each monster's portrait is a still of its model drawn
by the game's own renderer: the client's viewer page
(viewer.html?monster=<refObjId>&still=1&size=N, docs/VIEWER.md) resolves
window.__viewerStill with a transparent PNG data URL, or rejects with a
fixed reason and the stage it stopped at. One owned headless browser loads
one page per monster, in turn, with background throttling off (a hidden
page draws no frame). Uniques go first: they are the cards the site shows
most. The viewer must already be served (the client's dev server, or a
preview of its build); this stage never starts one.

===========================================================================
*/
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { launchProbeBrowser } from "../../lib/probeBrowser.mjs";

// PORTRAIT_SIZE is the square still's edge in pixels.
export const PORTRAIT_SIZE = 384;

// STILL_TIMEOUT_MS bounds one monster's draw; the viewer itself gives up
// at 30 s with "timeout", so this only catches a page that never answers.
const STILL_TIMEOUT_MS = 45000;

// PNG_DATA_URL is the only answer accepted from the page.
const PNG_DATA_URL = /^data:image\/png;base64,([A-Za-z0-9+/=]+)$/;

/*
================
portraitOrder

The monsters to draw, uniques first, then by id.
================
*/
export function portraitOrder( monsters ) {
	return [ ...monsters ].sort( ( a, b ) =>
		(a.grade === "unique" ? 0 : 1) - (b.grade === "unique" ? 0 : 1) || a.id - b.id
	);
}

/*
================
renderPortraits

Draws each monster and writes <workDir>/<id>.png. Returns the drawn ids
with their PNG paths and the failures with the viewer's reason; one
failure never stops the pass.
================
*/
export async function renderPortraits( { viewerBase, monsters, workDir, size = PORTRAIT_SIZE, log = console.log } ) {
	mkdirSync( workDir, { recursive: true } );
	const drawn = new Map(), failed = [];
	const { browser, page } = await launchProbeBrowser( { backgroundThrottling: false } );
	try {
		for ( const monster of portraitOrder( monsters ) ) {
			const url = new URL( "viewer.html", viewerBase );
			url.searchParams.set( "monster", String( monster.id ) );
			url.searchParams.set( "still", "1" );
			url.searchParams.set( "size", String( size ) );
			let outcome;
			try {
				await page.goto( url.href );
				outcome = await page.evaluate( async timeout => {
					const still = /** @type {Promise<string>} */ (Reflect.get( window, "__viewerStill" ));
					if ( !still ) return { reason: "no-viewer" };
					const late = new Promise( resolve =>
						setTimeout( () => resolve( { reason: "no-answer" } ), timeout )
					);
					try {
						const png = await Promise.race( [ still, late ] );
						return typeof png === "string" ? { png } : png;
					} catch ( error ) {
						const failure = /** @type {Error & { cause?: { stage?: string } }} */ (error);
						return { reason: failure.message, stage: failure.cause?.stage };
					}
				}, STILL_TIMEOUT_MS );
			} catch ( error ) {
				outcome = { reason: "page", stage: String( error ).slice( 0, 120 ) };
			}
			const match = PNG_DATA_URL.exec( outcome.png ?? "" );
			if ( !match ) {
				failed.push( { id: monster.id, reason: outcome.reason ?? "not-png", stage: outcome.stage } );
				continue;
			}
			const file = path.join( workDir, monster.id + ".png" );
			writeFileSync( file, Buffer.from( match[1], "base64" ) );
			drawn.set( monster.id, file );
			if ( drawn.size % 25 === 0 ) log( `portraits: ${drawn.size} drawn` );
		}
	} finally {
		await browser.close();
	}
	return { drawn, failed };
}
