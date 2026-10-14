/*
===========================================================================

packs-cancel.test.mjs - a cancelled read never waits on shared work
(packs.ts untilCancelled)

The loader acknowledges a cancellation when the request settles. A read
parked on a shared small-pack or manifest download used to settle only when
that download did: "Asset worker did not acknowledge cancelling
/assets/effects/programs.json within 30 s" (prod, 2026-10-14).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { createHash } from "node:crypto";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createPacks } = await load( "src/engine/runtime/assets/worker/packs/packs.ts" );

/*
================
sha
================
*/
function sha( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
fixture

One small pack holding a.bin and b.json, and its manifest.
================
*/
function fixture() {
	const a = Uint8Array.of( 1, 2, 3 ), b = new TextEncoder().encode( '{"fixture":true}' );
	const entries = [
		{ path: "/assets/a.bin", offset: 0, length: a.length, mime: "application/octet-stream", sha256: sha( a ) },
		{ path: "/assets/b.json", offset: a.length, length: b.length, mime: "application/json", sha256: sha( b ) }
	];
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: entries } ) );
	const bytes = new Uint8Array( 12 + header.length + a.length + b.length );
	bytes.set( Buffer.from( "SROPACK2" ) );
	new DataView( bytes.buffer ).setUint32( 8, header.length, true );
	bytes.set( header, 12 );
	bytes.set( a, 12 + header.length );
	bytes.set( b, 12 + header.length + a.length );
	const pack = { path: "/assets/packs/fixture.bin", bytes: bytes.length, sha256: sha( bytes ), assetCount: 2 };
	const manifest = {
		version: 2,
		groups: [ { name: "test", assetCount: 2, packs: [ pack ] } ],
		assets: entries.map( e => ({ ...e, packPath: pack.path }) )
	};
	return { bytes, manifest };
}

test("a cancelled read waiting on a shared pack download settles at once; the download goes on for others", async () => {
	const f = fixture();
	/** @type {( value?: unknown ) => void} */
	let open = () => {};
	const gate = new Promise( resolve => {
		open = resolve;
	} );
	const packs = createPacks( async url => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		await gate;
		return f.bytes;
	} );
	const cancelled = new AbortController(), kept = new AbortController();
	const doomed = packs.read( new URL( "http://localhost/assets/a.bin" ), 100, cancelled.signal );
	const other = packs.read( new URL( "http://localhost/assets/b.json" ), 100, kept.signal );
	// Let both reach the shared, still unfinished pack download.
	await new Promise( resolve => setTimeout( resolve, 20 ) );
	cancelled.abort();
	const settled = await Promise.race( [
		doomed.then( () => "resolved", error => String( error ) ),
		new Promise( resolve => setTimeout( () => resolve( "still waiting" ), 200 ) )
	] );
	assert.match( settled, /cancelled/, "the loader can acknowledge the cancellation now" );
	open();
	assert.deepEqual( JSON.parse( new TextDecoder().decode( await other ) ), { fixture: true } );
	packs.dispose();
});
