/*
===========================================================================

navigation-view.test.mjs - the main thread's navigation view (GH #1439)

The render-thread mover steps against a clipper the main thread's
navigation stream keeps (runtime/navigation/navigation.ts). It must answer
exactly as the simulation worker's clipper does, which receives the same
product as a structured clone. These tests install one published product
both ways and compare a seeded corpus of clips and surfaces, then check
the stream installs what it emits, under the same request id.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as assetBytes } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
const { createNavigation } = await import( "../../src/engine/foundation/navigation/clipper.ts" );
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const { createNavigationStream } = await import( "../../src/engine/runtime/navigation/navigation.ts" );

const JANGAN = 0x61a8;
const LEGS = 4000;
const REGION_SIZE = 1920;
const bytes = path => assetBytes( path, CLIENT_PUBLIC_ROOT );
const product = await createNavigationResources().resolve(
	bytes( "/assets/world/outdoor/regions/region-61a8.json" ),
	JANGAN,
	async path => bytes( path )
);

/*
================
random

A seeded generator (mulberry32): the corpus is the same on every run.
================
*/
function random( seed ) {
	let a = seed;
	return () => {
		a = (a + 0x6d2b79f5) | 0;
		let t = Math.imul( a ^ (a >>> 15), 1 | a );
		t = (t + Math.imul( t ^ (t >>> 7), 61 | t )) ^ t;
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

/*
================
answers

Every clip and surface answer one clipper gives for the corpus, query
outputs included.
================
*/
function answers( navigation ) {
	const next = random( 1439 ), out = [];
	for ( let i = 0; i < LEGS; i++ ) {
		// From inside the centre region to anywhere up to 60 units away,
		// which crosses into the neighbours near the edges.
		const from = navigation.surface( {
			regionId: JANGAN,
			x: next() * REGION_SIZE,
			y: 0,
			z: next() * REGION_SIZE,
			angle: 0
		} );
		const to = { ...from, x: from.x + (next() - .5) * 120, z: from.z + (next() - .5) * 120 };
		const query = { slide: next() < .5 };
		const clipped = navigation.clip( from, to, query );
		const cursor = {};
		const surfaced = navigation.surface( to, from, query.owner, cursor );
		out.push( JSON.stringify( { from, clipped, query, surfaced, cursor } ) );
	}
	return out;
}

test("a clipper fed a structured clone answers exactly as one fed the original", () => {
	const main = createNavigation(), worker = createNavigation();
	main.install( JANGAN, product );
	worker.install( JANGAN, structuredClone( product ) );
	const expected = answers( worker ), actual = answers( main );
	assert.equal( actual.length, LEGS );
	let blocked = 0;
	for ( let i = 0; i < LEGS; i++ ) {
		assert.equal( actual[i], expected[i], "leg " + i );
		if ( JSON.parse( actual[i] ).query.status ) blocked++;
	}
	// The corpus has to reach walls and objects, or it proves nothing.
	assert.ok( blocked > LEGS / 50, `only ${blocked} legs touched an obstacle` );
});

/*
================
assetsWith

An asset owner that answers the catalog first and then each navigation
request with the next of results, in order.
================
*/
function assetsWith( ...results ) {
	const catalog = new TextEncoder().encode( JSON.stringify( {
		regionsById: {
			"0x61a8": [ { bundlePublicPath: "/assets/world/outdoor/regions/region-61a8.json" } ],
			"0x61a9": [ { bundlePublicPath: "/assets/world/outdoor/regions/region-61a9.json" } ]
		}
	} ) );
	const requests = [];
	// Only the four methods the stream calls; the rest of AssetOwner is unused.
	const owner = /** @type {import("../../src/engine/contracts/assets").AssetOwner} */ (/** @type {unknown} */ ({
		available: () => true,
		request( url ) {
			requests.push( url );
			return requests.length;
		},
		take( id ) {
			return id === 1 ? { kind: "bytes", buffer: catalog.buffer } : results[id - 2];
		},
		cancel() {}
	}));
	return { requests, owner };
}

/*
================
walkUntil

Steps the stream at pose until it has emitted count commands.
================
*/
function walkUntil( stream, pose, commands, count ) {
	for ( let i = 0; i < 8 && commands.length < count; i++ ) stream.step( pose, undefined, undefined, undefined, i );
}

test("the stream installs the product it emits, under the emitted request id", () => {
	const commands = [], { owner } = assetsWith( { kind: "navigation", product } );
	const stream = createNavigationStream( owner, command => commands.push( command ), "http://test.invalid" );
	const pose = { regionId: JANGAN, x: 1640, y: 0, z: 1390, angle: 0 };
	assert.equal( stream.view(), null );
	walkUntil( stream, pose, commands, 1 );
	const command = defined( commands[0] );
	const view = defined( stream.view() );
	assert.equal( view.requestId, command.requestId );
	assert.equal( command.bundle, product );
	const reference = createNavigation();
	reference.install( JANGAN, product );
	assert.deepEqual( answers( view.navigation ).slice( 0, 200 ), answers( reference ).slice( 0, 200 ) );
	stream.dispose();
	assert.equal( stream.view(), null );
});

test("a refused product keeps the previous view, as the worker keeps its product", () => {
	const commands = [];
	const refused = { regionId: JANGAN + 1, objects: [], complete: false, navmesh: { regions: "not a list" } };
	const { owner } = assetsWith( { kind: "navigation", product }, { kind: "navigation", product: refused } );
	const stream = createNavigationStream( owner, command => commands.push( command ), "http://test.invalid" );
	const pose = { regionId: JANGAN, x: 1640, y: 0, z: 1390, angle: 0 };
	walkUntil( stream, pose, commands, 1 );
	const first = defined( stream.view() );
	// The first product admitted, the player crosses into the next region.
	stream.step( pose, JANGAN, undefined, first.requestId, 10 );
	walkUntil( stream, { ...pose, regionId: JANGAN + 1 }, commands, 2 );
	// The worker refuses the same bytes and reports it; until then, and after,
	// both sides hold the first product under the first id.
	assert.equal( defined( commands[1] ).bundle, refused );
	const after = defined( stream.view() );
	assert.equal( after.requestId, first.requestId );
	assert.equal( after.navigation, first.navigation );
	assert.equal( after.navigation.available( pose ), true );
});
