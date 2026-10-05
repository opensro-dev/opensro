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
function installGpu() {
	let lose;
	const lost = new Promise( resolve => lose = resolve );
	const device = new Proxy( anything(), {
		get( target, key ) {
			if ( key === "lost" ) return lost;
			if ( key === "features" ) return new Set();
			if ( key === "limits" ) return { maxTextureDimension2D: 8192, maxBufferSize: 1 << 30 };
			if ( key === "addEventListener" ) return () => {};
			return target[key];
		}
	} );
	const adapter = { features: new Set(), requestDevice: async () => device };
	// The WebGPU flag namespaces the owner reads while building resources.
	for ( const name of [ "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage", "GPUMapMode", "GPUColorWrite" ] ) {
		globalThis[name] ??= new Proxy( {}, { get: () => 1 } );
	}
	globalThis.navigator ??= /** @type {any} */ ({});
	Object.defineProperty( globalThis.navigator, "gpu", {
		configurable: true,
		value: { requestAdapter: async () => adapter, getPreferredCanvasFormat: () => "bgra8unorm" }
	} );
	return { lose: info => lose( info ) };
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
