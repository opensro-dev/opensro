/*
===========================================================================

world-references.test.mjs - tests for http.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

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
		referencesVersion: 2,
		skillLifecycleVersion: 1,
		refSkillSnapshot: [],
		refItemSnapshot: [ { refObjId: 7 } ]
	} ),
	bytes = new TextEncoder().encode( data ),
	hash = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
const identity = { path: `/transport/references/${hash}.json`, sha256: hash, bytes: bytes.length };
test("immutable public references require bounded bytes, matching content identity and exact public schema", async t => {
	const http = createSessionHttp(), signal = new AbortController().signal, base = "https://fixture.invalid";
	let response = new Response( data ), requested;
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		requested = String( url );
		assert.equal( options.cache, "force-cache" );
		assert.equal( options.credentials, "omit" );
		return response;
	} );
	assert.deepEqual( await http.references( identity, base, signal ), {
		refSkillSnapshot: [],
		refItemSnapshot: [ { refObjId: 7 } ]
	} );
	assert.equal( requested, base + identity.path );
	response = new Response( data );
	await http.references( identity, base + "/shards/a", signal );
	assert.equal( requested, base + "/shards/a" + identity.path, "an edge route prefixes the reference path" );
	response = new Response( data.replace( "[]", "{}" ) );
	await assert.rejects( http.references( identity, base, signal ), /digest/ );
	response = new Response( data + " " );
	await assert.rejects( http.references( identity, base, signal ), /byte limit/ );
	response = new Response( data.slice( 1 ) );
	await assert.rejects( http.references( identity, base, signal ), /size/ );
	response = new Response( "", { status: 404 } );
	await assert.rejects( http.references( identity, base, signal ), /404/ );
	await assert.rejects(
		http.references( { ...identity, path: "https://foreign.invalid/catalog" }, base, signal ),
		/identity/
	);
	await assert.rejects( http.references( { ...identity, bytes: 33 << 20 }, base, signal ), /identity/ );
	await assert.rejects( http.references( identity, "wss://fixture.invalid", signal ), /transport base/ );
});
test("references of the previous contract are refused", async t => {
	const legacy = JSON.stringify( { skillLifecycleVersion: 1, refSkillSnapshot: [] } ),
		legacyBytes = new TextEncoder().encode( legacy ),
		legacyHash = Buffer.from( await crypto.subtle.digest( "SHA-256", legacyBytes ) ).toString( "hex" );
	t.mock.method( globalThis, "fetch", async () => new Response( legacy ) );
	await assert.rejects(
		createSessionHttp().references(
			{ path: `/transport/references/${legacyHash}.json`, sha256: legacyHash, bytes: legacyBytes.length },
			"https://fixture.invalid",
			new AbortController().signal
		),
		/contract undefined, expected 2/
	);
});
