/*
===========================================================================

character-load-slots.test.mjs - character sources load side by side

resources.ts used to hold one load slot and admit one completed source a
frame, so a crowd's wearers dressed a part a fetch and were drawn bare
meanwhile. Up to CHARACTER_LOAD_SLOTS sources now load at once, any load
past the first leaves one of the asset owner's shared request slots free,
and every source that lands is admitted in the same poll.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterResources } = await import( "../../src/engine/runtime/characters/resources/resources.ts" );

const MODEL = { nodes: [], primitives: [], images: [], clips: [] };

/*
================
fakeWorld

Four request slots shared with `others` loads of other owners; a fetch
finishes when the test says so.
================
*/
function fakeWorld( others = 0 ) {
	let nextId = 1;
	const inFlight = new Map(), done = new Set(), started = [], cancelled = [], admitted = [];
	const assets = {
		available: () => 4 - others - inFlight.size,
		progress: () => null,
		health: () => ({ phase: "running" }),
		request( url ) {
			const id = nextId++, path = new URL( url ).pathname;
			inFlight.set( id, path );
			started.push( path );
			return id;
		},
		take( id ) {
			const path = inFlight.get( id );
			if ( !path || !done.has( path ) ) return null;
			inFlight.delete( id );
			return path.endsWith( ".json" ) ?
				{ kind: "bytes", id, buffer: new TextEncoder().encode( path ).buffer } :
				{ kind: "character", id, model: MODEL, images: [] };
		},
		cancel( id ) {
			cancelled.push( inFlight.get( id ) );
			inFlight.delete( id );
		},
		textureFeatures() {},
		install() {},
		dispose() {}
	};
	const renderer = {
		setCharacterModel( path ) {
			admitted.push( path );
		},
		setCharacterAnimation() {
			return 0;
		},
		retainCharacterModels() {}
	};
	const resources = createCharacterResources(
		/** @type {any} */ (assets),
		/** @type {any} */ (renderer),
		"http://probe/"
	);
	return { resources, assets, inFlight, done, started, cancelled, admitted };
}

const parts = [ "body", "hair", "head", "chest", "legs", "hands", "feet", "weapon" ].map( name =>
	`/assets/char/${name}.glb`
);

test("a wearer's cold parts load three at a time and land in one poll", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	for ( const path of parts ) assert.equal( resources.ready( path ), false );
	assert.deepStrictEqual( world.started, parts.slice( 0, 3 ) );
	assert.equal( world.assets.available(), 1, "one request slot stays free for the world" );
	for ( const path of parts.slice( 0, 3 ) ) world.done.add( path );
	resources.begin( 2 );
	assert.equal( resources.poll(), null );
	assert.deepStrictEqual( world.admitted, parts.slice( 0, 3 ), "all that landed is admitted at once" );
	for ( const path of parts.slice( 0, 3 ) ) assert.equal( resources.ready( path ), true );
	for ( const path of parts.slice( 3 ) ) resources.ready( path );
	assert.deepStrictEqual( world.started, parts.slice( 0, 6 ) );
	assert.deepStrictEqual( world.cancelled, [] );
});

test("other owners' loads leave the characters fewer slots", () => {
	// The world holds two of the four slots: one character load may take the
	// third, and the fourth stays free.
	const world = fakeWorld( 2 ), resources = world.resources;
	resources.begin( 1 );
	for ( const path of parts ) resources.ready( path );
	assert.deepStrictEqual( world.started, parts.slice( 0, 1 ) );
	assert.equal( world.assets.available(), 1 );
	// With every slot taken elsewhere nothing starts.
	const full = fakeWorld( 4 );
	full.resources.begin( 1 );
	full.resources.ready( parts[0] );
	assert.deepStrictEqual( full.started, [] );
	// With one slot left, a single character load still takes it, as before.
	const last = fakeWorld( 3 );
	last.resources.begin( 1 );
	for ( const path of parts ) last.resources.ready( path );
	assert.deepStrictEqual( last.started, parts.slice( 0, 1 ) );
});

test("a load no longer wanted is cancelled; the others keep their slots", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	for ( const path of parts.slice( 0, 3 ) ) resources.ready( path );
	resources.retainWanted( [] );
	resources.begin( 2 );
	resources.ready( parts[0] );
	resources.ready( parts[2] );
	resources.retainWanted( [] );
	assert.deepStrictEqual( world.cancelled, [ parts[1] ] );
	assert.equal( world.inFlight.size, 2 );
	resources.reset();
	assert.equal( world.inFlight.size, 0 );
});

test("byte sources that land together are each delivered, in completion order", () => {
	const world = fakeWorld(), resources = world.resources;
	const manifests = [ "/assets/a.json", "/assets/b.json" ];
	resources.begin( 1 );
	for ( const path of manifests ) resources.manifest( path );
	assert.deepStrictEqual( world.started, manifests );
	for ( const path of manifests ) world.done.add( path );
	resources.begin( 2 );
	const first = resources.poll(), second = resources.poll();
	assert.equal( first?.path, manifests[0] );
	assert.equal( second?.path, manifests[1] );
	assert.equal( resources.poll(), null );
});
