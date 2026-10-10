/*
===========================================================================

title-resource-lifetime.test.mjs - title completions survive world entry

Controls the worker boundary while using the real four-slot asset owner.
The UI integration collects hidden title work without another title render.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createAssets } = await import( "../../src/engine/runtime/assets/assets.ts" );
const { createTitleResources } = await import( "../../src/engine/runtime/ui/title/resources.ts" );
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const ORIGIN = "https://fixture.invalid";
const CREATION_PATHS = [
	"/assets/cif/layouts/pscharactercreate_europe.json",
	"/assets/cif/layouts/pscharactercreatechina.json"
];
const FONT = {
	image: "/assets/fonts/fixture.png",
	atlasWidth: 1,
	atlasHeight: 1,
	fonts: {
		"0": {
			recordHeight: 1,
			ascent: 1,
			descent: 0,
			glyphs: {
				"63": { x: 0, y: 0, width: 1, height: 1, originX: 0, originY: 0, advanceX: 1 }
			}
		}
	}
};
/*
================
fixture
================
*/
function fixture( t ) {
	const messages = [];
	let worker;
	/*
	================
	ControlledWorker
	================
	*/
	class ControlledWorker {
		onmessage = null;
		/* ================ constructor ================ */
		constructor() {
			worker = this;
		}
		/* ================ postMessage ================ */
		postMessage( message ) {
			messages.push( message );
		}
		/* ================ terminate ================ */
		terminate() {}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
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
		/* ================ complete ================ */
		complete( request, failed = false ) {
			const path = new URL( request.url ).pathname;
			const value = path.includes( "font-atlas" ) ? FONT : { controlsByName: {}, sections: [], entries: {} };
			worker.onmessage( {
				data: failed ? { kind: "error", id: request.id, error: "fixture failure" } : request.decode === "png" ?
					{
						kind: "image",
						id: request.id,
						image: { width: 1, height: 1, close() {} }
					} :
					{
						kind: "bytes",
						id: request.id,
						buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer
					}
			} );
		}
	};
}

for ( const failed of [ false, true ] ) {
	test(`hidden title drains late creation layouts, failed=${failed}`, t => {
		const { assets, messages, complete } = fixture( t );
		const owner = createTitleResources( assets, ORIGIN );
		t.after( () => owner.dispose() );
		assert.equal( owner.step( false ), false );
		assert.equal( messages.length, 0 );
		owner.step();
		for ( const request of messages.slice() ) complete( request );
		owner.step();
		assert.ok( owner.data(), "title becomes usable before creation layouts settle" );
		const creation = messages.slice( 4 );
		assert.deepEqual( creation.map( request => new URL( request.url ).pathname ), CREATION_PATHS );
		assert.equal( assets.available(), 2 );
		assert.equal( owner.step( false ), false );
		for ( const request of creation ) complete( request, failed );
		assert.equal( owner.step( false ), true );
		assert.equal( assets.available(), 4 );
		assert.equal( messages.length, 6, "hidden collection does not admit new work" );
		assert.equal( !!owner.error(), failed );
		if ( !failed ) {
			const data = owner.data();
			assert.ok( data );
			assert.ok( data.creation.every( Boolean ) );
			owner.step();
			assert.equal( messages.length, 6, "returning reuses collected layouts" );
		}
	});
}

test("hidden title does not admit remaining layouts when the first four finish", t => {
	const { assets, messages, complete } = fixture( t );
	const owner = createTitleResources( assets, ORIGIN );
	t.after( () => owner.dispose() );
	owner.step();
	for ( const request of messages.slice() ) complete( request );
	owner.step( false );
	assert.equal( assets.available(), 4 );
	assert.equal( messages.length, 4 );
	assert.ok( owner.data() );
	owner.step();
	assert.equal( messages.length, 6 );
});

test("world UI frames collect title layouts without rendering the title", t => {
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
		frontend: { phase: "intro", generation: 1, elapsed: 0, alpha: 1, logoAlpha: 0, error: null }
	};
	const completed = new Set();
	let creation = [];
	for ( let frame = 0; frame < 20 && creation.length < 2; frame++ ) {
		ui.step( view, frame );
		for ( const request of messages ) {
			if ( request.kind !== "load" || completed.has( request.id ) ) continue;
			if ( CREATION_PATHS.includes( new URL( request.url ).pathname ) ) continue;
			complete( request );
			completed.add( request.id );
		}
		creation = messages.filter( request =>
			request.kind === "load" && CREATION_PATHS.includes( new URL( request.url ).pathname )
		);
	}
	assert.equal( creation.length, 2, "title admitted both optional layouts" );
	assert.ok( view.frontend );
	const world = { ...view, frontend: { ...view.frontend, phase: /** @type {const} */ ("world") } };
	ui.step( world, 100 );
	for ( const request of creation ) complete( request );
	ui.step( world, 101 );
	for ( const request of creation ) {
		assert.equal( assets.take( request.id ), null, "world frame already collected the hidden title result" );
	}
	assert.equal(
		messages.filter( request =>
			request.kind === "load" && CREATION_PATHS.includes( new URL( request.url ).pathname )
		).length,
		2
	);
});
