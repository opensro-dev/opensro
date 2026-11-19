/*
===========================================================================

world-references.test.mjs - tests for http.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

A stored copy that does not match its own name is fetched again once with
the HTTP cache bypassed; every other failure is final.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createSessionHttp } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/http/http.ts" ).href
);
const data = JSON.stringify( {
		referencesVersion: 3,
		skillLifecycleVersion: 1,
		refSkillSnapshot: [],
		refItemSnapshot: [ { refObjId: 7 } ],
		refObjSnapshot: [ { refObjId: 9, kind: "monster" } ]
	} ),
	bytes = new TextEncoder().encode( data ),
	hash = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
const identity = { path: `/transport/references/${hash}.json`, sha256: hash, bytes: bytes.length };
const parsed = {
	refSkillSnapshot: [],
	refItemSnapshot: [ { refObjId: 7 } ],
	refObjSnapshot: [ { refObjId: 9, kind: "monster" } ]
};

/*
================
serve

A fetch double answering each cache mode with its own body (a string, a
Response factory or an Error to throw). It records every request, in order.
================
*/
function serve( t, bodies ) {
	const requests = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		requests.push( { url: String( url ), cache: options.cache, credentials: options.credentials } );
		const body = bodies[options.cache];
		if ( body === undefined ) throw new Error( "unexpected cache mode " + options.cache );
		if ( body instanceof Error ) throw body;
		return typeof body === "function" ? body() : new Response( body );
	} );
	return requests;
}

function load( ref = identity, base = "https://fixture.invalid" ) {
	return createSessionHttp().references( ref, base, new AbortController().signal );
}

test("a valid stored copy is read once from the HTTP cache, without credentials", async t => {
	const base = "https://fixture.invalid";
	const requests = serve( t, { "force-cache": data } );
	assert.deepEqual( await load( identity, base ), parsed );
	await load( identity, base + "/shards/a" );
	assert.deepEqual( requests, [
		{ url: base + identity.path, cache: "force-cache", credentials: "omit" },
		{ url: base + "/shards/a" + identity.path, cache: "force-cache", credentials: "omit" }
	], "an edge route prefixes the reference path" );
});

test("a damaged, cut or overlong stored copy is fetched again with the cache bypassed", async t => {
	for ( const stored of [ data.replace( "[]", "{}" ), data.slice( 1 ), data + " " ] ) {
		const requests = serve( t, { "force-cache": stored, "reload": data } );
		assert.deepEqual( await load(), parsed );
		assert.deepEqual( requests.map( request => request.cache ), [ "force-cache", "reload" ] );
		t.mock.restoreAll();
	}
});

test("a publication that is wrong on the network too fails after one retry", async t => {
	for (
		const { stored, refusal } of [
			{ stored: data.replace( "[]", "{}" ), refusal: /digest/ },
			{ stored: data.slice( 1 ), refusal: /size/ },
			{ stored: data + " ", refusal: /byte limit/ }
		]
	) {
		const requests = serve( t, { "force-cache": stored, "reload": stored } );
		await assert.rejects( load(), refusal );
		assert.equal( requests.length, 2 );
		t.mock.restoreAll();
	}
});

test("HTTP and network failures are not retried", async t => {
	const missing = serve( t, { "force-cache": () => new Response( "", { status: 404 } ) } );
	await assert.rejects( load(), /404/ );
	assert.equal( missing.length, 1 );
	t.mock.restoreAll();
	const offline = serve( t, { "force-cache": new TypeError( "Failed to fetch" ) } );
	await assert.rejects( load(), /Failed to fetch/ );
	assert.equal( offline.length, 1 );
});

test("a reference outside the transport identity is refused before any fetch", async t => {
	const requests = serve( t, {} );
	await assert.rejects( load( { ...identity, path: "https://foreign.invalid/catalog" } ), /identity/ );
	await assert.rejects( load( { ...identity, bytes: 33 << 20 } ), /identity/ );
	await assert.rejects( load( identity, "wss://fixture.invalid" ), /identity/ );
	assert.equal( requests.length, 0 );
});

/*
================
loadDocument

Serves one reference document under its own content identity.
================
*/
async function loadDocument( t, document ) {
	const text = JSON.stringify( document ),
		encoded = new TextEncoder().encode( text ),
		digest = Buffer.from( await crypto.subtle.digest( "SHA-256", encoded ) ).toString( "hex" );
	t.mock.method( globalThis, "fetch", async () => new Response( text ) );
	return createSessionHttp().references(
		{ path: `/transport/references/${digest}.json`, sha256: digest, bytes: encoded.length },
		"https://fixture.invalid",
		new AbortController().signal
	);
}

test("references of the previous contract are refused", async t => {
	// Contract 2 published no monster rows (#369); a browser of 3 needs them.
	await assert.rejects(
		loadDocument( t, {
			referencesVersion: 2,
			skillLifecycleVersion: 1,
			refSkillSnapshot: [],
			refItemSnapshot: []
		} ),
		/contract 2, expected 3/
	);
	await assert.rejects(
		loadDocument( t, {
			referencesVersion: 3,
			skillLifecycleVersion: 1,
			refSkillSnapshot: [],
			refItemSnapshot: []
		} ),
		/Invalid object references/
	);
});
