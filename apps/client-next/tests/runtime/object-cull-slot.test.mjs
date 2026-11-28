/*
===========================================================================

object-cull-slot.test.mjs - a refreshed culled draw keeps the pass's count

The GPU object cull writes each slot's instance count (argument word 1).
A new slot starts at 0, drawing nothing until the pass decides; a draw
whose counts change later (updateInstances, updateIndices) rewrites the
other words only, so a frame that draws it before the next pass keeps
the count the last pass chose.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE } from "../helpers/strict-gpu.mjs";

globalThis.GPUBufferUsage ??= /** @type {any} */ (GPU_BUFFER_USAGE);
const { createObjectCull } = await import( "../../src/engine/runtime/renderer/device/object-cull.ts" );
const { DeviceDraw } = await import( "../../src/engine/runtime/renderer/device/device-draw.ts" );

// Argument word 1 of slot 0: bytes 4..8 of the arguments buffer.
const COUNT_FIRST_BYTE = 4, COUNT_END_BYTE = 8;

/*
================
recordingDevice

A device whose queue records each write's buffer label and byte range.
================
*/
function recordingDevice() {
	/** @type {{ label: string; from: number; to: number; }[]} */
	const writes = [];
	const device = /** @type {any} */ ({
		createBuffer: ( /** @type {any} */ descriptor ) => ({ label: descriptor.label, destroy() {} }),
		createShaderModule: () => ({}),
		createComputePipelineAsync: async () => ({}),
		queue: {
			writeBuffer(
				/** @type {any} */ buffer,
				/** @type {number} */ offset,
				/** @type {any} */ _data,
				/** @type {number} */ _at,
				/** @type {number} */ size
			) {
				writes.push( { label: buffer.label, from: offset, to: offset + size } );
			}
		}
	});
	return { device, writes };
}

test("only a new slot writes the instance count; a refresh writes around it", async () => {
	const { device, writes } = recordingDevice();
	const cull = createObjectCull( device, () => {} );
	await cull.ready;
	const draw = new DeviceDraw(
		/** @type {any} */ ({ pipeline: {}, vertices: {}, indices: {}, count: 36 }),
		/** @type {any} */ ({}),
		1,
		1
	);
	const touchesCount = () =>
		writes.some( w => w.label === "object-cull-arguments" && w.from < COUNT_END_BYTE && w.to > COUNT_FIRST_BYTE );
	cull.adopt( draw, [ 0, 0, 0, 1 ] );
	assert.ok( touchesCount(), "a new slot starts at count 0" );
	writes.length = 0;
	cull.refresh( draw );
	assert.ok( writes.some( w => w.label === "object-cull-arguments" ), "the refresh writes the other words" );
	assert.ok( !touchesCount(), "the refresh leaves the pass's instance count" );
});
