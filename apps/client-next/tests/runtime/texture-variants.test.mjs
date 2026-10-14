/*
===========================================================================

texture-variants.test.mjs - re-encoded textures for devices without BC

Port-only. A device that samples BC keeps the retail textures; one without
BC, with the Experimental switch on, is served the build's ASTC
re-encoding (foundation/assets/texture-variants.ts). The NTX container
admits the variant formats, which have no CPU fallback, and the asset
owner tells its worker the variant once: at once with the switch off,
when the device's features arrive with it on.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
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
const { chooseTextureVariant, textureVariantPath } = await load( "src/engine/foundation/assets/texture-variants.ts" );
const {
	decodeNativeTexture,
	decodeNativeSurface,
	nativeTextureFeature,
	nativeTextureLevelBytes,
	validateNativeTexture
} = await load( "src/engine/foundation/assets/native-texture.ts" );
const { nativePickAlpha } = await load( "src/engine/foundation/rendering/pick-alpha.ts" );
const { EXPERIMENTAL_STORAGE_KEY } = await load( "src/engine/foundation/ui/experimental-options.ts" );
const { createAssets } = await load( "src/engine/runtime/assets/assets.ts" );

const BC = "texture-compression-bc", ETC2 = "texture-compression-etc2", ASTC = "texture-compression-astc";

test("only a device without BC, with the switch on, is served a variant, and only the published ASTC set", () => {
	assert.equal( chooseTextureVariant( new Set( [ BC, ETC2, ASTC ] ), true ), null, "BC keeps the retail textures" );
	assert.equal( chooseTextureVariant( new Set( [ ETC2, ASTC ] ), false ), null, "the switch off is native" );
	assert.equal( chooseTextureVariant( new Set( [ ETC2, ASTC ] ), true ), "astc", "an iPhone samples both" );
	assert.equal( chooseTextureVariant( new Set( [ ETC2 ] ), true ), null, "no ETC2 set is published" );
	assert.equal( chooseTextureVariant( new Set( [ ASTC ] ), true ), "astc" );
	assert.equal( chooseTextureVariant( new Set(), true ), null, "no sampled format: the RGBA fallback" );
});

test("variants mirror native texture paths under one root, and nothing else has one", () => {
	assert.equal(
		textureVariantPath( "/assets/world/stone.texture", "astc" ),
		"/assets/texture-variants/astc/world/stone.texture"
	);
	assert.equal( textureVariantPath( "/assets/world/stone.png", "etc2" ), null );
	assert.equal( textureVariantPath( "/assets/texture-variants/etc2/world/stone.texture", "etc2" ), null );
	assert.equal( textureVariantPath( "/other/stone.texture", "etc2" ), null );
});

/*
================
ntx

An NTX container of the given four-character format code, 8x8 with a
full mip chain of blockBytes-byte 4x4 blocks.
================
*/
function ntx( code, blockBytes ) {
	const levels = [ 4, 1, 1, 1 ].map( blocks => new Uint8Array( blocks * blockBytes ).fill( 7 ) );
	const out = new Uint8Array( 20 + levels.reduce( ( sum, level ) => sum + level.length, 0 ) );
	const view = new DataView( out.buffer );
	view.setUint32( 0, 0x3158544e, true );
	view.setUint32( 4, 8, true );
	view.setUint32( 8, 8, true );
	view.setUint32( 12, Buffer.from( code, "ascii" ).readUInt32LE( 0 ), true );
	view.setUint32( 16, levels.length, true );
	let at = 20;
	for ( const level of levels ) {
		out.set( level, at );
		at += level.length;
	}
	return out;
}

test("the NTX container admits the variant formats, with their features and no CPU decoder", () => {
	/** @type {[string, string, number, string][]} */
	const formats = [
		[ "E2A1", "etc2-rgb8a1unorm", 8, ETC2 ],
		[ "E2A8", "etc2-rgba8unorm", 16, ETC2 ],
		[ "AS44", "astc-4x4-unorm", 16, ASTC ]
	];
	for ( const [code, format, blockBytes, feature] of formats ) {
		const texture = decodeNativeTexture( ntx( code, blockBytes ) );
		assert.equal( texture.format, format );
		assert.equal( texture.levels.length, 4 );
		assert.equal( nativeTextureLevelBytes( format, 8, 8 ), 4 * blockBytes );
		assert.equal( nativeTextureFeature( format ), feature );
		assert.throws(
			() => decodeNativeSurface( { width: 8, height: 8, format, bytes: texture.levels[0] } ),
			/No CPU decoder/
		);
	}
	assert.equal( nativeTextureFeature( "bc1-rgba-unorm" ), BC );
});

test("a variant carries its source's picking mask after its levels; nothing else may trail", () => {
	const mask = Buffer.concat( [ Buffer.from( "PKA8", "ascii" ), Buffer.alloc( 64, 9 ) ] );
	const texture = decodeNativeTexture( Buffer.concat( [ ntx( "AS44", 16 ), mask ] ) );
	assert.equal( texture.pickAlpha.length, 64 );
	assert.equal( texture.pickAlpha[63], 9 );
	assert.equal( validateNativeTexture( texture ), 4 * 16 + 3 * 16 + 64, "the mask counts toward residency" );
	assert.deepEqual( nativePickAlpha( texture ), { width: 8, height: 8, pixels: texture.pickAlpha } );
	const opaque = nativePickAlpha( decodeNativeTexture( ntx( "AS44", 16 ) ) );
	assert.deepEqual( [ opaque.width, opaque.height, opaque.pixels[0] ], [ 1, 1, 255 ] );
	assert.throws(
		() => decodeNativeTexture( Buffer.concat( [ ntx( "DXT1", 8 ), mask ] ) ),
		/extent/,
		"BC reads its own alpha"
	);
	assert.throws(
		() => decodeNativeTexture( Buffer.concat( [ ntx( "AS44", 16 ), mask.subarray( 0, 40 ) ] ) ),
		/extent/
	);
	const wrongTag = Buffer.from( mask );
	wrongTag.write( "JUNK", 0, "ascii" );
	assert.throws( () => decodeNativeTexture( Buffer.concat( [ ntx( "AS44", 16 ), wrongTag ] ) ), /extent/ );
	assert.throws( () => validateNativeTexture( { ...texture, pickAlpha: new Uint8Array( 3 ) } ), /picking mask/ );
});

/*
================
owner

An asset owner over a fake worker, with the switch saved as given.
================
*/
function owner( t, compressedTextures ) {
	const saved = Object.getOwnPropertyDescriptor( globalThis, "Worker" ),
		storage = Object.getOwnPropertyDescriptor( globalThis, "localStorage" );
	const messages = [];
	Object.defineProperty( globalThis, "Worker", {
		configurable: true,
		value: class {
			postMessage( message ) {
				messages.push( message );
			}
			terminate() {}
		}
	} );
	Object.defineProperty( globalThis, "localStorage", {
		configurable: true,
		value: {
			getItem: key => key === EXPERIMENTAL_STORAGE_KEY ? JSON.stringify( { compressedTextures } ) : null
		}
	} );
	t.after( () => {
		/** @type {[string, PropertyDescriptor | undefined][]} */
		const restore = [ [ "Worker", saved ], [ "localStorage", storage ] ];
		for ( const [name, old] of restore ) {
			if ( old ) Object.defineProperty( globalThis, name, old );
			else Reflect.deleteProperty( globalThis, name );
		}
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	return { assets, variants: () => messages.filter( message => message.kind === "texture-variant" ) };
}

test("with the switch off the worker is told at once; with it on, when the device's features arrive", t => {
	const off = owner( t, false );
	assert.deepEqual( off.variants(), [ { kind: "texture-variant", variant: null } ] );
	off.assets.textureFeatures( [ ETC2 ] );
	assert.equal( off.variants().length, 1, "told once" );
	const on = owner( t, true );
	assert.deepEqual( on.variants(), [], "the worker waits for the device" );
	on.assets.textureFeatures( [ ETC2, ASTC ] );
	on.assets.textureFeatures( [ ASTC ] );
	assert.deepEqual( on.variants(), [ { kind: "texture-variant", variant: "astc" } ] );
});

test("a device that samples BC is never served a variant, even with the switch on and ASTC present", t => {
	const pc = owner( t, true );
	pc.assets.textureFeatures( [ BC, ETC2, ASTC ] );
	assert.deepEqual( pc.variants(), [ { kind: "texture-variant", variant: null } ] );
});
