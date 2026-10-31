/*
===========================================================================

renderer-diagnostics.test.mjs - device selections survive device replacement

The renderer replaces a failed device with a new one. The diagnostic
selection it was built with, and the Experimental video preference it
received, must reach every replacement.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";

const built = await build( {
	entryPoints: [ "src/engine/runtime/renderer/renderer.ts" ],
	bundle: true,
	platform: "node",
	format: "esm",
	write: false,
	plugins: [ {
		name: "device-observation",
		setup( b ) {
			b.onLoad( { filter: /renderer[\\/]device[\\/]device\.ts$/ }, () => ({
				contents: "export const createDevice=(enabled)=>globalThis.__diagnosticDevice(enabled);",
				loader: "ts"
			}) );
		}
	} ]
} );
const { createRenderer } = await import(
	"data:text/javascript;base64," + Buffer.from( built.outputFiles[0].contents ).toString( "base64" )
);

const VIDEO = Object.freeze( {
	renderScale: 100,
	postProcessing: true,
	anisotropicFiltering: true,
	heightFog: false,
	dynamicSun: false,
	floatBloom: false,
	hdrToneMap: false,
	sunShadow: false,
	perPixelLighting: false,
	sunRays: false
} );

/*
================
diagnosticDevices

Install a fake device factory that records each device's flag, its
replacement and the Experimental video values it receives. features(i)
gives the i-th device's texture features (none, at once, by default).
================
*/
/**
 * @param {import("node:test").TestContext} t
 * @param {( index: number ) => Promise<readonly string[]>} [features]
 */
function diagnosticDevices( t, features = () => Promise.resolve( [] ) ) {
	const original = Object.getOwnPropertyDescriptor( globalThis, "__diagnosticDevice" );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "__diagnosticDevice", original );
		else delete globalThis.__diagnosticDevice;
	} );
	const devices = [];
	globalThis.__diagnosticDevice = flag => {
		const row = { flag, phase: "starting", disposed: false, video: /** @type {unknown[]} */ ([]) };
		const rowFeatures = features( devices.length );
		devices.push( row );
		return {
			phase: () => row.phase,
			recoverable: () => true,
			error: () => null,
			textureOptions() {},
			experimentalVideo( value ) {
				row.video.push( value );
			},
			gpuTiming: () => null,
			textureFeatures: () => rowFeatures,
			geometry: () => null,
			images: () => null,
			dispose() {
				row.disposed = true;
			}
		};
	};
	return devices;
}

test("diagnostic selection reaches initial and replacement device owners", t => {
	for ( const enabled of [ false, true ] ) {
		const devices = diagnosticDevices( t );
		const renderer = createRenderer( {}, undefined, undefined, { gpuTiming: enabled } );
		assert.equal( renderer.gpuTiming().enabled, enabled );
		for ( let i = 0; i < 3; i++ ) {
			devices[i].phase = "failed";
			renderer.frame( { width: 1024, height: 768 } );
			assert.equal( devices[i].disposed, true );
			assert.equal( devices.length, i + 2 );
		}
		assert.deepEqual( devices.map( d => d.flag ), [ enabled, enabled, enabled, enabled ] );
		renderer.dispose();
		assert.ok( devices.every( d => d.disposed ) );
	}
});

test("texture features come from the replacement of a device lost before it learned them", async t => {
	/** @type {((features: readonly string[]) => void)[]} */
	const known = [];
	const devices = diagnosticDevices(
		t,
		() => /** @type {Promise<readonly string[]>} */ (new Promise( resolve => known.push( resolve ) ))
	);
	const renderer = createRenderer( {}, undefined, undefined, {} );
	devices[0].phase = "failed";
	renderer.frame( { width: 1024, height: 768 } );
	assert.equal( devices.length, 2 );
	known[1]( [ "texture-compression-astc" ] );
	assert.deepEqual( await renderer.textureFeatures(), [ "texture-compression-astc" ] );
	renderer.dispose();
});

test("texture features are empty once no device will learn them, so texture reads never wait", async t => {
	const devices = diagnosticDevices( t, () => new Promise( () => {} ) );
	const renderer = createRenderer( {}, undefined, undefined, {} );
	for ( let i = 0; i < 4; i++ ) {
		devices[i].phase = "failed";
		renderer.frame( { width: 1024, height: 768 } );
	}
	assert.equal( devices.length, 4, "three recoveries, then the renderer gives up" );
	assert.deepEqual( await renderer.textureFeatures(), [] );
	renderer.dispose();
});

test("the Experimental video preference reaches the device and every replacement", t => {
	const devices = diagnosticDevices( t );
	const renderer = createRenderer( {}, undefined, undefined, {} );
	renderer.experimentalVideo( VIDEO );
	assert.deepEqual( devices[0].video, [ VIDEO ] );
	devices[0].phase = "failed";
	renderer.frame( { width: 1024, height: 768 } );
	assert.equal( devices.length, 2 );
	assert.deepEqual( devices[1].video, [ VIDEO ] );
	renderer.dispose();
});
