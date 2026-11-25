/*
===========================================================================

gpu-work.test.mjs - the queue-work counters and their upload funnel

The counters (foundation/rendering/gpu-work.ts) total queue submits and
geometry upload writes; the geometry upload arena is the funnel that
counts them, so a frame with snapshots costs one write and one submit,
and a direct write outside a frame is counted as itself. #4392.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE } from "../helpers/strict-gpu.mjs";
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
const { createGpuWork } = await import(
	"../../src/engine/foundation/rendering/gpu-work.ts"
);
const { createGeometryUploads } = await import(
	"../../src/engine/runtime/renderer/device/geometry-uploads.ts"
);

test("counters total submits and uploads, and a read is a snapshot", () => {
	const work = createGpuWork();
	assert.deepEqual( work.read(), { submits: 0, uploadWrites: 0, uploadBytes: 0 } );
	work.noteSubmit();
	work.noteUpload( 16 );
	work.noteSubmit();
	work.noteUpload( 8 );
	const snapshot = work.read();
	assert.deepEqual( snapshot, { submits: 2, uploadWrites: 2, uploadBytes: 24 } );
	work.noteSubmit();
	assert.equal( snapshot.submits, 2 );
	assert.equal( work.read().submits, 3 );
});

test("the upload arena counts one write and one submit per flushed frame", () => {
	const submitted = [], written = [];
	const device = {
		limits: { maxBufferSize: 1024 * 1024 },
		createBuffer: () => ({ destroy() {} }),
		createCommandEncoder: () => ({
			copyBufferToBuffer() {},
			finish: () => ({})
		}),
		queue: {
			submit: buffers => submitted.push( buffers.length ),
			writeBuffer: ( buffer, offset, data, dataOffset, size ) => written.push( size )
		}
	};
	const work = createGpuWork();
	const uploads = createGeometryUploads(
		/** @type {GPUDevice} */ (/** @type {unknown} */ (device)),
		undefined,
		work
	);
	const target = /** @type {GPUBuffer} */ (/** @type {unknown} */ ({}));
	// Two snapshots in one frame batch into the arena's single write.
	uploads.beginFrame();
	uploads.write( target, 0, new Float32Array( 4 ) );
	uploads.write( target, 16, new Float32Array( 2 ) );
	uploads.submit( /** @type {GPUCommandBuffer} */ (/** @type {unknown} */ ({})) );
	assert.deepEqual( written, [ 24 ] );
	assert.deepEqual( submitted, [ 2 ] );
	assert.deepEqual( work.read(), { submits: 1, uploadWrites: 1, uploadBytes: 24 } );
	// A submit with nothing pending is still a submit, with no write.
	uploads.submit( /** @type {GPUCommandBuffer} */ (/** @type {unknown} */ ({})) );
	assert.deepEqual( work.read(), { submits: 2, uploadWrites: 1, uploadBytes: 24 } );
	// A write outside a frame goes direct and counts as itself.
	uploads.endFrame();
	uploads.write( target, 0, new Float32Array( 1 ) );
	assert.deepEqual( written, [ 24, 4 ] );
	assert.deepEqual( work.read(), { submits: 2, uploadWrites: 2, uploadBytes: 28 } );
	uploads.dispose();
});
