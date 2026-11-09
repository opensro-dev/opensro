/*
===========================================================================

experimental-device.test.mjs - retained experimental device state

Exercise the real device and geometry owners through an observed strict
GPU fake. Startup preferences, caster-independent binding selection and
preview suppression are checked at the GPU resource/uniform boundary.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE, GPU_SHADER_STAGE } from "../helpers/strict-gpu.mjs";

const { createDevice } = await import( "../../src/engine/runtime/renderer/device/device.ts" );
const { experimentalOptions, experimentalVideo } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { D3DBLEND_ONE, D3DBLEND_ZERO, D3DBLEND_SRCALPHA } = await import(
	"../../src/engine/foundation/rendering/blend-state.ts"
);
const { ENVIRONMENT_STAGES_OFFSET, ENVIRONMENT_BLOCK_BYTES } = await import(
	"../../src/engine/runtime/renderer/device/environment-block.ts"
);
const IDENTITY = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
const SHADOW_BINDING = 13;
const SHADOW_STAGE = 5;
const MATERIAL_SHADOW_RECEIVER_FLOAT = 46;

/*
================
mockGlobal
================
*/
function mockGlobal( t, name, value ) {
	const previous = Object.getOwnPropertyDescriptor( globalThis, name );
	Object.defineProperty( globalThis, name, { configurable: true, value } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, name, previous );
		else Reflect.deleteProperty( globalThis, name );
	} );
}

/*
================
options
================
*/
function options( value = {} ) {
	return experimentalVideo( experimentalOptions( value ) );
}

/*
================
deviceHarness

Observe descriptors without replacing the production owners or rewriting
their source. The strict fake still tracks borrowed resources for disposal.
================
*/
async function deviceHarness( t, initial = {} ) {
	const { device } = createStrictGpu();
	Object.assign( device.limits, {
		maxStorageBufferBindingSize: 128 * 1024 * 1024,
		maxBufferSize: 256 * 1024 * 1024,
		maxComputeWorkgroupsPerDimension: 65535
	} );
	mockGlobal( t, "GPUBufferUsage", GPU_BUFFER_USAGE );
	mockGlobal( t, "GPUTextureUsage", GPU_TEXTURE_USAGE );
	mockGlobal( t, "GPUShaderStage", GPU_SHADER_STAGE );
	mockGlobal( t, "navigator", {
		gpu: {
			requestAdapter: async () => ({ features: new Set(), requestDevice: async () => device }),
			getPreferredCanvasFormat: () => "bgra8unorm"
		}
	} );
	const pipelines = new WeakMap(), bindings = new WeakMap(), materials = new WeakMap(), stages = [];
	const createPipeline = device.createRenderPipeline;
	t.mock.method( device, "createRenderPipeline", descriptor => {
		const pipeline = createPipeline();
		pipelines.set( pipeline, descriptor );
		return pipeline;
	} );
	t.mock.method( device, "createRenderPipelineAsync", async descriptor => {
		const pipeline = createPipeline();
		pipelines.set( pipeline, descriptor );
		return pipeline;
	} );
	const createBinding = device.createBindGroup;
	t.mock.method( device, "createBindGroup", descriptor => {
		const binding = createBinding( descriptor );
		bindings.set( binding, descriptor );
		return binding;
	} );
	const writeBuffer = device.queue.writeBuffer;
	t.mock.method( device.queue, "writeBuffer", ( buffer, offset, data, start = 0, size = data.byteLength ) => {
		writeBuffer( buffer );
		// Material uniforms are slab ranges: one table per buffer, keyed by offset.
		if ( buffer.label === "geometry-material" && data instanceof ArrayBuffer ) {
			if ( !materials.has( buffer ) ) materials.set( buffer, new Map() );
			materials.get( buffer ).set( offset, new Float32Array( data.slice( start, start + size ) ) );
		}
		if ( buffer.label === "environment" && offset === ENVIRONMENT_STAGES_OFFSET ) {
			stages.push( Array.from( data ) );
		}
	} );
	const owner = createDevice( false, false );
	t.after( () => owner.dispose() );
	assert.equal( owner.phase(), "starting" );
	owner.experimentalVideo( options( initial ) );
	await new Promise( resolve => setImmediate( resolve ) );
	assert.equal( owner.phase(), "running", owner.error() ?? "device did not start" );
	const geometry = owner.geometry() ?? assert.fail( "running device must expose geometry" );
	/*
	================
	upload
	================
	*/
	function upload( world = true, omitWorld = false, material = {} ) {
		return geometry.upload( {
			positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
			indices: Uint32Array.of( 0, 1, 2 ),
			transform: IDENTITY,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true, ...material },
			...(omitWorld ? {} : { world })
		} );
	}
	/*
	================
	shadowTexture
	================
	*/
	function shadowTexture( draw ) {
		return bindings.get( draw.binding ).entries.find( entry => entry.binding === SHADOW_BINDING ).resource.texture;
	}
	/*
	================
	format
	================
	*/
	function format( draw ) {
		return pipelines.get( draw.pipeline ).fragment.targets[0].format;
	}
	/*
	================
	shadowReceiver
	================
	*/
	function shadowReceiver( draw ) {
		const resource = bindings.get( draw.binding ).entries.find( entry => entry.binding === 2 ).resource;
		return materials.get( resource.buffer ).get( resource.offset )[MATERIAL_SHADOW_RECEIVER_FLOAT];
	}
	/*
	================
	pipelineDescriptor
	================
	*/
	function pipelineDescriptor( draw ) {
		return pipelines.get( draw.pipeline );
	}
	return { owner, upload, shadowTexture, format, stages, shadowReceiver, pipelineDescriptor };
}

for ( const hdrToneMap of [ false, true ] ) {
	for ( const sunShadow of [ false, true ] ) {
		test(`startup retains HDR=${hdrToneMap} and sun shadows=${sunShadow} before the first draw`, async t => {
			const { owner, upload, shadowTexture, format } = await deviceHarness( t, { hdrToneMap, sunShadow } );
			const draw = upload();
			assert.equal( format( draw ), hdrToneMap ? "rgba16float" : "bgra8unorm" );
			assert.equal( shadowTexture( draw ).label, sunShadow ? "sun-shadow-cascade" : "sun-shadow-dummy" );
			assert.equal( owner.sunShadow().active(), false, "binding must not depend on a prepared caster frame" );
		});
	}
}

test("shadow toggles rebind old and new draws before prepare and survive HDR repipelining", async t => {
	const { owner, upload, shadowTexture, format } = await deviceHarness( t );
	const first = upload(), dummy = shadowTexture( first );
	const originalBinding = first.binding;
	owner.experimentalVideo( options( { sunShadow: true } ) );
	assert.notEqual( first.binding, originalBinding );
	const cascade = shadowTexture( first );
	assert.equal( cascade.label, "sun-shadow-cascade" );
	assert.equal( owner.sunShadow().active(), false );
	const second = upload();
	assert.equal( shadowTexture( second ), cascade );

	owner.sunShadow().prepare( true, [ 0, 0, 0 ], [ 0, 1, 0 ] );
	owner.experimentalVideo( options( { hdrToneMap: true, sunShadow: true } ) );
	assert.equal( format( first ), "rgba16float" );
	assert.equal( shadowTexture( first ), cascade );
	owner.experimentalVideo( options() );
	assert.equal( owner.sunShadow().active(), true, "last prepared state must not keep a disabled binding alive" );
	for ( const draw of [ first, second, upload() ] ) {
		assert.equal( shadowTexture( draw ), dummy );
		assert.equal( format( draw ), "bgra8unorm" );
	}
	owner.experimentalVideo( options( { sunShadow: true } ) );
	assert.equal( shadowTexture( first ), cascade, "re-enable reuses the owned cascade" );
});

test("effective preview suppression clears the receiver flag without losing the saved preference", async t => {
	const { owner, stages } = await deviceHarness( t, { hdrToneMap: true, sunShadow: true, perPixelLighting: true } );
	const environment = new Float32Array( ENVIRONMENT_BLOCK_BYTES / Float32Array.BYTES_PER_ELEMENT );
	for ( const effective of [ true, false, false, true ] ) {
		owner.worldView( IDENTITY, environment, effective );
		assert.deepEqual( stages.at( -1 ), [ 0, 0, 0, 0, 1, effective ? 1 : 0, 1, 0 ] );
	}
	owner.experimentalVideo( options() );
	owner.worldView( IDENTITY, environment, true );
	assert.equal( stages.at( -1 )[SHADOW_STAGE], 0, "effective enable cannot override an off preference" );
});

test("only explicit world draws receive the cascade, including after shadow and HDR toggles", async t => {
	const { owner, upload, shadowReceiver } = await deviceHarness( t, { sunShadow: true } );
	const world = upload( true ), portrait = upload( false ), unspecified = upload( false, true );
	for ( const flags of [ {}, { sunShadow: true, hdrToneMap: true }, { sunShadow: true } ] ) {
		owner.experimentalVideo( options( flags ) );
		assert.equal( shadowReceiver( world ), 1 );
		assert.equal( shadowReceiver( portrait ), 0 );
		assert.equal( shadowReceiver( unspecified ), 0 );
	}
});

test("HDR coverage is derived only for fullscreen preview and leaves portrait source draws unchanged", async t => {
	const { owner, upload, pipelineDescriptor } = await deviceHarness( t );
	const blended = { blend: true }, cutout = { blend: false, alphaCutoff: 0.5 };
	const world = upload( true, false, blended ), local = upload( false, false, blended );
	const worldCutout = upload( true, false, cutout ), localCutout = upload( false, false, cutout );
	const nativeBlended = local.pipeline, nativeCutout = localCutout.pipeline;
	assert.equal( world.pipeline, nativeBlended, "native world/local descriptors share one pipeline" );
	assert.equal( worldCutout.pipeline, nativeCutout );
	const nativeBlend = pipelineDescriptor( local ).fragment.targets[0].blend;
	assert.equal( nativeBlend.alpha.srcFactor, "src-alpha" );
	const geometry = owner.geometry() ?? assert.fail( "missing geometry" );
	const sources = [ local, localCutout ];
	assert.equal( geometry.hdrPreview( sources ), sources, "native path returns original draws" );

	owner.experimentalVideo( options( { hdrToneMap: true } ) );
	const [preview, previewCutout] = geometry.hdrPreview( sources );
	const worldDescriptor = pipelineDescriptor( world ), localDescriptor = pipelineDescriptor( preview );
	assert.equal( local.pipeline, world.pipeline, "HDR portraits retain the ordinary authored blend" );
	assert.deepEqual( pipelineDescriptor( local ).fragment.targets[0].blend, nativeBlend );
	assert.notEqual( preview.binding, local.binding );
	assert.equal( geometry.hdrPreview( sources )[0].binding, preview.binding, "unchanged binding is cached" );
	assert.equal( preview.indexCount, local.indexCount );
	assert.equal( preview.instanceCount, local.instanceCount );
	assert.equal( preview.instanceCapacity, local.instanceCapacity );
	geometry.updateInstances( local, new Float32Array() );
	const empty = geometry.hdrPreview( sources )[0];
	assert.equal( empty.binding, preview.binding );
	assert.equal( empty.instanceCount, 0, "cached siblings refresh getter-backed counts" );
	geometry.updateInstances( local, IDENTITY );
	assert.deepEqual(
		worldDescriptor.fragment.targets[0].blend,
		nativeBlend,
		"world HDR retains native blend semantics"
	);
	assert.deepEqual( localDescriptor.fragment.targets[0].blend.color, nativeBlend.color );
	assert.deepEqual( localDescriptor.fragment.targets[0].blend.alpha, {
		srcFactor: "one",
		dstFactor: "one-minus-src-alpha",
		operation: "add"
	} );
	assert.equal( pipelineDescriptor( worldCutout ).fragment.entryPoint, "fs" );
	assert.equal( pipelineDescriptor( localCutout ).fragment.entryPoint, "fs" );
	assert.equal( pipelineDescriptor( previewCutout ).fragment.entryPoint, "fsPreviewOpaque" );
	assert.equal( pipelineDescriptor( previewCutout ).fragment.targets[0].blend, undefined );
	assert.equal( upload( false, false, blended ).pipeline, local.pipeline, "new portraits retain authored alpha" );
	owner.experimentalVideo( options( { hdrToneMap: true, sunShadow: true } ) );
	assert.notEqual( geometry.hdrPreview( sources )[0].binding, preview.binding, "source rebind invalidates sibling" );

	owner.experimentalVideo( options() );
	assert.equal( local.pipeline, nativeBlended );
	assert.equal( world.pipeline, nativeBlended );
	assert.equal( localCutout.pipeline, nativeCutout );
	assert.equal( worldCutout.pipeline, nativeCutout );
	assert.equal( geometry.hdrPreview( sources ), sources );
	owner.experimentalVideo( options( { hdrToneMap: true } ) );
	geometry.release( local );
	assert.throws( () => geometry.hdrPreview( sources ), /Unknown or released sibling draw/ );
	assert.throws( () => geometry.hdrPreview( [ preview ] ), /Unknown or released sibling draw/ );
});

test("the MSAA stage's draws keep single-sampled siblings for single-sampled passes", async t => {
	const { owner, upload, pipelineDescriptor } = await deviceHarness( t );
	const material = { blend: true };
	const draw = upload( false, false, material ), sources = [ draw ];
	const geometry = owner.geometry() ?? assert.fail( "missing geometry" );
	assert.equal( geometry.singleSampled( sources ), sources, "the native path returns original draws" );
	assert.equal( pipelineDescriptor( draw ).multisample, undefined );

	owner.experimentalVideo( options( { msaa: true } ) );
	assert.equal( pipelineDescriptor( draw ).multisample.count, 4, "the shared draw follows the 4x scene" );
	const [sibling] = geometry.singleSampled( sources );
	const [preview] = geometry.hdrPreview( sources );
	assert.equal( pipelineDescriptor( sibling ).multisample, undefined, "portraits and particles stay 1x" );
	assert.equal( pipelineDescriptor( preview ).multisample, undefined, "the preview pass stays 1x without HDR" );
	assert.notEqual( sibling.binding, draw.binding );
	assert.equal( geometry.singleSampled( sources )[0].binding, sibling.binding, "an unchanged binding is cached" );

	owner.experimentalVideo( options() );
	assert.equal( geometry.singleSampled( sources ), sources );
	assert.equal( pipelineDescriptor( draw ).multisample, undefined, "off restores the native pipeline" );
});

test("only HDR fullscreen additive siblings preserve coverage while portrait sources retain native alpha", async t => {
	const { owner, upload, pipelineDescriptor } = await deviceHarness( t );
	const materials = [
		{ blend: true, blendPair: { source: D3DBLEND_SRCALPHA, destination: D3DBLEND_ONE } },
		{ blend: true, blendPair: { source: D3DBLEND_ONE, destination: D3DBLEND_ONE } },
		{ blend: true, blendPair: { source: D3DBLEND_ONE, destination: D3DBLEND_ZERO } }
	];
	const rows = materials.map( material => {
		const world = upload( true, false, material ), local = upload( false, false, material );
		return { world, local, native: local.pipeline, blend: pipelineDescriptor( local ).fragment.targets[0].blend };
	} );
	owner.experimentalVideo( options( { hdrToneMap: true } ) );
	const geometry = owner.geometry() ?? assert.fail( "missing geometry" );
	for ( const [index, row] of rows.entries() ) {
		const actual = pipelineDescriptor( geometry.hdrPreview( [ row.local ] )[0] ).fragment;
		assert.deepEqual( pipelineDescriptor( row.local ).fragment.targets[0].blend, row.blend );
		assert.equal( pipelineDescriptor( row.local ).fragment.entryPoint, "fs" );
		assert.deepEqual( pipelineDescriptor( row.world ).fragment.targets[0].blend, row.blend );
		assert.deepEqual( actual.targets[0].blend.color, row.blend.color );
		if ( index < 2 ) {
			assert.deepEqual( actual.targets[0].blend.alpha, {
				srcFactor: "zero",
				dstFactor: "one",
				operation: "add"
			} );
			assert.equal( actual.entryPoint, "fs" );
		} else {
			assert.equal( actual.entryPoint, "fsPreviewOpaque" );
			assert.deepEqual( actual.targets[0].blend.alpha, row.blend.alpha );
		}
	}
	owner.experimentalVideo( options() );
	for ( const row of rows ) {
		assert.equal( row.local.pipeline, row.native );
		assert.equal( row.world.pipeline, row.native );
	}
});
