/*
===========================================================================

water-reflection.test.mjs - mirror geometry and GPU target lifetime

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { waterReflectionMatrix } = await import( "../../src/engine/foundation/rendering/water-reflection.ts" );
const { createWaterReflection } = await import( "../../src/engine/runtime/renderer/device/water-reflection.ts" );
const { createRetirement } = await import( "../../src/engine/runtime/renderer/device/retirement.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

test("reflection preserves points on the plane and mirrors twice to the original view", () => {
	const view = new Float32Array( [ 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17 ] );
	const mirror = waterReflectionMatrix( view, 20 );
	assert.deepEqual( waterReflectionMatrix( mirror, 20 ), view );
	for ( let row = 0; row < 4; row++ ) {
		assert.equal(
			mirror[row] + mirror[row + 4] * 20 + mirror[row + 8] + mirror[row + 12],
			view[row] + view[row + 4] * 20 + view[row + 8] + view[row + 12]
		);
	}
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
