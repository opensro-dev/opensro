/*
===========================================================================

moonlitThiefSuit.test.mjs - the Moonlit Ash recolour build step

The recolour changes only the endpoint colours of BC blocks: a block keeps
its mode (no texel turns transparent), its endpoint texels take the
recipe's colour, and a model keeps its length with every byte outside its
native textures untouched.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	MOONLIT_MESHES,
	moonlitColour,
	moonlitGlb,
	moonlitModelPath,
	recolourNativeTexture
} from "../../build/char/moonlitThiefSuit.mjs";
import {
	decodeNativeTexture,
	decodeNativeTextureLevel
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";

const NTX_MAGIC = 0x3158544e;
const DXT1 = 0x31545844;
const DXT5 = 0x35545844;
const NATIVE_MIME = "application/x-sro-texture";

/*
================
pack565
================
*/
function pack565( r, g, b ) {
	return Math.round( r * 31 ) << 11 | Math.round( g * 63 ) << 5 | Math.round( b * 31 );
}

/*
================
ntx

A one-level 4x4 NTX texture from raw blocks.
================
*/
function ntx( format, block ) {
	const header = Buffer.alloc( 20 );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( 4, 4 );
	header.writeUInt32LE( 4, 8 );
	header.writeUInt32LE( format, 12 );
	header.writeUInt32LE( 1, 16 );
	return Buffer.concat( [ header, block ] );
}

/*
================
dxt1

A DXT1 block: two endpoints, then 16 two-bit indices.
================
*/
function dxt1( c0, c1, indices ) {
	const block = Buffer.alloc( 8 );
	block.writeUInt16LE( c0, 0 );
	block.writeUInt16LE( c1, 2 );
	block.writeUInt32LE( indices >>> 0, 4 );
	return block;
}

/*
================
texels
================
*/
function texels( bytes ) {
	return decodeNativeTextureLevel( decodeNativeTexture( new Uint8Array( bytes ) ), 0 );
}

test("the recipe turns indigo cloth to pale silk and gold trim to pale gold, and barely tints neutrals", () => {
	const [r, g, b] = moonlitColour( 60 / 255, 70 / 255, 140 / 255 );
	const saturation = (Math.max( r, g, b ) - Math.min( r, g, b )) / Math.max( r, g, b );
	assert.ok( saturation < 0.15, "cloth loses its colour: " + saturation );
	assert.ok( Math.max( r, g, b ) > 140 / 255, "cloth brightens" );
	const gold = moonlitColour( 200 / 255, 160 / 255, 60 / 255 );
	assert.ok( gold[0] >= gold[1] && gold[1] > gold[2], "trim stays warm" );
	assert.deepEqual( moonlitColour( 0.5, 0.5, 0.5 ).map( c => Math.round( c * 1000 ) ), [ 500, 500, 520 ] );
});

test("a DXT1 block keeps its mode and its endpoint texels take the recipe's colour", () => {
	// Every index once per row: 0, 1, 2, 3.
	const indices = 0xe4e4e4e4;
	let seed = 12345;
	const random = () => (seed = seed * 1103515245 + 12345 >>> 0) / 0x100000000;
	for ( let i = 0; i < 400; i++ ) {
		let c0 = random() * 0x10000 | 0, c1 = random() * 0x10000 | 0;
		if ( c0 === c1 ) continue;
		const source = ntx( DXT1, dxt1( c0, c1, indices ) );
		const before = texels( source ), after = texels( recolourNativeTexture( source ) );
		for ( let texel = 0; texel < 16; texel++ ) {
			assert.equal( after[texel * 4 + 3], before[texel * 4 + 3], "alpha of texel " + texel );
		}
		// Texels 0 and 1 of each row are the two endpoints.
		for ( const texel of [ 0, 1 ] ) {
			const want = moonlitColour( ...[ 0, 1, 2 ].map( c => before[texel * 4 + c] / 255 ) );
			for ( let c = 0; c < 3; c++ ) {
				assert.ok( Math.abs( after[texel * 4 + c] - want[c] * 255 ) <= 9, `block ${i} texel ${texel}` );
			}
		}
	}
});

test("a four-colour pair that becomes equal is one solid colour, never transparent", () => {
	const grey = pack565( 0.5, 0.5, 0.5 );
	const block = dxt1( grey + 1, grey, 0xffffffff );
	const after = texels( recolourNativeTexture( ntx( DXT1, block ) ) );
	for ( let texel = 0; texel < 16; texel++ ) assert.equal( after[texel * 4 + 3], 255 );
});

test("a DXT5 texture keeps its alpha block and only its colour endpoints change", () => {
	const alpha = Buffer.from( [ 255, 0, 1, 2, 3, 4, 5, 6 ] );
	const colour = dxt1( pack565( 60 / 255, 70 / 255, 140 / 255 ), pack565( 0.1, 0.1, 0.2 ), 0x1b1b1b1b );
	const source = ntx( DXT5, Buffer.concat( [ alpha, colour ] ) );
	const out = recolourNativeTexture( source );
	assert.equal( out.length, source.length );
	assert.deepEqual( out.subarray( 20, 28 ), alpha );
	assert.deepEqual( out.subarray( 32, 36 ), source.subarray( 32, 36 ) );
	assert.notDeepEqual( out.subarray( 28, 32 ), source.subarray( 28, 32 ) );
});

test("a model keeps its length and every byte outside its native textures", () => {
	const texture = ntx( DXT1, dxt1( pack565( 60 / 255, 70 / 255, 140 / 255 ), 0, 0 ) );
	const other = Buffer.from( "geometry-bytes-!" );
	const json = {
		asset: { version: "2.0" },
		buffers: [ { byteLength: other.length + texture.length } ],
		bufferViews: [
			{ buffer: 0, byteOffset: 0, byteLength: other.length },
			{ buffer: 0, byteOffset: other.length, byteLength: texture.length }
		],
		images: [ { bufferView: 1, mimeType: NATIVE_MIME } ]
	};
	let text = Buffer.from( JSON.stringify( json ) );
	text = Buffer.concat( [ text, Buffer.alloc( (4 - text.length % 4) % 4, 0x20 ) ] );
	let bin = Buffer.concat( [ other, texture ] );
	bin = Buffer.concat( [ bin, Buffer.alloc( (4 - bin.length % 4) % 4 ) ] );
	const header = Buffer.alloc( 12 ), jsonHeader = Buffer.alloc( 8 ), binHeader = Buffer.alloc( 8 );
	header.writeUInt32LE( 0x46546c67, 0 );
	header.writeUInt32LE( 2, 4 );
	header.writeUInt32LE( 12 + 8 + text.length + 8 + bin.length, 8 );
	jsonHeader.writeUInt32LE( text.length, 0 );
	jsonHeader.writeUInt32LE( 0x4e4f534a, 4 );
	binHeader.writeUInt32LE( bin.length, 0 );
	binHeader.writeUInt32LE( 0x004e4942, 4 );
	const glb = Buffer.concat( [ header, jsonHeader, text, binHeader, bin ] );
	const out = moonlitGlb( glb );
	assert.equal( out.length, glb.length );
	const start = 12 + 8 + text.length + 8 + other.length;
	assert.deepEqual( out.subarray( 0, start ), glb.subarray( 0, start ) );
	assert.deepEqual( out.subarray( start, start + texture.length ), recolourNativeTexture( texture ) );
	assert.notDeepEqual( out.subarray( start, start + texture.length ), texture );
});

test("each level-20 thief model maps to its higher suit, and the variant sits beside it", () => {
	assert.equal( MOONLIT_MESHES.size, 4 );
	assert.deepEqual( MOONLIT_MESHES.get( "res/item/china/man_item/thief_02.bsr" ), {
		mesh: "res/item/china/man_item/thief_03.bsr",
		warmCloth: false
	} );
	assert.deepEqual( MOONLIT_MESHES.get( "res/item/china/woman_item/thief_02.bsr" ), {
		mesh: "res/item/china/woman_item/thief_03.bsr",
		warmCloth: true
	} );
	assert.equal(
		MOONLIT_MESHES.get( "res/item/europe/woman_item/thief_eu_01.bsr" ).mesh,
		"res/item/europe/woman_item/thief_eu_03.bsr"
	);
	assert.equal(
		moonlitModelPath( "/assets/char/equipment/ch_m_1.glb" ),
		"/assets/char/equipment/ch_m_1_moonlit.glb"
	);
	assert.throws( () => moonlitModelPath( "/assets/char/equipment/ch_m_1.texture" ) );
});

test("warm cloth turns dark mauve-brown to silk only when asked, and keeps skin", () => {
	// The CH woman's thief_03 cloth (hue ~20, saturation ~0.28, value ~0.2)
	// and skin (hue ~14, saturation ~0.5, value ~0.4).
	const cloth = [ 0.2, 0.16, 0.144 ], skin = [ 0.4, 0.26, 0.2 ];
	assert.deepEqual(
		moonlitColour( ...cloth ).map( c => c.toFixed( 3 ) ),
		[ 0.2, 0.16, 0.15 ].map( c => c.toFixed( 3 ) )
	);
	const silk = moonlitColour( ...cloth, true );
	assert.ok( Math.max( ...silk ) > 0.35 && silk[2] >= silk[0], "cloth turns light and cool: " + silk );
	const kept = moonlitColour( ...skin, true );
	skin.forEach( ( c, i ) => assert.ok( Math.abs( kept[i] - c * [ 1, 1, 1.04 ][i] ) < 0.02, "skin stays: " + kept ) );
});
