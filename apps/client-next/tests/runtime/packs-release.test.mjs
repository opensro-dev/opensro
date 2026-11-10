/*
===========================================================================

packs-release.test.mjs - the pack reader's word on a moved release

An open tab holds the manifest it loaded with. When a publish replaces
a pack, the member's loose recovery may still serve it, so no job ever
fails - yet the page is stale. These tests pin the two signals the
release watch consumes (#3104): a pack the server answers 404 for is
reported gone once, and the live packs manifest compares against the
admitted one (a data publish keeps the entry bundle).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fixture } from "../helpers/packs-fixture.mjs";

const { createPacks } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/packs/packs.ts" ).href
);

test("a missing pack is reported gone once, and loose recovery still serves", async () => {
	const f = fixture(), gone = [];
	const packs = createPacks(
		async url => {
			if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
			if ( url.endsWith( ".bin" ) && url.includes( "/packs/" ) ) {
				throw Object.assign( new Error( "missing" ), { status: 404 } );
			}
			return Uint8Array.of( 1, 2, 3 );
		},
		() => {},
		() => {},
		packPath => gone.push( packPath )
	);
	const signal = new AbortController().signal;
	assert.deepEqual( [ ...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ) ], [ 1, 2, 3 ] );
	assert.deepEqual( [ ...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ) ], [ 1, 2, 3 ] );
	assert.deepEqual( gone, [ f.manifest.assets[0].packPath ] );
	packs.dispose();
});

test("the live packs manifest compare sees a moved client-data generation", async () => {
	const f = fixture();
	let live = JSON.stringify( f.manifest );
	const packs = createPacks( async url => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( live );
		return f.bytes;
	} );
	const signal = new AbortController().signal;
	await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal );
	assert.equal( await packs.liveManifestChanged( "http://localhost" ), false );
	live = JSON.stringify( { ...f.manifest, generatedAt: "later" } );
	assert.equal( await packs.liveManifestChanged( "http://localhost" ), true );
	packs.dispose();
});
