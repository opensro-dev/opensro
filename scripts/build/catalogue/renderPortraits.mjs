/*
===========================================================================

renderPortraits.mjs - the catalogue's monster portraits, from the viewer

Port-only, not native. Each monster's portrait is a still of its model drawn
by the game's own renderer: the client's viewer page
(viewer.html?monster=<refObjId>&still=1&size=N, docs/guides/VIEWER.md) resolves
window.__viewerStill with a transparent PNG data URL, or rejects with a
fixed reason and the stage it stopped at. One owned headless browser draws
them in one batch page (&batch=1): the first monster loads the page, each
next one replaces it through window.__viewerNext, so the renderer, its
pipelines and the shared assets load once for the whole catalogue. A page
that fails to render, answers nothing or breaks is reloaded for the next
monster. Background throttling is off (a hidden page draws no frame).
Uniques go first: they are the cards the site shows most. The viewer must already be served (the client's dev server, or a
preview of its build); this stage never starts one.

A still depends only on the viewer's code and the packs it draws from, so
a pass with a cacheDir keeps its stills under the key of both (portraitKey)
and the next pass with the same key copies them instead of drawing: the
release's catalogue step redrew every portrait on every freeze, minutes of
a release whose client and data had not changed (opensro #4337). A failed
draw is never kept, so it is tried again.

===========================================================================
*/
import { createHash } from "node:crypto";
import { copyFileSync, existsSync, mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { launchProbeBrowser } from "../../lib/probeBrowser.mjs";

// PORTRAIT_SIZE is the square still's edge in pixels.
export const PORTRAIT_SIZE = 384;

// STILL_TIMEOUT_MS bounds one monster's draw; the viewer itself gives up
// at 30 s with "timeout", so this only catches a page that never answers.
const STILL_TIMEOUT_MS = 45000;

// PORTRAIT_PAGES batch pages draw at once, each loading the viewer once.
export const PORTRAIT_PAGES = 4;

// RELOAD_REASONS end a batch page: the next monster loads a fresh one.
const RELOAD_REASONS = new Set( [ "page", "render", "no-answer", "no-viewer", "timeout" ] );

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

// KEY_INPUTS are what the served viewer draws a still from: the page
// (its hashed chunk names stand for the renderer's code) and the pack
// index (each pack's sha256 stands for the models and textures).
const KEY_INPUTS = [ "viewer.html", "assets/packs/manifest.json" ];

/*
================
fetchServed

The bytes the viewer serves at a path under its base; a failed answer
refuses, since a key over an error page would match the next error page.
================
*/
async function fetchServed( base, name ) {
	const response = await fetch( new URL( name, base ) );
	if ( !response.ok ) throw Error( `portraits: ${name} answered ${response.status}` );
	return Buffer.from( await response.arrayBuffer() );
}

/*
================
portraitKey

The cache key of a pass: the served viewer page, its pack index and the
still's size.
================
*/
export async function portraitKey( { viewerBase, size, fetchBytes = fetchServed } ) {
	const hash = createHash( "sha256" ).update( `size ${size}\0` );
	for ( const name of KEY_INPUTS ) {
		const bytes = await fetchBytes( viewerBase, name );
		hash.update( `${name} ${bytes.length}\0` ).update( bytes );
	}
	return hash.digest( "hex" );
}

/*
================
reusePortraits

Copies each monster's kept still under key into workDir. Returns the
reused ids with their paths and the monsters still to draw.
================
*/
export function reusePortraits( { cacheDir, key, monsters, workDir } ) {
	const kept = path.join( cacheDir, key ), drawn = new Map(), rest = [];
	for ( const monster of monsters ) {
		const source = path.join( kept, monster.id + ".png" );
		if ( !existsSync( source ) ) {
			rest.push( monster );
			continue;
		}
		const file = path.join( workDir, monster.id + ".png" );
		copyFileSync( source, file );
		drawn.set( monster.id, file );
	}
	return { drawn, rest };
}

/*
================
keepPortraits

Keeps the pass's stills under key and drops every other key's folder:
only the last pass's stills can match the next one.
================
*/
export function keepPortraits( { cacheDir, key, drawn } ) {
	const kept = path.join( cacheDir, key );
	mkdirSync( kept, { recursive: true } );
	for ( const name of readdirSync( cacheDir ) ) {
		if ( name !== key ) rmSync( path.join( cacheDir, name ), { recursive: true, force: true } );
	}
	for ( const [id, file] of drawn ) {
		const target = path.join( kept, id + ".png" );
		if ( file !== target ) copyFileSync( file, target );
	}
}

/*
================
renderPortraits

Draws each monster and writes <workDir>/<id>.png. Returns the drawn ids
with their PNG paths and the failures with the viewer's reason; one
failure never stops the pass. With a cacheDir, stills kept under this
pass's key are copied instead of drawn, and the pass's stills are kept.
================
*/
export async function renderPortraits( {
	viewerBase,
	monsters,
	workDir,
	cacheDir,
	size = PORTRAIT_SIZE,
	pages = PORTRAIT_PAGES,
	log = console.log,
	fetchBytes = fetchServed,
	launch = launchProbeBrowser
} ) {
	mkdirSync( workDir, { recursive: true } );
	const key = cacheDir ? await portraitKey( { viewerBase, size, fetchBytes } ) : "";
	const reused = cacheDir ?
		reusePortraits( { cacheDir, key, monsters, workDir } ) :
		{ drawn: new Map(), rest: monsters };
	const drawn = reused.drawn, failed = [];
	if ( cacheDir ) log( `portraits: ${drawn.size} reused, ${reused.rest.length} to draw` );
	if ( reused.rest.length === 0 ) {
		keepPortraits( { cacheDir, key, drawn } );
		return { drawn, failed };
	}
	const { browser, page } = await launch( { backgroundThrottling: false } );
	const queue = portraitOrder( reused.rest );
	let next = 0;
	/*
	================
	drawBatch

	One page drawing monsters from the shared queue until it is empty.
	================
	*/
	const drawBatch = async target => {
		// Whether the page holds a live batch that can draw the next monster.
		let live = false;
		while ( next < queue.length ) {
			const monster = queue[next++];
			let outcome;
			try {
				if ( !live ) {
					const url = new URL( "viewer.html", viewerBase );
					url.searchParams.set( "monster", String( monster.id ) );
					url.searchParams.set( "still", "1" );
					url.searchParams.set( "size", String( size ) );
					url.searchParams.set( "batch", "1" );
					await target.goto( url.href );
				}
				outcome = await target.evaluate( awaitStill, { id: live ? monster.id : 0, timeout: STILL_TIMEOUT_MS } );
			} catch ( error ) {
				outcome = { reason: "page", stage: String( error ).slice( 0, 120 ) };
			}
			// A broken page, a render failure or silence leaves no batch to reuse.
			live = !RELOAD_REASONS.has( outcome.reason );
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
	};
	try {
		const targets = [ page ];
		while ( targets.length < Math.max( 1, Math.min( pages, queue.length ) ) ) {
			targets.push( await browser.newPage() );
		}
		await Promise.all( targets.map( drawBatch ) );
	} finally {
		await browser.close();
	}
	if ( cacheDir ) keepPortraits( { cacheDir, key, drawn } );
	return { drawn, failed };
}

/*
================
awaitStill

Runs in the page: the still of the monster the page just loaded (id 0) or
of the next one the batch draws in its place. A PNG, or a reason and stage.
================
*/
async function awaitStill( { id, timeout } ) {
	const still = id ? Reflect.get( window, "__viewerNext" )?.( id ) : Reflect.get( window, "__viewerStill" );
	if ( !still ) return { reason: "no-viewer" };
	const late = new Promise( resolve => setTimeout( () => resolve( { reason: "no-answer" } ), timeout ) );
	try {
		const png = await Promise.race( [ still, late ] );
		return typeof png === "string" ? { png } : png;
	} catch ( error ) {
		return { reason: error.message, stage: error.cause?.stage };
	}
}
