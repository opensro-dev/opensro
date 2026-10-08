/*
===========================================================================

gpu-retirement.test.mjs - a released GPU resource outlives the frame naming it

WebGPU rejects a submit whose command buffer names a destroyed buffer or
texture ("used in submit while destroyed"), and the renderer fails. These
cases drive the real particle, geometry and renderer owners against a device
fake that enforces that rule (tests/helpers/strict-gpu.mjs): each releases
or grows a resource after a pass naming it was recorded, in the same frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_SHADER_STAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";

globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;
globalThis.GPUShaderStage = GPU_SHADER_STAGE;

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createRetirement, destroyNow } = await load( "src/engine/runtime/renderer/device/retirement.ts" );
const { createParticlePresentation } = await load( "src/engine/runtime/renderer/device/particles.ts" );
const { createGeometryResources } = await load( "src/engine/runtime/renderer/device/geometry.ts" );
const { createStaleDrawGuard } = await load( "src/engine/runtime/renderer/frame/stale-draws.ts" );
const { createRenderer } = await load( "src/engine/runtime/renderer/renderer.ts" );
const { PARTICLE_ACTOR, PARTICLE_RECORD } = await load( "src/engine/foundation/animation/particle-records.ts" );

const DESTROYED = /used in submit while destroyed/;
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );

test("empty particle streams clear once and retain pending work until encoding", async () => {
	const gpu = createStrictGpu(), particles = createParticlePresentation( gpu.device );
	await particles.ready;
	const draw = {},
		instances = gpu.device.createBuffer( { size: 160 } ),
		bones = gpu.device.createBuffer( { size: 64 } );
	const input = {
		rows: 1,
		slots: 1,
		live: 0,
		graph: false,
		view: 0,
		lifetime: 1,
		loop: false,
		records: new Float32Array( PARTICLE_RECORD ),
		actors: new Float32Array( PARTICLE_ACTOR ),
		axes: new Float32Array( 12 ),
		dirtyStart: 0,
		dirtyEnd: 1
	};
	const encode = () => {
		const encoder = gpu.device.createCommandEncoder( { label: "particles" } );
		particles.encode( encoder );
		gpu.device.queue.submit( [ encoder.finish() ] );
	};
	particles.present( draw, instances, bones, input );
	particles.present( draw, instances, bones, input );
	encode();
	assert.equal( particles.stats().dispatches, 1, "repeated empty presentation keeps its initial clear" );
	particles.present( draw, instances, bones, input );
	encode();
	assert.equal( particles.stats().dispatches, 1 );
	input.live = 1;
	particles.present( draw, instances, bones, input );
	encode();
	assert.equal( particles.stats().dispatches, 2 );
	input.live = 0;
	particles.present( draw, instances, bones, input );
	encode();
	assert.equal( particles.stats().dispatches, 3, "a previously live stream clears again" );
	particles.dispose();
	instances.destroy();
	bones.destroy();
});

test("a resource retired inside a frame is destroyed when the frame closes, outside one at once", () => {
	const retirement = createRetirement(), log = [];
	const resource = name => ({
		destroy() {
			log.push( name );
		}
	});
	retirement.retire( resource( "idle" ) );
	assert.deepEqual( log, [ "idle" ] );
	retirement.open();
	retirement.retire( resource( "first" ) );
	retirement.retire( resource( "second" ) );
	assert.deepEqual( log, [ "idle" ], "nothing dies while the frame may still name it" );
	assert.equal( retirement.waiting(), 2 );
	retirement.close();
	assert.deepEqual( log, [ "idle", "first", "second" ] );
	assert.equal( retirement.waiting(), 0 );
	// A second close (a disposed device, then its pending frame) is harmless.
	retirement.close();
	retirement.retire( resource( "after" ) );
	assert.deepEqual( log, [ "idle", "first", "second", "after" ] );
});

/*
================
encodeDeferredParticles

One frame as the deferred particle pass runs it: a stream is presented and
its pass encoded, then a second, larger stream needs a separate input page
before the command buffer is submitted. Returns the submit and cleanup.
================
*/
async function encodeDeferredParticles( gpu, retire ) {
	const particles = createParticlePresentation( gpu.device, retire );
	await particles.ready;
	const buffers = [];
	const presentation = rows => ({
		rows,
		slots: 1,
		graph: false,
		view: 0,
		lifetime: 1,
		loop: false,
		records: new Float32Array( rows * PARTICLE_RECORD ),
		actors: new Float32Array( rows * PARTICLE_ACTOR ),
		axes: new Float32Array( 12 ),
		dirtyStart: 0,
		dirtyEnd: 0
	});
	const present = rows => {
		const instances = gpu.device.createBuffer( { label: "instances", size: rows * 160 } );
		const bones = gpu.device.createBuffer( { label: "bones", size: rows * 64 } );
		buffers.push( instances, bones );
		particles.present( {}, instances, bones, presentation( rows ) );
	};
	particles.beginFrame();
	const encoder = gpu.device.createCommandEncoder( { label: "frame" } );
	present( 1 );
	particles.encode( encoder );
	// The deferred pass needs a larger page, while the first remains in use.
	present( 300 );
	particles.encode( encoder );
	return {
		submit: () => gpu.device.queue.submit( [ encoder.finish() ] ),
		dispose() {
			particles.dispose();
			for ( const buffer of buffers ) buffer.destroy();
		}
	};
}

test("deferred particle pages outlive every pass in their frame, then dispose completely", async () => {
	for ( const immediate of [ true, false ] ) {
		const gpu = createStrictGpu(), retirement = createRetirement();
		retirement.open();
		const frame = await encodeDeferredParticles( gpu, immediate ? destroyNow : retirement.retire );
		assert.doesNotThrow( frame.submit );
		assert.deepEqual( gpu.log, [ "submit frame" ] );
		retirement.close();
		frame.dispose();
		assert.equal( gpu.live(), 0 );
	}
});

test("particle frame pages and resident slots remain bounded during stream replacement", async () => {
	const gpu = createStrictGpu(), particles = createParticlePresentation( gpu.device );
	await particles.ready;
	const draw = {},
		instances = gpu.device.createBuffer( { size: 65 * 160 } ),
		bones = gpu.device.createBuffer( { size: 65 * 64 } );
	let retained = 0;
	for ( let frame = 0; frame < 100; frame++ ) {
		particles.beginFrame();
		particles.release( draw );
		const count = frame % 2 ? 65 : 1;
		particles.present( draw, instances, bones, {
			rows: 1,
			slots: count,
			graph: false,
			view: 0,
			lifetime: 1,
			loop: false,
			records: new Float32Array( count * PARTICLE_RECORD ),
			actors: new Float32Array( PARTICLE_ACTOR ),
			axes: new Float32Array( 12 ),
			dirtyStart: 0,
			dirtyEnd: count
		} );
		const encoder = gpu.device.createCommandEncoder();
		particles.encode( encoder );
		gpu.device.queue.submit( [ encoder.finish() ] );
		if ( frame === 3 ) retained = gpu.live();
		if ( frame > 3 ) assert.equal( gpu.live(), retained, "replacement reuses admitted GPU capacity" );
		assert.equal( particles.stats().streams, 1 );
	}
	particles.dispose();
	instances.destroy();
	bones.destroy();
	assert.equal( gpu.live(), 0 );
});

test("particle batching obeys aggregate storage, material and workgroup limits", async () => {
	const cases = [
		{ streams: 4, slots: 129, materialFloats: 0, groups: 4, dispatches: 4 },
		{ streams: 4, slots: 1, materialFloats: 4096, groups: 4, dispatches: 2 },
		{ streams: 5, slots: 1, materialFloats: 0, groups: 2, dispatches: 3 }
	];
	for ( const sample of cases ) {
		const gpu = createStrictGpu(), buffers = [];
		const maxBytes = 65536;
		gpu.device.limits = {
			maxStorageBufferBindingSize: maxBytes,
			maxBufferSize: maxBytes,
			maxComputeWorkgroupsPerDimension: sample.groups
		};
		const createBuffer = gpu.device.createBuffer;
		gpu.device.createBuffer = descriptor => {
			assert.ok( descriptor.size <= maxBytes, "every buffer fits the admitted device budget" );
			return createBuffer( descriptor );
		};
		const particles = createParticlePresentation( gpu.device );
		await particles.ready;
		particles.beginFrame();
		for ( let stream = 0; stream < sample.streams; stream++ ) {
			const instances = gpu.device.createBuffer( { size: sample.slots * 160 } );
			const bones = gpu.device.createBuffer( { size: sample.slots * 64 } );
			buffers.push( instances, bones );
			particles.present( {}, instances, bones, {
				rows: 1,
				slots: sample.slots,
				graph: false,
				view: 0,
				lifetime: 1,
				loop: false,
				records: new Float32Array( sample.slots * PARTICLE_RECORD ),
				actors: new Float32Array( PARTICLE_ACTOR ),
				axes: new Float32Array( 12 ),
				dirtyStart: 0,
				dirtyEnd: sample.slots,
				frames: sample.materialFloats ?
					{
						fps: 1,
						colors: new Float32Array( sample.materialFloats ),
						windows: new Float32Array( sample.materialFloats )
					} :
					undefined
			} );
		}
		const encoder = gpu.device.createCommandEncoder();
		particles.encode( encoder );
		gpu.device.queue.submit( [ encoder.finish() ] );
		assert.equal(
			particles.stats().dispatches,
			sample.dispatches,
			"work partitions instead of exceeding GPU limits"
		);
		particles.dispose();
		for ( const buffer of buffers ) buffer.destroy();
		assert.equal( gpu.live(), 0 );
	}
});

/*
================
releaseAfterRecord

One frame that records three draws, then releases one and outgrows another's
instance storage before the submit. Returns the submit.
================
*/
function releaseAfterRecord( gpu, retire ) {
	const { commands, draws } = geometryOwner( gpu, retire );
	const encoder = gpu.device.createCommandEncoder( { label: "frame" } );
	record( encoder, draws );
	commands.release( draws[1] );
	// Two instances outgrow the single slot the draw was uploaded with.
	commands.updateInstances( draws[2], new Float32Array( 32 ) );
	return () => gpu.device.queue.submit( [ encoder.finish() ] );
}

/*
================
geometryOwner

Geometry resources over the strict device, with three uploaded draws.
================
*/
function geometryOwner( gpu, retire ) {
	const resources = createGeometryResources(
		gpu.device,
		() => gpu.device,
		error => {
			throw error;
		},
		() => ({
			getBindGroupLayout() {
				return {};
			}
		}),
		{
			texture: () => {
				throw Error( "No image is uploaded" );
			},
			acquire() {},
			drop() {}
		},
		{},
		{},
		gpu.device.createBuffer( { label: "environment", size: 336 } ),
		() => ({}),
		undefined,
		"bgra8unorm",
		undefined,
		retire
	);
	const commands = resources.commands;
	const geometry = () => ({
		positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: identity()
	});
	return {
		commands,
		draws: [ commands.upload( geometry() ), commands.upload( geometry() ), commands.upload( geometry() ) ]
	};
}

/*
================
record

One render pass drawing each draw.
================
*/
function record( encoder, draws ) {
	const pass = encoder.beginRenderPass( { colorAttachments: [] } );
	for ( const draw of draws ) {
		pass.setBindGroup( 0, draw.binding );
		pass.setVertexBuffer( 0, draw.vertices );
		pass.setIndexBuffer( draw.indices, "uint32" );
		pass.drawIndexed( draw.indexCount, draw.instanceCount );
	}
	pass.end();
}

test("a draw an owner still lists after its release frame never reaches a later submit, and names its owner", () => {
	const gpu = createStrictGpu(),
		retirement = createRetirement(),
		{ commands, draws } = geometryOwner(
			gpu,
			retirement.retire
		);
	// Frame 1 releases the draw; its storage is destroyed when the frame closes.
	retirement.open();
	commands.release( draws[1] );
	retirement.close();
	// Frame 2: an owner hands the frame its stale list.
	const unguarded = gpu.device.createCommandEncoder( { label: "unguarded" } );
	record( unguarded, draws );
	assert.throws( () => gpu.device.queue.submit( [ unguarded.finish() ] ), DESTROYED );
	const reports = [], guard = createStaleDrawGuard( draw => commands.releasedDraw( draw ), r => reports.push( r ) );
	const guarded = gpu.device.createCommandEncoder( { label: "guarded" } );
	const live = guard.live( "characters", draws );
	record( guarded, live );
	gpu.device.queue.submit( [ guarded.finish() ] );
	assert.deepEqual( live, [ draws[0], draws[2] ] );
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].list, "characters" );
	assert.equal( reports[0].index, 1 );
	assert.match( reports[0].releaseStack, /geometry release/ );
	// The same stale site recurs every frame but is reported once.
	guard.live( "characters", draws );
	assert.equal( reports.length, 1 );
	assert.equal( guard.live( "characters", [ draws[0] ] )[0], draws[0], "a clean list passes through unchanged" );
});

test("a draw released or regrown after it is recorded outlives that frame's submit", () => {
	const unguarded = createStrictGpu();
	assert.throws( releaseAfterRecord( unguarded, destroyNow ), DESTROYED );

	const gpu = createStrictGpu(), retirement = createRetirement();
	retirement.open();
	releaseAfterRecord( gpu, retirement.retire )();
	assert.deepEqual( gpu.log, [ "submit frame" ], "nothing was destroyed before the submit" );
	retirement.close();
	const destroyed = gpu.log.slice( 1 ).map( entry => entry.replace( "destroy ", "" ) ).sort();
	assert.deepEqual( destroyed, [
		"geometry-indices",
		"geometry-instances",
		"geometry-instances",
		"geometry-material",
		"geometry-transform",
		"geometry-vertices"
	] );
});

/*
================
rendererOnStrictGpu

A renderer over the strict GPU, its navigator restored after the test.
================
*/
function rendererOnStrictGpu( t ) {
	const gpu = createStrictGpu();
	const original = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			gpu: {
				requestAdapter: async () => ({ features: new Set(), requestDevice: async () => gpu.device }),
				getPreferredCanvasFormat: () => "bgra8unorm"
			}
		}
	} );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "navigator", original );
		else Reflect.deleteProperty( globalThis, "navigator" );
	} );
	const context = {
		configure() {},
		unconfigure() {},
		getCurrentTexture: () => ({ createView: () => ({}) })
	};
	return { gpu, renderer: createRenderer( { getContext: () => context } ) };
}

/*
================
resizeLog

Run one frame, then a resized one, and return what the resized frame
submitted and destroyed.
================
*/
async function resizeLog( gpu, renderer ) {
	renderer.setImage( { width: 16, height: 16, close() {} } );
	await new Promise( resolve => setImmediate( resolve ) );
	renderer.frame( { width: 100, height: 100 } );
	assert.equal( renderer.phase(), "running", renderer.error() );
	gpu.log.length = 0;
	// The resize replaces the depth target while this frame is preparing.
	renderer.frame( { width: 200, height: 200 } );
	assert.equal( renderer.phase(), "running", renderer.error() );
	return [ ...gpu.log ];
}

test("the renderer keeps its frame open until the submit: a resized depth target dies after it", async t => {
	const { gpu, renderer } = rendererOnStrictGpu( t );
	try {
		// Native: the frame is copied to the swapchain, no presentation pass.
		assert.deepEqual( await resizeLog( gpu, renderer ), [ "submit sro-frame", "destroy surface-depth" ] );
	} finally {
		renderer.dispose();
	}
	assert.equal( gpu.live(), 0, "disposal destroys everything, the retired included" );
});

test("the experimental presentation pass publishes the offscreen frame and retires it after the submit", async t => {
	const { gpu, renderer } = rendererOnStrictGpu( t );
	try {
		renderer.experimentalVideo( {
			postProcessing: true,
			anisotropicFiltering: false,
			heightFog: false
		} );
		// The presentation pass is encoded inside the frame's own command
		// buffer after the unchanged HUD composition, so the frame is one submit; the
		// resize then retires the retained offscreen and the old depth.
		assert.deepEqual( await resizeLog( gpu, renderer ), [
			"submit sro-frame",
			"destroy deferred-frame-color",
			"destroy surface-depth"
		] );
	} finally {
		renderer.dispose();
	}
	assert.equal( gpu.live(), 0, "disposal destroys everything, the retired included" );
});

const { createImages } = await load( "src/engine/runtime/renderer/device/images.ts" );

/*
================
imageLeaseOwners

An image owner and a geometry owner over one strict device. Released
textures are retired immediately, so a texture a draw still binds would be
"used in submit while destroyed".
================
*/
function imageLeaseOwners( gpu ) {
	gpu.device.queue.copyExternalImageToTexture = () => {};
	const retire = resource => resource.destroy();
	const layout = { getBindGroupLayout: () => ({}) };
	const images = createImages( {
		current: () => gpu.device,
		fail: error => {
			throw error;
		},
		pipeline: () => layout,
		sampler: {},
		generateMips() {},
		retire
	} );
	const resources = createGeometryResources(
		gpu.device,
		() => gpu.device,
		error => {
			throw error;
		},
		() => layout,
		images.leases,
		{},
		{},
		gpu.device.createBuffer( { label: "environment", size: 336 } ),
		() => ({}),
		undefined,
		"bgra8unorm",
		undefined,
		retire
	);
	return { images, geometry: resources.commands, resources };
}

/*
================
imageLeaseGeometry
================
*/
function imageLeaseGeometry() {
	return {
		positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
		uvs: Float32Array.of( 0, 0, 1, 0, 0, 1 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: identity()
	};
}

test("a released image stays bound until the last geometry draw naming it is released", () => {
	const gpu = createStrictGpu(), { images, geometry, resources } = imageLeaseOwners( gpu );
	const image = images.commands.upload( { width: 4, height: 4 }, undefined, false );
	const texture = images.texture( image );
	const first = geometry.upload( imageLeaseGeometry(), image ),
		second = geometry.upload( imageLeaseGeometry(), image );
	// The world drops its claim (a scene change prunes the selection ring)
	// while the ring's draw is still resident.
	images.commands.release( image );
	assert.equal( texture.destroyed, false, "a bound image was destroyed by its owner's release" );
	// A texture setting rebinds every resident draw.
	assert.doesNotThrow( () => resources.textureOptions( true, 1 ) );
	geometry.release( first );
	assert.equal( texture.destroyed, false, "the image died while a second draw still binds it" );
	geometry.release( second );
	assert.equal( texture.destroyed, true, "the last draw's release did not retire the released image" );
	assert.throws( () => images.texture( image ), /Stale image handle/ );
	assert.throws( () => geometry.upload( imageLeaseGeometry(), image ), /Stale image handle/ );
});

test("an image its owner keeps survives every draw that bound it", () => {
	const gpu = createStrictGpu(), { images, geometry } = imageLeaseOwners( gpu );
	const image = images.commands.upload( { width: 4, height: 4 }, undefined, false );
	const texture = images.texture( image );
	geometry.release( geometry.upload( imageLeaseGeometry(), image ) );
	assert.equal( texture.destroyed, false, "a draw's release retired an image its owner still holds" );
	images.commands.release( image );
	assert.equal( texture.destroyed, true );
	images.commands.release( image );
});
