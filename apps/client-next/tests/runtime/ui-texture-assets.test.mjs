/*
===========================================================================

ui-texture-assets.test.mjs - the UI loads .texture images as native textures

A .texture path is requested as container bytes (no PNG decode) and
published as the decoded native texture; PNG paths keep the image decode.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createUiAssets } = await import( sourceFileUrl( "src/engine/runtime/ui/resources/resources.ts" ).href );

/*
================
ntx1

An NTX1 container: magic, width, height, DXT1 fourCC, one level of zero blocks.
================
*/
function ntx1( width, height ) {
	const levelBytes = (width / 4) * (height / 4) * 8;
	const bytes = new Uint8Array( 20 + levelBytes );
	const view = new DataView( bytes.buffer );
	view.setUint32( 0, 0x3158544e, true );
	view.setUint32( 4, width, true );
	view.setUint32( 8, height, true );
	view.setUint32( 12, 0x31545844, true );
	view.setUint32( 16, 1, true );
	return bytes.buffer;
}

test("a .texture path loads as its native texture, a .png path as an image", () => {
	const requests = [], results = new Map(), published = [];
	const assets = {
		available: () => 8,
		request: ( url, limit, decode ) => {
			requests.push( { url, decode } );
			return requests.length;
		},
		take: id => results.get( id ),
		cancel() {}
	};
	const loader = createUiAssets(
		assets,
		( id, image ) => published.push( [ id, image ] ),
		"https://fixture.invalid/"
	);
	const tile = "/assets/images/Media_extracted/minimap/1x1.texture",
		icon = "/assets/images/Media_extracted/icon/a.png";
	loader.step( [ tile, icon ], 0 );
	assert.deepEqual( requests.map( r => r.decode ), [ undefined, "png" ] );
	results.set( 1, { kind: "bytes", buffer: ntx1( 256, 256 ) } );
	loader.step( [ tile, icon ], 1 );
	const [id, native] = published.at( -1 );
	assert.equal( id, tile );
	assert.equal( native.kind, "native-texture" );
	assert.equal( native.format, "bc1-rgba-unorm" );
	assert.deepEqual( loader.size( tile ), [ 256, 256 ] );
	loader.dispose();
});
