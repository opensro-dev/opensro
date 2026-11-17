/*
===========================================================================

characterMultiTexture.test.mjs - embedding the ModDataMultiTex texture

The GLB exporter embeds each texture a ModDataMultiTex(Rev) names once,
listing its texture index per modifier. The payload decoding is checked
against real BSRs in the client's asset-backed scenery-modifiers test.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { embedCharacterMultiTextures } from "../../build/shared/characterMultiTexture.mjs";

test("each named texture is embedded once and indexed per modifier", () => {
	const json = { images: [ { name: "base" } ], textures: [ { source: 0, sampler: 0 } ] };
	const reads = [], views = [];
	const read = path => {
		reads.push( path );
		return { bytes: Buffer.from( path ), mime: "image/png" };
	};
	const material = ( paths ) => ({
		extras: {
			sroBsrModifiers: {
				multiTextureModifiers: paths.map( texturePath => ({ texturePath }) )
			}
		}
	});
	const first = material( [ "prim\\mtrl\\etc\\robe_line_03.ddj", "prim\\mtrl\\etc\\robe_line_04.ddj" ] );
	const second = material( [ "PRIM\\MTRL\\ETC\\ROBE_LINE_03.DDJ" ] );
	const append = bytes => views.push( bytes ) - 1;
	embedCharacterMultiTextures( json, first, append, read );
	embedCharacterMultiTextures( json, second, append, read );
	assert.deepEqual( reads, [ "prim/mtrl/etc/robe_line_03.ddj", "prim/mtrl/etc/robe_line_04.ddj" ] );
	assert.deepEqual( first.extras.sroMultiTextures, [ 1, 2 ] );
	assert.deepEqual( second.extras.sroMultiTextures, [ 1 ] );
	assert.equal( json.images.length, 3 );
	// A material without the modifier carries no stale index list.
	const plain = { extras: { sroBsrModifiers: { multiTextureModifiers: [] }, sroMultiTextures: [ 9 ] } };
	embedCharacterMultiTextures( json, plain, append, read );
	assert.equal( plain.extras.sroMultiTextures, undefined );
});
