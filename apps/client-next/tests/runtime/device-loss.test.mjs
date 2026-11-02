/*
===========================================================================

device-loss.test.mjs - a device the browser destroys is recreated

A player's session ended with "Renderer failed: Device lost: Device was
destroyed." Only dispose() destroys a device here, and it leaves running
first: a "destroyed" loss while running was the browser's (Chrome destroys
a page's device after a GPU-process or driver reset), so the renderer must
recreate the device instead of ending the session.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createDevice } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/device.ts" ).href );
const { createDeviceRecovery } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/device-recovery.ts" ).href
);

/*
================
anything

A stand-in for every WebGPU object: each property is a callable stand-in
too, so the owner can build all of its resources.
================
*/
function anything() {
	const target = function() {};
	return new Proxy( target, {
		get( _, key ) {
			if ( key === "then" ) return undefined;
			if ( key === Symbol.toPrimitive ) return () => 1;
			if ( key === Symbol.iterator ) return function*() {};
			// Shaders compile cleanly; the promise-returning calls resolve.
			if ( key === "getCompilationInfo" ) return async () => ({ messages: [] });
			if ( typeof key === "string" && /Async$|^onSubmittedWorkDone$|^popErrorScope$/.test( key ) ) {
				return async () => anything();
			}
			return anything();
		},
		apply: () => anything(),
		construct: () => anything()
	} );
}

/*
================
installGpu

A browser whose device reports the given loss once the test resolves it.
================
*/
function installGpu( options = {} ) {
	let lose;
	const lost = new Promise( resolve => lose = resolve );
	const listeners = new Map();
	const device = new Proxy( anything(), {
		get( target, key ) {
			if ( key === "lost" ) return lost;
			if ( key === "features" ) return new Set();
			if ( key === "limits" ) return { maxTextureDimension2D: 8192, maxBufferSize: 1 << 30 };
			if ( key === "addEventListener" ) {
				return ( type, handler ) => {
					listeners.set( type, handler );
				};
			}
			if ( key === "createComputePipelineAsync" && options.rejectCompute ) {
				return async descriptor => {
					if ( options.rejectCompute( descriptor?.label ) ) {
						throw new Error(
							`GPUPipelineError: CreateComputePipelines failed with VK_ERROR_UNKNOWN - While initializing [ComputePipeline ${descriptor?.label}]`
						);
					}
					return anything();
				};
			}
			return target[key];
		}
	} );
	const adapter = {
		features: new Set(),
		info: options.info,
		limits: { maxBufferSize: 268435456, maxStorageBufferBindingSize: 134217728, maxTextureDimension2D: 8192 },
		requestDevice: async () => {
			if ( options.rejectDevice ) throw new Error( options.rejectDevice );
			return device;
		}
	};
	// The WebGPU flag namespaces the owner reads while building resources.
	for ( const name of [ "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage", "GPUMapMode", "GPUColorWrite" ] ) {
		globalThis[name] ??= new Proxy( {}, { get: () => 1 } );
	}
	globalThis.navigator ??= /** @type {any} */ ({});
	Object.defineProperty( globalThis.navigator, "gpu", {
		configurable: true,
		value: {
			requestAdapter: async () => options.noAdapter ? null : adapter,
			getPreferredCanvasFormat: () => "bgra8unorm"
		}
	} );
	return {
		lose: info => lose( info ),
		uncaptured: ( message, error = { message } ) => listeners.get( "uncapturederror" )?.( { error } )
	};
}

const settle = () => new Promise( resolve => setTimeout( resolve, 0 ) );

test("a device the browser destroys while running can be recreated", async () => {
	const gpu = installGpu();
	const device = createDevice();
	for ( let i = 0; i < 20 && device.phase() !== "running"; i++ ) await settle();
	assert.equal( device.phase(), "running", device.error() );
	gpu.lose( { reason: "destroyed", message: "Device was destroyed." } );
	await settle();
	assert.equal( device.phase(), "failed" );
	assert.equal( device.recoverable(), true, "the renderer recreates it" );
	assert.match( device.error(), /destroyed/ );
	device.dispose();
});

test("the owner's own dispose is not a failure", async () => {
	const gpu = installGpu();
	const device = createDevice();
	for ( let i = 0; i < 20 && device.phase() !== "running"; i++ ) await settle();
	device.dispose();
	gpu.lose( { reason: "destroyed", message: "Device was destroyed." } );
	await settle();
	assert.equal( device.phase(), "disposed" );
});

test("optional compute pipeline failures degrade without killing the device and report in adapterFacts", async () => {
	const gpu = installGpu( {
		info: { vendor: "qualcomm", architecture: "adreno-7xx" },
		rejectCompute: label => label === "particle-presentation" || label === "flare-visibility"
	} );
	const device = createDevice();
	for ( let i = 0; i < 20 && device.phase() !== "running"; i++ ) await settle();
	assert.equal( device.phase(), "running", device.error() ?? undefined );
	assert.equal( device.error(), null );
	// Even if a driver also emits an uncapturederror naming an optional compute pipeline,
	// the device stays running and records the degraded pipeline label.
	gpu.uncaptured(
		"GPUPipelineError: CreateComputePipelines failed with VK_ERROR_UNKNOWN - While initializing [ComputePipeline skeletal-animation]"
	);
	assert.equal( device.phase(), "running" );
	assert.deepEqual( device.adapterFacts(), {
		features: [],
		limits: {
			maxBufferSize: 268435456,
			maxStorageBufferBindingSize: 134217728,
			maxTextureDimension2D: 8192
		},
		vendor: "qualcomm",
		architecture: "adreno-7xx",
		degradedPipelines: [ "skeletal-animation", "particle-presentation", "flare-visibility" ]
	} );
	assert.equal(
		device.flares( { uniforms: new Float32Array( 28 ), textures: new Array( 8 ).fill( {} ) }, {} ),
		undefined
	);
	device.dispose();
});

test("a requestDevice rejected after a GPU reset can be tried again", async () => {
	installGpu( { rejectDevice: "OperationError: ID3D12Device::CreateCommandQueue failed" } );
	const device = createDevice( false, true, true );
	for ( let i = 0; i < 20 && device.phase() !== "failed"; i++ ) await settle();
	assert.equal( device.phase(), "failed" );
	assert.equal( device.recoverable(), true );
	assert.match( device.error(), /ID3D12Device/ );
	device.dispose();
});

test("a requestDevice rejected on the first start can be tried again too", async () => {
	installGpu( { rejectDevice: "OperationError: ID3D12Device::CreateCommandQueue failed" } );
	const device = createDevice();
	for ( let i = 0; i < 20 && device.phase() !== "failed"; i++ ) await settle();
	assert.equal( device.recoverable(), true );
	device.dispose();
});

test("no adapter means no WebGPU on a first start, and a reset under way on a replacement", async () => {
	installGpu( { noAdapter: true } );
	const first = createDevice();
	for ( let i = 0; i < 20 && first.phase() !== "failed"; i++ ) await settle();
	assert.equal( first.recoverable(), false );
	first.dispose();
	const replacement = createDevice( false, true, true );
	for ( let i = 0; i < 20 && replacement.phase() !== "failed"; i++ ) await settle();
	assert.equal( replacement.recoverable(), true );
	replacement.dispose();
});

test("a device error after creation stays permanent", async () => {
	const gpu = installGpu();
	const device = createDevice();
	for ( let i = 0; i < 20 && device.phase() !== "running"; i++ ) await settle();
	gpu.uncaptured( "createView: texture is not valid" );
	await settle();
	assert.equal( device.phase(), "failed" );
	assert.equal( device.recoverable(), false );
	device.dispose();
});

test("running out of GPU memory can be cured by a new device", async () => {
	class OutOfMemory extends Error {}
	globalThis.GPUOutOfMemoryError = OutOfMemory;
	try {
		const gpu = installGpu();
		const device = createDevice();
		for ( let i = 0; i < 20 && device.phase() !== "running"; i++ ) await settle();
		gpu.uncaptured( "Out of memory", new OutOfMemory( "Out of memory" ) );
		await settle();
		assert.equal( device.phase(), "failed" );
		assert.equal( device.recoverable(), true );
		device.dispose();
	} finally {
		delete globalThis.GPUOutOfMemoryError;
	}
});

test("recovery waits longer each time and gives up in under two minutes", () => {
	const recovery = createDeviceRecovery();
	let now = 0, tries = 0;
	while ( recovery.pending() && now < 600000 ) {
		if ( recovery.due( now ) ) {
			recovery.began();
			tries++;
		} else now += 100;
	}
	assert.equal( tries, 8 );
	assert.ok( now < 120000, `spent ${now} ms` );
	assert.equal( recovery.due( now + 600000 ), false );
});

test("the first attempt is immediate and the second waits", () => {
	const recovery = createDeviceRecovery();
	assert.equal( recovery.due( 5000 ), true );
	recovery.began();
	assert.equal( recovery.due( 5001 ), false );
	assert.equal( recovery.due( 6001 ), true );
});

test("a device that stays up earns a fresh budget, a brief one does not", () => {
	const recovery = createDeviceRecovery();
	const spend = () => {
		let now = 0;
		while ( recovery.pending() ) {
			if ( recovery.due( now ) ) recovery.began();
			now += 1000;
		}
		return now;
	};
	const at = spend();
	assert.equal( recovery.pending(), false );
	recovery.running( at );
	recovery.running( at + 59999 );
	assert.equal( recovery.pending(), false );
	recovery.running( at + 60000 );
	assert.equal( recovery.pending(), true );
});
