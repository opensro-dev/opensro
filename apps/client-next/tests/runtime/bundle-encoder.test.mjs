/*
===========================================================================

bundle-encoder.test.mjs - a bundle encoder is single-use (#2560)

Recording forwards to the raw encoder until finish() hands out the
bundle; from there every recording call - and finish() itself - is
refused at the wrapper, in the frame's own terms, instead of deep in
the GPU backend as a validation error.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createSingleUseBundleEncoder } = await import(
	"../../src/engine/runtime/renderer/device/bundle-encoder.ts"
);

test("recording forwards until finish, and is refused after it", () => {
	const bundle = { bundle: true };
	const calls = [];
	const raw = /** @type {any} */ ({
		label: "retained-scene",
		/*
		================
		setPipeline
		================
		*/
		setPipeline( ...args ) {
			calls.push( [ "setPipeline", ...args ] );
		},
		/*
		================
		setBindGroup
		================
		*/
		setBindGroup( ...args ) {
			calls.push( [ "setBindGroup", ...args ] );
		},
		/*
		================
		setImmediates
		================
		*/
		setImmediates( ...args ) {
			calls.push( [ "setImmediates", ...args ] );
		},
		/*
		================
		setVertexBuffer
		================
		*/
		setVertexBuffer( ...args ) {
			calls.push( [ "setVertexBuffer", ...args ] );
		},
		/*
		================
		setIndexBuffer
		================
		*/
		setIndexBuffer( ...args ) {
			calls.push( [ "setIndexBuffer", ...args ] );
		},
		/*
		================
		draw
		================
		*/
		draw( ...args ) {
			calls.push( [ "draw", ...args ] );
		},
		/*
		================
		drawIndexed
		================
		*/
		drawIndexed( ...args ) {
			calls.push( [ "drawIndexed", ...args ] );
		},
		/*
		================
		finish
		================
		*/
		finish() {
			calls.push( [ "finish" ] );
			return bundle;
		}
	});
	const encoder = createSingleUseBundleEncoder( raw );
	assert.equal( encoder.label, "retained-scene" );
	encoder.label = "renamed";
	assert.equal( raw.label, "renamed" );
	const pipeline = {}, binding = {}, vertices = {}, indices = {}, immediates = new Uint32Array( [ 1 ] );
	encoder.setPipeline( pipeline );
	encoder.setBindGroup( 0, binding );
	encoder.setImmediates( 0, immediates );
	encoder.setVertexBuffer( 0, vertices );
	encoder.setIndexBuffer( indices, "uint32" );
	encoder.drawIndexed( 36, 2, 0, 0, 0 );
	encoder.draw( 6 );
	assert.equal( encoder.finish(), bundle );
	assert.deepEqual( calls, [
		[ "setPipeline", pipeline ],
		[ "setBindGroup", 0, binding ],
		[ "setImmediates", 0, immediates ],
		[ "setVertexBuffer", 0, vertices ],
		[ "setIndexBuffer", indices, "uint32" ],
		[ "drawIndexed", 36, 2, 0, 0, 0 ],
		[ "draw", 6 ],
		[ "finish" ]
	] );
	for (
		const record of [
			() => encoder.setPipeline( pipeline ),
			() => encoder.setBindGroup( 0, binding ),
			() => encoder.setImmediates( 0, new Uint32Array( [ 2 ] ) ),
			() => encoder.draw( 6 ),
			() => encoder.drawIndexed( 36 ),
			() => encoder.finish()
		]
	) {
		assert.throws( record, /used after finish/ );
	}
	assert.equal( calls.length, 8, "a refused call reached the raw encoder" );
});
