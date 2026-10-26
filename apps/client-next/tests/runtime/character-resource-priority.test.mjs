/*
===========================================================================

character-resource-priority.test.mjs - the wearer's own part owns the slot

A newly equipped weapon must not wait behind unrelated character
assets: the frame declares the local player's worn sources, and the
single load slot serves them before any other cold source starts.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterResources } = await import(
	"../../src/engine/runtime/characters/resources/resources.ts"
);

const WEAPON = "/assets/char/weapon.glb";
const FOREIGN = "/assets/char/foreign.glb";
const MANIFEST = "/assets/manifest.json";
const MODEL = { nodes: [], primitives: [], images: [], clips: [] };

/*
================
fakeWorld

An asset owner whose fetches finish only when the test says so, plus
the renderer surface the resource owner talks to.
================
*/
function fakeWorld() {
	let nextId = 1;
	/** @type {string[]} */
	const started = [];
	/** @type {number[]} */
	const cancelled = [];
	/** @type {string[]} */
	const loadedModels = [];
	const done = new Set(), failed = new Set();
	/** @type {Map<number, string>} */
	const byId = new Map();
	const assets = {
		available: () => 4,
		progress: () => null,
		health: () => ({ phase: "running" }),
		request( url ) {
			const id = nextId++;
			byId.set( id, url );
			started.push( url );
			return id;
		},
		take( id ) {
			const url = byId.get( id );
			if ( !url ) return null;
			const path = [ WEAPON, FOREIGN, MANIFEST ].find( p => url.endsWith( p ) );
			if ( !path || !done.has( path ) ) return null;
			if ( failed.has( path ) ) return { kind: "error", id, error: "boom" };
			return { kind: "character", id, model: MODEL, images: [] };
		},
		cancel( id ) {
			cancelled.push( id );
		},
		textureFeatures() {},
		install() {},
		dispose() {}
	};
	const renderer = {
		setCharacterModel( path ) {
			loadedModels.push( path );
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
	const finish = ( path, ok = true ) => {
		done.add( path );
		if ( !ok ) failed.add( path );
		resources.poll();
	};
	return { resources, started, cancelled, loadedModels, finish };
}

test("a foreign part cannot take the slot ahead of the wearer's declared part", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	resources.prioritize( [ WEAPON ] );
	assert.equal( resources.ready( FOREIGN ), false );
	assert.equal( world.started.length, 0, "the foreign part started ahead of the wearer" );
	assert.equal( resources.ready( WEAPON ), false );
	assert.equal( world.started.length, 1 );
	assert.ok( world.started[0].endsWith( WEAPON ) );
	world.finish( WEAPON );
	assert.equal( resources.ready( WEAPON ), true );
});

test("without a declared part the slot stays first-come", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	assert.equal( resources.ready( FOREIGN ), false );
	assert.equal( world.started.length, 1 );
	assert.ok( world.started[0].endsWith( FOREIGN ) );
});

test("priority waits for the load in flight, then takes the next slot", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	assert.equal( resources.ready( FOREIGN ), false );
	assert.equal( world.started.length, 1 );
	resources.begin( 2 );
	resources.prioritize( [ WEAPON ] );
	assert.equal( resources.ready( WEAPON ), false );
	assert.equal( world.started.length, 1, "the wearer's part displaced the load in flight" );
	assert.deepEqual( world.cancelled, [] );
	world.finish( FOREIGN );
	resources.begin( 3 );
	assert.equal( resources.ready( WEAPON ), false );
	assert.equal( world.started.length, 2 );
	assert.ok( world.started[1].endsWith( WEAPON ) );
});

test("a wearer part waiting out its retry does not hold the slot", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	resources.prioritize( [ WEAPON ] );
	assert.equal( resources.ready( WEAPON ), false );
	world.finish( WEAPON, false );
	// The failure's retry is two seconds out; other work proceeds.
	resources.begin( 2 );
	assert.equal( resources.ready( FOREIGN ), false );
	assert.equal( world.started.length, 2 );
	assert.ok( world.started[1].endsWith( FOREIGN ) );
});

test("the manifest lane yields to a declared wearer part", () => {
	const world = fakeWorld(), resources = world.resources;
	resources.begin( 1 );
	resources.prioritize( [ WEAPON ] );
	resources.manifest( MANIFEST );
	assert.equal( world.started.length, 0, "a manifest took the slot ahead of the wearer" );
	assert.equal( resources.ready( WEAPON ), false );
	assert.equal( world.started.length, 1 );
	assert.ok( world.started[0].endsWith( WEAPON ) );
});
