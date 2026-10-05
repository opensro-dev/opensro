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
growArenaAfterEncode

One frame as the deferred particle pass runs it: a stream is presented and
its pass encoded, then a second, larger stream grows the frame arena before
the command buffer is submitted. Returns the submit.
================
*/
async function growArenaAfterEncode( gpu, retire ) {
	const particles = createParticlePresentation( gpu.device, retire );
	await particles.ready;
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
	const present = rows =>
		particles.present(
			{},
			gpu.device.createBuffer( { label: "instances", size: rows * 160 } ),
			gpu.device.createBuffer( { label: "bones", size: rows * 64 } ),
			presentation( rows )
		);
	const encoder = gpu.device.createCommandEncoder( { label: "frame" } );
	present( 1 );
	particles.encode( encoder );
	// 300 actor rows need more than the arena's initial 64 units.
	present( 300 );
	particles.encode( encoder );
	assert.ok( particles.stats().arenaBytes > 64 * 256, "the arena grew" );
	return () => gpu.device.queue.submit( [ encoder.finish() ] );
}

test("a particle arena that grows after its pass is encoded outlives that frame's submit", async () => {
	// The rule itself: destroyed at once, the recorded pass names a dead arena.
	const unguarded = createStrictGpu();
	assert.throws( await growArenaAfterEncode( unguarded, destroyNow ), DESTROYED );

	const gpu = createStrictGpu(), retirement = createRetirement();
	retirement.open();
	const submit = await growArenaAfterEncode( gpu, retirement.retire );
	submit();
	assert.deepEqual( gpu.log, [ "submit frame" ] );
	retirement.close();
	assert.deepEqual( gpu.log, [ "submit frame", "destroy particle-frame" ] );
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
		() => {
			throw Error( "No image is uploaded" );
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

test("the renderer keeps its frame open until the submit: a resized depth target dies after it", async t => {
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
	const renderer = createRenderer( { getContext: () => context } );
	try {
		renderer.setImage( { width: 16, height: 16, close() {} } );
		await new Promise( resolve => setImmediate( resolve ) );
		renderer.frame( { width: 100, height: 100 } );
		assert.equal( renderer.phase(), "running", renderer.error() );
		gpu.log.length = 0;
		// The resize replaces the depth target while this frame is preparing.
		renderer.frame( { width: 200, height: 200 } );
		assert.equal( renderer.phase(), "running", renderer.error() );
		assert.deepEqual( gpu.log, [ "submit sro-frame", "destroy surface-depth" ] );
	} finally {
		renderer.dispose();
	}
	assert.equal( gpu.live(), 0, "disposal destroys everything, the retired included" );
});
