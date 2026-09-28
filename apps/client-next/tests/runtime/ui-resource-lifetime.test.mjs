/*
===========================================================================

ui-resource-lifetime.test.mjs - hidden screens must release asset capacity

Runs the real asset owner and UI metadata owners against a controlled worker.
Completion order is explicit: no network speed, cache state or timer can hide
the transition from a visible screen to an inactive resource owner.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createAssets } = await import( "../../src/engine/runtime/assets/assets.ts" );
const { createHudResources } = await import( "../../src/engine/runtime/ui/hud/resources.ts" );
const { createGuideResources } = await import( "../../src/engine/runtime/ui/guide/resources.ts" );
const { createLocalization } = await import( "../../src/engine/runtime/ui/localization/localization.ts" );
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );

const ORIGIN = "https://fixture.invalid";
const EMPTY_LAYOUT = { controlsByName: {} };
/** @type {readonly (readonly [string, typeof createHudResources | typeof createGuideResources])[]} */
const METADATA_OWNERS = [ [ "HUD", createHudResources ], [ "guide", createGuideResources ] ];

/*
================
fixture

Keep the production admission, completion and cancellation bookkeeping.
Only the worker boundary is controlled by the test.
================
*/
function fixture( t ) {
	const original = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	const messages = [];
	let worker;
	/*
	================
	ControlledWorker
	================
	*/
	class ControlledWorker {
		onmessage = null;
		onerror = null;
		onmessageerror = null;
		/*
		================
		constructor
		================
		*/
		constructor() {
			worker = this;
		}
		/*
		================
		postMessage
		================
		*/
		postMessage( message ) {
			messages.push( message );
		}
		/*
		================
		terminate
		================
		*/
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: ControlledWorker } );
	const assets = createAssets();
	t.after( () => {
		assets.dispose();
		if ( original ) Object.defineProperty( globalThis, "Worker", original );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	return {
		assets,
		messages,
		/*
		================
		complete
		================
		*/
		/** @param {number} id @param {unknown} value */
		complete( id, value = EMPTY_LAYOUT ) {
			assert.ok( worker?.onmessage );
			worker.onmessage( {
				data: {
					kind: "bytes",
					id,
					buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer
				}
			} );
		}
	};
}

for ( const [name, create] of METADATA_OWNERS ) {
	for ( const finishBeforeLeaving of [ false, true ] ) {
		test(`${name} drains completed work after leaving, completion before departure: ${finishBeforeLeaving}`, t => {
			const { assets, messages, complete } = fixture( t );
			const owner = create( assets, ORIGIN );
			t.after( () => owner.dispose() );
			owner.step( false );
			assert.equal( messages.length, 0, "hidden owner must not prefetch" );
			owner.step( true );
			const admitted = messages.slice();
			assert.equal( admitted.length, 4 );
			assert.equal( assets.available(), 0 );
			if ( finishBeforeLeaving ) { for ( const request of admitted ) complete( request.id ); }
			else {
				owner.step( false );
				assert.equal( assets.available(), 0, "in-flight work still reserves capacity" );
				for ( const request of [ ...admitted ].reverse() ) complete( request.id );
			}
			assert.equal( assets.available(), 0, "completion retains ownership until collected" );
			assert.equal( owner.step( false ), true );
			assert.equal( assets.available(), 4, "inactive owner must release every completed handle" );
			assert.equal( messages.length, admitted.length, "draining must not admit hidden work" );
			assert.equal( owner.error(), null );
			const preview = assets.request( ORIGIN + "/assets/char/roster.json" );
			complete( preview, { models: [] } );
			assert.equal( assets.take( preview )?.kind, "bytes", "the next screen can make progress" );
			owner.step( true );
			assert.ok( messages.length > admitted.length + 1, "returning resumes remaining work" );
			const priorUrls = new Set( admitted.map( request => request.url ) );
			assert.ok( messages.slice( admitted.length + 1 ).every( request => !priorUrls.has( request.url ) ) );
		});
	}
}

test("localization collects a late catalogue while hidden and retains it on return", t => {
	const { assets, messages, complete } = fixture( t );
	const owner = createLocalization( assets, ORIGIN );
	t.after( () => owner.dispose() );
	owner.step( 0, false );
	assert.equal( messages.length, 0 );
	owner.step( 0, true );
	owner.step( 1, false );
	complete( messages[0].id, { entries: { SN_SKILL: "Strike" } } );
	assert.equal( owner.step( 2, false ), true );
	assert.equal( assets.available(), 4 );
	assert.equal( owner.text( "SN_SKILL", "fallback" ), "Strike" );
	owner.step( 3, true );
	assert.equal( messages.length, 1 );
});

test("localization collects invalid results while hidden but defers retries until demanded", t => {
	const { assets, messages, complete } = fixture( t );
	const owner = createLocalization( assets, ORIGIN );
	t.after( () => owner.dispose() );
	owner.step( 0, true );
	complete( messages[0].id, { entries: null } );
	owner.step( 1, false );
	assert.equal( assets.available(), 4 );
	owner.step( 60000, false );
	assert.equal( messages.length, 1 );
	owner.step( 60001, true );
	assert.equal( messages.length, 2 );
});

test("the UI frame loop drains HUD requests after leaving the dock", t => {
	const { assets, messages, complete } = fixture( t );
	const ui = createUi( assets, () => {}, () => {}, () => {}, ORIGIN, ORIGIN );
	t.after( () => ui.dispose() );
	/** @type {import('../../src/engine/contracts/ui.ts').UiView} */
	const view = {
		session: null,
		gameplay: null,
		entities: [],
		width: 1024,
		height: 768,
		worldReady: false,
		frontend: {
			phase: "dock",
			generation: 1,
			elapsed: 0,
			alpha: 1,
			logoAlpha: 0,
			error: null
		}
	};
	ui.step( view, 0 );
	const hudRequests = messages.filter( request => request.url?.includes( "/assets/cif/layouts/" ) );
	assert.ok( hudRequests.length > 0, "dock starts HUD prefetch" );
	for ( const request of hudRequests ) complete( request.id );
	assert.ok( view.frontend );
	ui.step( { ...view, frontend: { ...view.frontend, phase: "loading-create" } }, 1 );
	for ( const request of hudRequests ) {
		assert.equal( assets.take( request.id ), null, "frame loop already collected the hidden HUD result" );
	}
});

test("explicit UI timing observes comparison and publication without forcing redraws", t => {
	const { assets } = fixture( t );
	const published = [], spans = [], active = [];
	const ui = createUi( assets, () => {}, scene => published.push( scene ), () => {}, ORIGIN, ORIGIN );
	t.after( () => ui.dispose() );
	const probe = {
		/*
		================
		detailBegin
		================
		*/
		detailBegin( name ) {
			assert.equal( active.length, 0 );
			active.push( name );
		},
		/*
		================
		detailEnd
		================
		*/
		detailEnd( name ) {
			assert.equal( active.pop(), name );
			spans.push( name );
		}
	};
	/** @type {import('../../src/engine/contracts/ui.ts').UiView} */
	const view = {
		session: null,
		gameplay: null,
		entities: [],
		width: 1024,
		height: 768,
		worldReady: false,
		frontend: { phase: "dock", generation: 1, elapsed: 0, alpha: 1, logoAlpha: 0, error: null }
	};
	assert.ok( ui.step( view, 0, probe ) );
	assert.equal( ui.step( view, 1, probe ), null );
	assert.ok( ui.step( { ...view, width: 1280 }, 2, probe ) );
	assert.equal( published.length, 2 );
	assert.deepEqual( spans, [
		"ui-assembly",
		"ui-finalize",
		"ui-compare",
		"ui-publish",
		"ui-assembly",
		"ui-finalize",
		"ui-compare",
		"ui-assembly",
		"ui-finalize",
		"ui-compare",
		"ui-publish"
	] );
	assert.deepEqual( active, [] );
});
