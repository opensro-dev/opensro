/*
===========================================================================

ui-residency.test.mjs - tests for ui.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createUiResources } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/ui.ts" ).href );
test("missing images produce no white primitives and late residency restores the correct draw indices", async () => {
	const oldBuffer = globalThis.GPUBufferUsage,
		oldTexture = globalThis.GPUTextureUsage,
		oldStage = globalThis.GPUShaderStage;
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer: () => ({ destroy() {} }),
		createSampler: () => ({}),
		createTexture: () => ({ createView: () => ({}), destroy() {} }),
		createBindGroup: () => ({}),
		queue: { writeTexture() {}, writeBuffer() {}, copyExternalImageToTexture() {} }
	};
	const ui = createUiResources( device, "rgba8unorm" );
	try {
		await ui.ready;
		const q = texture => ({
			texture,
			rect: [ 0, 0, 10, 10 ],
			uv: [ 0, 0, 1, 1 ],
			clip: [ 0, 0, 100, 100 ],
			color: [ 1, 1, 1, 1 ]
		});
		const scene = { revision: 1, width: 100, height: 100, quads: [ q( "" ), q( "/hover.png" ), q( "" ) ] };
		assert.deepEqual( ui.prepare( scene ).map( d => [ d.first, d.count ] ), [ [ 0, 1 ], [ 2, 1 ] ] );
		ui.texture( "/hover.png", { width: 1, height: 1, data: new Uint8ClampedArray( [ 0, 0, 0, 255 ] ) } );
		assert.deepEqual( ui.prepare( scene ).map( d => [ d.first, d.count ] ), [ [ 0, 1 ], [ 1, 1 ], [ 2, 1 ] ] );
		ui.texture( "/hover.png", null );
		assert.deepEqual( ui.prepare( scene ).map( d => [ d.first, d.count ] ), [ [ 0, 1 ], [ 2, 1 ] ] );
		const masked = { ...scene, quads: [ { ...q( "" ), mask: { texture: "/mask.png", rect: [ 0, 0, 10, 10 ] } } ] };
		assert.deepEqual( ui.prepare( masked ), [], "a missing mask must not reveal the unmasked square" );
		ui.texture( "/mask.png", { width: 1, height: 1, data: new Uint8ClampedArray( [ 0, 0, 0, 128 ] ) } );
		const first = ui.prepare( masked );
		assert.equal( first.length, 1 );
		assert.equal( ui.prepare( masked ), first );
		ui.texture( "/mask.png", null );
		assert.deepEqual( ui.prepare( masked ), [] );
		ui.texture( "/mask.png", { width: 2, height: 1, data: new Uint8ClampedArray( 8 ) } );
		const replaced = ui.prepare( masked );
		assert.equal( replaced.length, 1 );
		assert.notEqual( replaced[0].binding, first[0].binding, "replacement binds the new mask resource" );
		ui.portraitTarget( "__doll", 176, 318 );
		const portraitScene = { ...scene, quads: [ q( "__doll" ) ] };
		const inventory = ui.prepare( portraitScene );
		ui.portraitTarget( "__doll", 176, 318 );
		assert.equal( ui.prepare( portraitScene ), inventory, "unchanged viewport retains GPU bindings" );
		ui.portraitTarget( "__doll", 88, 168 );
		const mall = ui.prepare( portraitScene );
		assert.notEqual( mall[0].binding, inventory[0].binding, "mall viewport replaces the inventory target" );
		assert.throws( () => ui.portraitTarget( "__doll", 0, 168 ), /Invalid portrait extent/ );
		assert.throws( () => ui.portraitTarget( "__doll", 8192, 8192 ), /Invalid portrait extent/ );
		assert.equal( ui.prepare( portraitScene ), mall, "invalid replacement leaves the admitted target intact" );
		ui.portraitTarget( "__doll", 176, 318 );
		assert.notEqual( ui.prepare( portraitScene )[0].binding, mall[0].binding );
	} finally {
		ui.dispose();
		globalThis.GPUBufferUsage = oldBuffer;
		globalThis.GPUTextureUsage = oldTexture;
		globalThis.GPUShaderStage = oldStage;
	}
});

test("unchanged UI revisions retain GPU bytes; one moving coordinate uploads only its changed float", async t => {
	const saved = [ "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage" ].map(
		k => [ k, Object.getOwnPropertyDescriptor( globalThis, k ) ]
	);
	t.after( () => {
		for ( const [k, v] of saved ) {
			if ( v ) Object.defineProperty( globalThis, k, v );
			else delete globalThis[k];
		}
	} );
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	const writes = [], buffers = [];
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer: options => {
			const buffer = { label: options.label, bytes: new Uint8Array( options.size ), destroy() {} };
			buffers.push( buffer );
			return buffer;
		},
		createSampler: () => ({}),
		createTexture: () => ({ createView: () => ({}), destroy() {} }),
		createBindGroup: () => ({}),
		queue: {
			writeTexture() {},
			copyExternalImageToTexture() {},
			writeBuffer( buffer, offset, data ) {
				const bytes = new Uint8Array( data.buffer, data.byteOffset, data.byteLength );
				buffer.bytes.set( bytes, offset );
				writes.push( { buffer, offset, size: bytes.length } );
			}
		}
	};
	const ui = createUiResources( device, "rgba8unorm" );
	await ui.ready;
	const quad = {
			texture: "",
			rect: [ 2, 3, 10, 10 ],
			uv: [ 0, 0, 1, 1 ],
			clip: [ 0, 0, 100, 100 ],
			color: [ 1, 1, 1, 1 ]
		},
		scene = { revision: 1, width: 100, height: 100, quads: [ quad ] };
	const draws = ui.prepare( scene );
	writes.length = 0;
	assert.equal( ui.prepare( structuredClone( { ...scene, revision: 2 } ) ), draws );
	assert.equal( writes.length, 0 );
	ui.prepare( { ...scene, revision: 3, quads: [ { ...quad, rect: [ 2, 4, 10, 10 ] } ] } );
	assert.equal( writes.length, 1 );
	assert.equal( writes[0].offset, 4 );
	assert.equal( writes[0].size, 4 );
	assert.equal( new Float32Array( buffers[0].bytes.buffer )[1], 4 );
	writes.length = 0;
	ui.prepare( { ...scene, revision: 4, width: 200, quads: [ { ...quad, rect: [ 2, 4, 10, 10 ] } ] } );
	assert.equal( writes.length, 1 );
	assert.equal( writes[0].size, 16 );
	const stable = Object.freeze( { ...quad, rect: Object.freeze( [ 7, 8, 10, 10 ] ) } );
	ui.prepare( { ...scene, revision: 5, quads: [ stable ] } );
	writes.length = 0;
	// Growing storage while the first record stays retained must preserve it.
	ui.prepare( {
		...scene,
		revision: 6,
		quads: [ stable, ...Array.from( { length: 40 }, ( _, i ) => ({ ...quad, rect: [ i, 20, 10, 10 ] }) ) ]
	} );
	assert.equal( new Float32Array( buffers[0].bytes.buffer )[0], 7 );
	assert.equal( new Float32Array( buffers[0].bytes.buffer )[1], 8 );
	ui.prepare( { ...scene, revision: 7, quads: [ stable ] } );
	writes.length = 0;
	ui.prepare( { ...scene, revision: 8, quads: [ stable, { ...quad, rect: [ 90, 91, 10, 10 ] } ] } );
	assert.equal( new Float32Array( buffers[0].bytes.buffer )[28], 90 );
	assert.equal( new Float32Array( buffers[0].bytes.buffer )[29], 91 );
	// Native console vertex alpha must survive packing beside an ordinary solid quad.
	const gradient = { ...quad, color: [ 0, 0, 0, 200 / 255 ], rightColor: [ 0, 0, 0, 0 ] };
	const gradientDraws = ui.prepare( { ...scene, revision: 9, quads: [ gradient, quad ] } );
	const bytes = new Float32Array( buffers[0].bytes.buffer );
	assert.equal( bytes[11], Math.fround( 200 / 255 ) );
	assert.equal( bytes[27], 0 );
	assert.deepEqual( Array.from( bytes.slice( 52, 56 ) ), quad.color, "solid quads keep both edge colors equal" );
	writes.length = 0;
	assert.equal(
		ui.prepare( { ...scene, revision: 10, quads: [ { ...gradient, rightColor: [ 0, 0, 0, .25 ] }, quad ] } ),
		gradientDraws
	);
	assert.equal( writes.length, 1 );
	assert.equal( writes[0].offset, 27 * 4 );
	assert.equal( writes[0].size, 4 );
	ui.dispose();
});

test("composed native windows fit the descriptor budget without bypassing memory or descriptor limits", async t => {
	const saved = [ "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage" ].map(
		k => [ k, Object.getOwnPropertyDescriptor( globalThis, k ) ]
	);
	t.after( () => {
		for ( const [k, v] of saved ) {
			if ( v ) Object.defineProperty( globalThis, k, v );
			else delete globalThis[k];
		}
	} );
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	let allocated = 0, destroyed = 0;
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer: () => ({ destroy() {} }),
		createSampler: () => ({}),
		createTexture: () => {
			allocated++;
			return {
				createView: () => ({}),
				destroy() {
					destroyed++;
				}
			};
		},
		createBindGroup: () => ({}),
		queue: { writeTexture() {}, writeBuffer() {}, copyExternalImageToTexture() {} }
	};
	const ui = createUiResources( device, "rgba8unorm" ),
		pixel = { width: 1, height: 1, data: new Uint8ClampedArray( 4 ) };
	try {
		await ui.ready;
		for ( let i = 0; i < 300; i++ ) ui.texture( "sprite:" + i, pixel );
		ui.texture( "large", { ...pixel, width: 2048, height: 4096 } );
		const before = allocated;
		assert.throws(
			() => ui.texture( "too-many-bytes", { ...pixel, width: 2048, height: 4096 } ),
			/residency budget/
		);
		assert.equal( allocated, before );
		ui.texture( "large", null );
		for ( let i = 300; i < 511; i++ ) ui.texture( "sprite:" + i, pixel );
		assert.throws( () => ui.texture( "too-many-descriptors", pixel ), /residency budget/ );
		ui.texture( "sprite:0", null );
		ui.texture( "replacement", pixel );
	} finally {
		ui.dispose();
	}
	assert.equal( allocated, destroyed );
});
