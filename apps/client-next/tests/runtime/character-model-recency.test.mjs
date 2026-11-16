/*
===========================================================================

character-model-recency.test.mjs - the model cache retires by recency

resources.ts keeps decoded character models within CHARACTER_MODELS and
CHARACTER_RESIDENT_BYTES, most recently used first. Recency used to be the
insertion order of a Set touched by delete and re-add; it is now a use
stamp per path, sorted only when the cache would overflow. This test drives
the owner through loads, touches and retains and holds every retained set
to the old delete-and-re-add order, modelled here.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterResources } = await import( "../../src/engine/runtime/characters/resources/resources.ts" );
const { characterBytes, CHARACTER_MODELS, CHARACTER_RESIDENT_BYTES } = await import(
	"../../src/engine/foundation/animation/character-budget.ts"
);

/*
================
fakeWorld

An asset owner that finishes a fetch when the test says so; each path's
model carries one image of the path's own size, so paths cost different
amounts.
================
*/
function fakeWorld( sizes ) {
	let nextId = 1;
	const done = new Set(), byId = new Map(), retained = [];
	const model = ( path ) => {
		const side = sizes.get( path );
		return { nodes: [], primitives: [], images: [ { width: side, height: side } ], clips: [] };
	};
	const assets = {
		available: () => 4,
		progress: () => null,
		health: () => ({ phase: "running" }),
		request( url ) {
			const id = nextId++;
			byId.set( id, new URL( url ).pathname );
			return id;
		},
		take( id ) {
			const path = byId.get( id );
			if ( !path || !done.has( path ) ) return null;
			const side = sizes.get( path );
			return { kind: "character", id, model: model( path ), images: [ { width: side, height: side } ] };
		},
		cancel() {},
		textureFeatures() {},
		install() {},
		dispose() {}
	};
	const renderer = {
		setCharacterModel() {},
		setCharacterAnimation() {
			return 0;
		},
		retainCharacterModels( paths ) {
			retained.push( paths );
		}
	};
	const resources = createCharacterResources(
		/** @type {any} */ (assets),
		/** @type {any} */ (renderer),
		"http://probe/"
	);
	const cost = ( path ) => {
		const side = sizes.get( path );
		return characterBytes( /** @type {any} */ (model( path )), [ { width: side, height: side } ] );
	};
	return { resources, done, retained, cost };
}

/*
================
oldCache

The retired implementation: a Set in use order (delete, then add, on every
use), kept from the newest within the count and byte budgets.
================
*/
function oldCache( cost ) {
	const loaded = new Set();
	return {
		loaded,
		use( path ) {
			loaded.delete( path );
			loaded.add( path );
		},
		retain() {
			let bytes = 0, count = 0;
			const keep = new Set();
			for ( const path of [ ...loaded ].reverse() ) {
				const c = cost( path );
				if ( count < CHARACTER_MODELS && bytes + c <= CHARACTER_RESIDENT_BYTES ) {
					keep.add( path );
					count++;
					bytes += c;
				}
			}
			for ( const path of [ ...loaded ] ) if ( !keep.has( path ) ) loaded.delete( path );
		}
	};
}

test("the model cache retires the same paths as the old delete-and-re-add order", () => {
	let seed = 7;
	const random = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
	const paths = Array.from( { length: 120 }, ( _, i ) => `/assets/char/m${i}.glb` );
	// 128 to 1536 pixels a side: a few dozen fit the resident budget, so
	// retains drop by bytes, and different paths need different room.
	const sizes = new Map( paths.map( path => [ path, 128 * (1 + Math.floor( random() * 12 )) ] ) );
	const world = fakeWorld( sizes ), resources = world.resources, old = oldCache( world.cost );
	let frame = 0, retains = 0, dropped = 0;
	const use = ( path ) => {
		resources.begin( ++frame );
		if ( !resources.ready( path ) ) {
			world.done.add( path );
			resources.poll();
			old.use( path );
			assert.equal( resources.ready( path ), true, path );
		}
		old.use( path );
	};
	for ( let step = 0; step < 3000; step++ ) {
		// Mostly a recent path, sometimes any: both orders get exercised.
		const pick = random() < .7 ? paths[Math.floor( random() * 30 )] : paths[Math.floor( random() * paths.length )];
		use( pick );
		if ( random() < .15 ) {
			resources.begin( ++frame );
			const before = old.loaded.size;
			resources.retainWanted( [] );
			old.retain();
			dropped += before - old.loaded.size;
			retains++;
			assert.deepStrictEqual(
				new Set( world.retained.at( -1 ) ),
				old.loaded,
				`retain ${retains} at step ${step}`
			);
		}
	}
	assert.ok( retains > 300 && dropped > 100, `${retains} retains dropped ${dropped} models` );
});

test("a cache within its budgets keeps every model", () => {
	const paths = Array.from( { length: 40 }, ( _, i ) => `/assets/char/s${i}.glb` );
	const world = fakeWorld( new Map( paths.map( path => [ path, 64 ] ) ) ), resources = world.resources;
	let frame = 0;
	for ( const path of paths ) {
		resources.begin( ++frame );
		resources.ready( path );
		world.done.add( path );
		resources.poll();
	}
	resources.begin( ++frame );
	resources.retainWanted( [] );
	assert.deepStrictEqual( new Set( world.retained.at( -1 ) ), new Set( paths ) );
});
