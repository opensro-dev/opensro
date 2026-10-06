/*
===========================================================================

water-reflection.test.mjs - mirror geometry and GPU target lifetime

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { waterReflectionMatrix, waterTextureProjection } = await import(
	"../../src/engine/foundation/rendering/water-reflection.ts"
);
const { createWaterReflection } = await import( "../../src/engine/runtime/renderer/device/water-reflection.ts" );
const { createRetirement } = await import( "../../src/engine/runtime/renderer/device/retirement.ts" );
const { identity, viewProjection } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

test("capture matches native mirrored orbit and leaves the underwater camera unchanged", () => {
	for ( const fov of [ .8, 1.2 ] ) {
		for ( const aspect of [ 1, 4 / 3, 16 / 9 ] ) {
			/** @type {import("../../src/engine/contracts/scene.ts").WorldCamera} */
			const camera = {
				eye: [ 4, 23, -8 ],
				target: [ 2, 19, 6 ],
				originRegion: 257,
				fov,
				near: .1,
				far: 500
			};
			const original = viewProjection( camera, aspect );
			const captured = waterReflectionMatrix( original, 20 );
			const expected = viewProjection( { ...camera, eye: [ 4, 17, -8 ], target: [ 2, 21, 6 ] }, aspect );
			for ( let i = 0; i < 16; i++ ) assert.ok( Math.abs( captured[i] - expected[i] ) < .00001 );
			assert.deepEqual( waterReflectionMatrix( original, 20, false ), original );
			const restored = waterReflectionMatrix( captured, 20 );
			for ( let i = 0; i < 16; i++ ) assert.ok( Math.abs( restored[i] - original[i] ) < .00001 );
		}
	}
});
test("native projected texture coordinates use camera Z and reverse vertical coefficients underwater", () => {
	// Literal float words loaded by 8BA79B, 8BAA28/49 and 8BA7D0/F1.
	const words = new Uint32Array( waterTextureProjection( true ).buffer );
	assert.deepEqual( Array.from( words ), [ 0x3f266666, 0x3f4ccccd, 0x3f000000, 0xbf051eb8 ] );
	assert.deepEqual( Array.from( new Uint32Array( waterTextureProjection( false ).buffer ) ), [
		0x3f266666,
		0xbf4ccccd,
		0x3f000000,
		0x3f051eb8
	] );
});
test("reflection allocates once, retires after submission, recreates and disposes completely", () => {
	const gpu = createStrictGpu(), retirement = createRetirement();
	const water = createWaterReflection(
		/** @type {GPUDevice} */ (/** @type {unknown} */ (gpu.device)),
		"rgba8unorm",
		retirement.retire
	);
	assert.equal( gpu.live(), 2 );
	assert.equal( water.update( undefined, 0, true, 0 ), false );
	assert.equal( water.update( identity(), 0, true, 0 ), true );
	const first = water.view();
	assert.equal( gpu.live(), 4 );
	assert.equal( water.update( identity(), 0, true, 1 ), false );
	assert.equal( water.view(), first );
	retirement.open();
	const encoder = gpu.device.createCommandEncoder( { label: "water-frame" } );
	water.encode( /** @type {GPUCommandEncoder} */ (/** @type {unknown} */ (encoder)), [] );
	assert.equal( water.update( undefined, 0, true, 2 ), true );
	assert.equal( retirement.waiting(), 2 );
	assert.doesNotThrow( () => gpu.device.queue.submit( [ encoder.finish() ] ) );
	retirement.close();
	assert.equal( gpu.live(), 2 );
	water.update( identity(), 0, false, 3 );
	assert.notEqual( water.view(), first );
	water.dispose();
	assert.equal( gpu.live(), 0 );
});
