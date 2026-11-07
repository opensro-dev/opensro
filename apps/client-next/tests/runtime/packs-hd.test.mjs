/*
===========================================================================

packs-hd.test.mjs - the HD texture set behind its Experimental row

#2061: the set lives under its own /hd root with its own index, outside
the default manifest. With the row off nothing of the set is fetched, not
even its index; with it on, a held texture serves its HD bytes and a
missing one its original. The default pack behaviour stays in
packs.test.mjs; both suites share tests/helpers/packs-fixture.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { fixture, sha } from "../helpers/packs-fixture.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createPackIndex } = await load( "src/engine/runtime/assets/worker/packs/index/index.ts" );
const { createPacks } = await load( "src/engine/runtime/assets/worker/packs/packs.ts" );

/*
================
hdFixture

Two one-member worlds: the default pack holding a native texture and a
texture the HD set does not hold, and the HD set's own pack under /hd
holding the first one's x2 variant (#2061), with distinct bytes. The
download names every request so a test can count what was fetched.
================
*/
function hdFixture() {
	const member = ( path, data ) => {
		const entry = { path, offset: 0, length: data.length, mime: "application/octet-stream", sha256: sha( data ) };
		const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: [ entry ] } ) );
		const bytes = new Uint8Array( 12 + header.length + data.length );
		bytes.set( Buffer.from( "SROPACK2" ) );
		new DataView( bytes.buffer ).setUint32( 8, header.length, true );
		bytes.set( header, 12 );
		bytes.set( data, 12 + header.length );
		return { entry, bytes };
	};
	const original = member( "/assets/world/t.texture", Uint8Array.of( 1, 2, 3 ) ),
		plain = member( "/assets/world/plain.texture", Uint8Array.of( 4, 5, 6 ) ),
		hd = member( "/hd/texture-variants/bc7/world/t.texture", Uint8Array.of( 7, 8, 9 ) );
	const packOf = ( path, members ) => ({
		path,
		bytes: members.bytes.length,
		sha256: sha( members.bytes ),
		assetCount: 1
	});
	const assetPack = packOf( "/assets/packs/assets.bin", original );
	const plainPack = packOf( "/assets/packs/plain.bin", plain );
	const hdPack = packOf( "/hd/packs/texture-variants-hd-bc7-001.bin", hd );
	const indexOf = ( group, packs, entries ) => ({
		version: 2,
		groups: [ { name: group, assetCount: packs.reduce( ( sum, p ) => sum + p.assetCount, 0 ), packs } ],
		assets: entries.map( ( entry, i ) => ({ ...entry, packPath: packs[i].path }) )
	});
	const assetIndex = indexOf( "test", [ assetPack, plainPack ], [ original.entry, plain.entry ] );
	const hdIndex = indexOf( "texture-variants-hd-bc7", [ hdPack ], [ hd.entry ] );
	const requests = [];
	const packs = createPacks( async url => {
		requests.push( url );
		if ( url.endsWith( "/assets/packs/manifest.json" ) ) {
			return new TextEncoder().encode( JSON.stringify( assetIndex ) );
		}
		if ( url.endsWith( "/hd/packs/manifest.json" ) ) {
			return new TextEncoder().encode( JSON.stringify( hdIndex ) );
		}
		if ( url.endsWith( assetPack.path ) ) return original.bytes;
		if ( url.endsWith( plainPack.path ) ) return plain.bytes;
		if ( url.endsWith( hdPack.path ) ) return hd.bytes;
		throw Object.assign( new Error( "unexpected " + url ), { status: 404 } );
	} );
	return { packs, requests };
}

test("the pack index admits the HD set only under its own root", () => {
	const hdIndex = {
		version: 2,
		groups: [ {
			name: "texture-variants-hd-bc7",
			assetCount: 1,
			packs: [ { path: "/hd/packs/x.bin", bytes: 100, sha256: "a".repeat( 64 ), assetCount: 1 } ]
		} ],
		assets: [ {
			path: "/hd/texture-variants/bc7/world/t.texture",
			packPath: "/hd/packs/x.bin",
			offset: 0,
			length: 3,
			mime: "application/octet-stream",
			sha256: "b".repeat( 64 )
		} ]
	};
	const admitted = createPackIndex().manifest( hdIndex, "/hd" );
	assert.deepEqual( [ ...admitted.assets.keys() ], [ "/hd/texture-variants/bc7/world/t.texture" ] );
	// The default root never admits an /hd path, and /hd admits no /assets one.
	assert.throws( () => createPackIndex().manifest( hdIndex ), /Invalid asset path/ );
});

test("with the HD row off, texture reads fetch nothing from the HD index", async () => {
	const f = hdFixture(), signal = new AbortController().signal;
	assert.deepEqual(
		[ ...await f.packs.read( new URL( "http://localhost/assets/world/t.texture" ), 100, signal ) ],
		[ 1, 2, 3 ],
		"the original serves"
	);
	assert.deepEqual(
		[ ...await f.packs.read( new URL( "http://localhost/assets/world/plain.texture" ), 100, signal ) ],
		[ 4, 5, 6 ]
	);
	assert.equal(
		f.requests.filter( url => url.includes( "/hd/" ) ).length,
		0,
		"off fetches nothing of the set, not even its index"
	);
	f.packs.dispose();
});

test("with the HD row on, a held texture serves its HD bytes and a missing one its original", async () => {
	const f = hdFixture(), signal = new AbortController().signal;
	f.packs.hdTextures( "bc7" );
	assert.deepEqual(
		[ ...await f.packs.read( new URL( "http://localhost/assets/world/t.texture" ), 100, signal ) ],
		[ 7, 8, 9 ],
		"the HD member serves"
	);
	assert.deepEqual(
		[ ...await f.packs.read( new URL( "http://localhost/assets/world/plain.texture" ), 100, signal ) ],
		[ 4, 5, 6 ],
		"a path the set does not hold keeps its original"
	);
	assert.ok(
		f.requests.some( url => url.endsWith( "/hd/packs/manifest.json" ) ),
		"the set was learned from its own index"
	);
	// Off again: the original serves, and the set is forgotten.
	f.packs.hdTextures( null );
	assert.deepEqual(
		[ ...await f.packs.read( new URL( "http://localhost/assets/world/t.texture" ), 100, signal ) ],
		[ 1, 2, 3 ]
	);
	f.packs.dispose();
});

test("an HD index that will not come admits an empty set and the originals serve", async () => {
	const f = fixture(), requests = [];
	const packs = createPacks( async url => {
		requests.push( url );
		if ( url.endsWith( "/hd/packs/manifest.json" ) ) {
			throw Object.assign( new Error( "Asset HTTP 404" ), { status: 404 } );
		}
		return url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes;
	} );
	packs.hdTextures( "bc7" );
	assert.deepEqual(
		[ ...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ) ],
		[ 1, 2, 3 ]
	);
	packs.dispose();
});
