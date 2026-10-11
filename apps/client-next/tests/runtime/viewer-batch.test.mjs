/*
===========================================================================

viewer-batch.test.mjs - a batch still page draws its next monster

The catalogue's portraits run in one page (batch=1): after a monster's
still, or its failure, the viewer keeps its owners (frame() answers "idle",
never "done") and scope.window.__viewerNext puts the next monster in its place
under a new gid. The character owner keeps per-gid state, so reusing the
old gid drew the previous model.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createViewer } = await import( "../../src/engine/runtime/viewer/viewer.ts" );
// The page globals the viewer installs (__viewerStill, __viewerNext), untyped
// in this test: the fakes stand in for browser objects.
const scope = /** @type {any} */ (globalThis);

/*
================
fakeHost

A viewer host whose character owner knows only the given monsters and
records every lifecycle event. Nothing ever draws, so a known monster
stays loading.
================
*/
function fakeHost( search, known ) {
	const lifecycle = /** @type {any[]} */ ([]);
	let wakes = 0;
	const host = {
		canvas: { width: 0, height: 0 },
		picture: { clientWidth: 0, clientHeight: 0, addEventListener() {} },
		search,
		wake: () => wakes++,
		renderer: { setViewerLight() {}, phase: () => "ready", setUi() {}, frame() {} },
		characters: {
			receiveLifecycle: events => lifecycle.push( ...events ),
			step() {},
			ready: () => false,
			knows: entity => known.includes( entity.refObjId ),
			itemType: () => undefined,
			dressed: () => false,
			linkedRide: () => undefined,
			error: () => null
		}
	};
	return { host: /** @type {any} */ (host), lifecycle, wakes: () => wakes };
}

/*
================
withBrowserGlobals

window and navigator.gpu as the viewer page has them, restored after.
================
*/
async function withBrowserGlobals( run ) {
	const window = Object.getOwnPropertyDescriptor( globalThis, "window" ),
		navigator = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	const page = /** @type {any} */ ({ opener: null });
	page.parent = page;
	Object.defineProperty( globalThis, "window", { value: page, configurable: true, writable: true } );
	Object.defineProperty( globalThis, "navigator", { value: { gpu: {} }, configurable: true } );
	try {
		await run();
	} finally {
		if ( window ) Object.defineProperty( globalThis, "window", window );
		else delete scope.window;
		if ( navigator ) Object.defineProperty( globalThis, "navigator", navigator );
		else delete scope.navigator;
	}
}

test("a batch page keeps its owners after a failed monster and draws the next under a new gid", () =>
	withBrowserGlobals( async () => {
		const { host, lifecycle, wakes } = fakeHost( "?monster=99&still=1&batch=1", [ 1933 ] );
		const viewer = createViewer( host );
		assert.deepEqual( lifecycle.map( e => [ e.kind, e.gid ?? e.entity.gid ] ), [ [ "spawn", 1 ] ] );
		const first = scope.window.__viewerStill;
		// 99 is unknown: the still fails, but the batch keeps its owners.
		assert.equal( viewer.frame( 0 ), "idle" );
		await assert.rejects( first, /unknown-monster/ );
		assert.equal( viewer.frame( 16 ), "idle" );
		// The next monster replaces it under the following gid and wakes the loop.
		const before = wakes();
		const next = scope.window.__viewerNext( 1933 );
		assert.equal( scope.window.__viewerStill, next );
		assert.deepEqual( lifecycle.slice( 1 ).map( e => [ e.kind, e.gid ?? e.entity.gid, e.entity?.refObjId ] ), [
			[ "despawn", 1, undefined ],
			[ "spawn", 2, 1933 ]
		] );
		assert.equal( wakes(), before + 1 );
		// Still loading: the page draws on.
		assert.equal( viewer.frame( 32 ), "draw" );
		// A second request before this one settles is refused, not interleaved.
		await assert.rejects( scope.window.__viewerNext( 1933 ), /bad-request/ );
	} ));

test("a single still still disposes its owners when it ends", () =>
	withBrowserGlobals( async () => {
		const { host } = fakeHost( "?monster=99&still=1", [] );
		const viewer = createViewer( host );
		assert.equal( viewer.frame( 0 ), "done" );
		await assert.rejects( scope.window.__viewerStill, /unknown-monster/ );
		assert.equal( scope.window.__viewerNext, undefined );
	} ));

/*
================
framingHost

A viewer host whose known monsters load at once and frame, and whose
character owner reports a glow (8EA960) on the gids in glowing, never yet
drawn. Counts the still readbacks, which never answer.
================
*/
function framingHost( search, known, glowing ) {
	let reads = 0;
	const host = {
		canvas: { width: 100, height: 100 },
		picture: { clientWidth: 100, clientHeight: 100, addEventListener() {} },
		search,
		wake: () => {},
		renderer: {
			setViewerLight() {},
			phase: () => "ready",
			setUi() {},
			frame() {},
			characterBounds: () => [ -1, 0, -1, 1, 2, 1 ],
			readDoll: () => {
				reads++;
				return new Promise( () => {} );
			}
		},
		characters: {
			receiveLifecycle() {},
			step() {},
			ready: () => true,
			knows: entity => known.includes( entity.refObjId ),
			itemType: () => undefined,
			dressed: () => true,
			glows: gid => glowing.includes( gid ),
			glowing: () => false,
			linkedRide: () => undefined,
			error: () => null
		}
	};
	return { host: /** @type {any} */ (host), reads: () => reads };
}

test("a batch page's next monster waits for its own glow", () =>
	withBrowserGlobals( async () => {
		// The first monster (gid 1) is unknown; the next (gid 2) glows.
		const { host, reads } = framingHost( "?monster=99&still=1&batch=1", [ 1933 ], [ 2 ] );
		const viewer = createViewer( host );
		assert.equal( viewer.frame( 0 ), "idle" );
		await assert.rejects( scope.window.__viewerStill, /unknown-monster/ );
		scope.window.__viewerNext( 1933 ).catch( () => {} );
		for ( let t = 16; t <= 1000; t += 16 ) assert.equal( viewer.frame( t ), "draw" );
		assert.equal( reads(), 0, "no still is read before the glow draws" );
	} ));
