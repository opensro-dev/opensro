/*
===========================================================================

minimapTextureParity.test.mjs - native minimap tiles draw what the PNGs drew

Every minimap tile published as an NTX1 .texture must decode, through the
client's own fallback decoder, to exactly the pixels of the PNG the image
converter made from the same retail DDJ. The GPU samples the same blocks;
this pins the container to the source and the client decoder to the
converter's decode (measured bit-exact on all tiles, 2026-10-09).

===========================================================================
*/
import "../../../apps/client-next/tests/helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { test } from "node:test";

import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../lib/publishedAsset.mjs";
import { generatedPath } from "../../lib/generatedRoot.mjs";
import { decodePngRgba } from "../lib/pngRgba.mjs";

const { decodeNativeTexture, decodeNativeTextureLevel } = await import(
	pathToFileURL( path.resolve( "apps/client-next/src/engine/foundation/assets/native-texture.ts" ) ).href
);
const STAGING_IMAGES = generatedPath( "intermediate", "images" );

test("every native minimap tile decodes to its converted PNG's pixels", async () => {
	const catalog = readPublishedAssetJsonSync( "/assets/data/mission-dungeon-minimap.json" );
	const textures = catalog.tilePaths.filter( tile => tile.endsWith( ".texture" ) );
	assert.ok( textures.length > 3000, `expected the DXT1 minimap tiles as .texture, found ${textures.length}` );
	for ( const tile of textures ) {
		const native = decodeNativeTexture( new Uint8Array( readPublishedAssetBytesSync( tile ) ) );
		assert.equal( native.format, "bc1-rgba-unorm", tile );
		const decoded = decodeNativeTextureLevel( native, 0 );
		const relative = tile.slice( "/assets/images/".length ).replace( /\.texture$/, ".png" );
		const png = decodePngRgba( await readFile( path.join( STAGING_IMAGES, relative ) ) );
		assert.deepEqual( [ png.width, png.height ], [ native.width, native.height ], tile );
		assert.ok( Buffer.from( decoded ).equals( Buffer.from( png.rgba ) ), `${tile} differs from its converted PNG` );
	}
});
