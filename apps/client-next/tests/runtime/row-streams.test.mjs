/*
===========================================================================

row-streams.test.mjs - shared instance streams and the portrait view

Fading opacities and hit point lights use renderer-owned scratch once per
batch instead of allocating once per primitive. Membership changes must
produce the same bytes as fresh arrays. Portrait targets reuse a view only
while its underlying texture remains resident.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { fillRowOpacities, fillRowPointLights } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
);
const { createUiResources } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/ui.ts" ).href );

/*
================
actor
================
*/
/** @param {ReturnType<typeof light> | undefined} pointLight */
const actor = ( gid, opacity, pointLight = undefined ) => ({
	gid,
	opacity,
	pointLight,
	pose: { regionId: 0, x: gid * 10, y: 0, z: 0, yaw: 0 }
});
/*
================
light
================
*/
const light = ( ambient, diffuse ) => ({
	pose: { regionId: 0, x: 1, y: 2, z: 3 },
	attenuation: 0.5,
	ambient,
	diffuse
});

test("a batch's opacity scratch matches a fresh allocation through growth and shrink", () => {
	const batch = {};
	const rows = [ actor( 1, 1 ), actor( 2, .5 ), actor( 3, .25 ), actor( 4, .75 ) ];
	const opacity = a => a.opacity ?? 1;
	// Grow: 2 rows, then 4 (past the initial power-of-two), then shrink to 1.
	const two = fillRowOpacities( batch, rows.slice( 0, 2 ), opacity );
	assert.deepEqual( [ ...two ], [ 1, .5 ] );
	const four = fillRowOpacities( batch, rows, opacity );
	assert.deepEqual( [ ...four ], [ 1, .5, .25, .75 ] );
	const one = fillRowOpacities( batch, rows.slice( 0, 1 ), opacity );
	assert.deepEqual( [ ...one ], [ 1 ] );
	// The shrunk view never leaks the retired rows' values.
	assert.equal( one.length, 1 );
	assert.equal( one.buffer, four.buffer, "shrinking reuses the backing store" );
	const reordered = fillRowOpacities( batch, [ rows[3], rows[1], rows[0] ], opacity );
	assert.equal( reordered.buffer, four.buffer );
	assert.deepEqual( [ ...reordered ], [ .75, .5, 1 ] );
	// A fresh scratch would produce the same bytes at every size.
	assert.deepEqual( [ ...fillRowOpacities( batch, rows, opacity ) ], rows.map( a => a.opacity ) );
	assert.equal( two.length, 2 );
});

test("the light scratch zeroes retired rows when the lights disappear", () => {
	const batch = {};
	const lit = [
		actor( 1, 1, light( [ .1, .2, .3 ], [ .4, .5, .6 ] ) ),
		actor( 2, 1, light( [ .7, .8, .9 ], [ .15, .25, .35 ] ) )
	];
	const both = fillRowPointLights( batch, lit, 0 );
	assert.equal( both.length, 24 );
	// Layout per row: placed position (x,y,z), attenuation, ambient rgb,
	// zero padding, diffuse rgb, zero padding - Float32 rounded.
	const f = v => Math.fround( v );
	assert.deepEqual( [ ...both.slice( 0, 12 ) ], [
		1,
		2,
		3,
		f( .5 ),
		f( .1 ),
		f( .2 ),
		f( .3 ),
		0,
		f( .4 ),
		f( .5 ),
		f( .6 ),
		0
	] );
	// Slots 7 and 11 stay zero padding.
	assert.equal( both[7], 0 );
	assert.equal( both[19], 0 );
	// The second row keeps its own light.
	assert.deepEqual(
		[ ...both.slice( 12, 24 ) ],
		[ 1, 2, 3, f( .5 ), f( .7 ), f( .8 ), f( .9 ), 0, f( .15 ), f( .25 ), f( .35 ), 0 ]
	);
	// Rows that lose their lights: the retired span reads zero, and a shorter
	// view never exposes stale bytes.
	const unlit = fillRowPointLights( batch, [ actor( 1, 1, undefined ) ], 0 );
	assert.equal( unlit.length, 12 );
	assert.equal( unlit.buffer, both.buffer );
	assert.deepEqual( [ ...unlit ], new Array( 12 ).fill( 0 ) );
	const mixed = fillRowPointLights( batch, [ actor( 3, 1 ), lit[1] ], 0 );
	assert.equal( mixed.buffer, both.buffer );
	assert.deepEqual( [ ...mixed.slice( 0, 12 ) ], new Array( 12 ).fill( 0 ) );
	assert.deepEqual( [ ...mixed.slice( 12 ) ], [
		1,
		2,
		3,
		f( .5 ),
		f( .7 ),
		f( .8 ),
		f( .9 ),
		0,
		f( .15 ),
		f( .25 ),
		f( .35 ),
		0
	] );
});

test("one scratch owner stays bounded through repeated full-roster submissions", () => {
	const scratch = {};
	const rows = Array.from(
		{ length: 512 },
		( _, i ) => actor( i + 1, (i % 7) / 7, i % 2 ? light( [ .1, .2, .3 ], [ .4, .5, .6 ] ) : undefined )
	);
	const opacities = fillRowOpacities( scratch, rows, row => row.opacity );
	const lights = fillRowPointLights( scratch, rows, 0 );
	assert.ok( opacities.buffer.byteLength + lights.buffer.byteLength <= 512 * 13 * 4 );
	for ( const count of [ 1, 17, 3, 512, 127, 512 ] ) {
		const batch = rows.slice( 0, count ).reverse();
		const opacity = fillRowOpacities( scratch, batch, row => row.opacity );
		const pointLights = fillRowPointLights( scratch, batch, 0 );
		assert.equal( opacity.buffer, opacities.buffer );
		assert.equal( pointLights.buffer, lights.buffer );
		assert.deepEqual( opacity, Float32Array.from( batch, row => row.opacity ) );
		assert.equal( pointLights.length, count * 12 );
		for ( let i = 0; i < count; i++ ) {
			assert.equal( pointLights[i * 12 + 3], batch[i].pointLight ? .5 : 0 );
			assert.equal( pointLights[i * 12 + 7], 0 );
			assert.equal( pointLights[i * 12 + 11], 0 );
		}
	}
});

test("the portrait target allocates one view per resident texture", async t => {
	const names = [ "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage" ];
	const descriptors = names.map( name => Object.getOwnPropertyDescriptor( globalThis, name ) );
	t.after( () => {
		for ( let i = 0; i < names.length; i++ ) {
			const descriptor = descriptors[i];
			if ( descriptor ) Object.defineProperty( globalThis, names[i], descriptor );
			else delete globalThis[names[i]];
		}
	} );
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	const textures = [];
	/** @type {any} */
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer: () => ({ destroy() {} }),
		createSampler: () => ({}),
		createTexture: () => {
			const texture = {
				views: 0,
				destroyed: false,
				destroy() {
					this.destroyed = true;
				},
				createView() {
					return { texture: this, serial: ++this.views };
				}
			};
			textures.push( texture );
			return texture;
		},
		createBindGroup: () => ({}),
		queue: { writeTexture() {}, writeBuffer() {}, copyExternalImageToTexture() {} }
	};
	const ui = createUiResources( device, "rgba8unorm" );
	t.after( () => ui.dispose() );
	await ui.ready;
	/** @type {any} */
	const first = ui.portraitTarget( "__p", 128, 128 );
	const again = ui.portraitTarget( "__p", 128, 128 );
	assert.equal( again, first, "the same slot returns the same view object" );
	assert.equal( first.texture.views, 1, "the mock returns fresh views, so this detects redundant allocation" );
	// A resize replaces the slot: a new view bound to the new texture.
	/** @type {any} */
	const resized = ui.portraitTarget( "__p", 64, 64 );
	assert.notEqual( resized, first );
	assert.notEqual( resized.texture, first.texture );
	assert.equal( first.texture.destroyed, true );
	assert.equal( resized.texture.views, 1 );
	// Returning to the original size replaces it again.
	/** @type {any} */
	const restored = ui.portraitTarget( "__p", 128, 128 );
	assert.notEqual( restored, resized );
	assert.notEqual( restored.texture, resized.texture );
	assert.notEqual( restored, first );
	assert.equal( resized.texture.destroyed, true );
	assert.equal( ui.portraitTarget( "__p", 128, 128 ), restored );
	assert.equal( restored.texture.views, 1 );
	ui.texture( "__p", null );
	assert.equal( restored.texture.destroyed, true );
	const recreated = ui.portraitTarget( "__p", 128, 128 );
	assert.notEqual( recreated, restored );
	ui.texture( "__p", { width: 1, height: 1, data: new Uint8ClampedArray( 4 ), colorSpace: "srgb" } );
	const uploaded = ui.portraitTarget( "__p", 1, 1 );
	assert.notEqual( uploaded, recreated );
	assert.equal( ui.portraitTarget( "__p", 1, 1 ), uploaded );
	ui.dispose();
	assert.equal( textures.every( texture => texture.destroyed ), true );
});
