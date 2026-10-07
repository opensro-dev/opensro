/*
===========================================================================

cloth-hold.test.mjs - skipping held cloth frames reproduces every frame

The world renderer skips a cloth stream's update and upload while it holds
(no step due, option unchanged, palette unchanged). A stream updated only
when it does not hold must keep the same vertices, frame for frame, as one
updated every frame, through irregular frame times, stalls, option flips
and gusting wind.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

const FRAME_SECONDS = [ .002, .0017, .004, .016, .033, .0021, .25, .0019, .049, .001 ];

/*
================
clothPrimitive

A three-vertex strip with one pin, long enough to swing.
================
*/
/** @param {boolean} skinned
 * @param {readonly number[]} pins
 * @param {readonly number[]} mobility
 * @returns {{ geometry: import("../../src/engine/contracts/geometry.ts").Geometry;
 *   cloth: import("../../src/engine/foundation/animation/cloth.ts").ClothData; }} */
function clothPrimitive( skinned, pins = [ 1, 0, 0 ], mobility = [ 0, 1, 1 ] ) {
	const positions = new Float32Array( [ 0, 0, 0, 1, 0, 0, 2, 0, 0 ] ),
		indices = new Uint32Array( [ 0, 1, 2 ] ),
		transform = identity();
	const geometry = skinned ?
		{
			positions,
			indices,
			transform,
			joints: new Uint32Array( 12 ),
			weights: new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] )
		} :
		{ positions, indices, transform };
	/** @type {import("../../src/engine/foundation/animation/cloth.ts").ClothData} */
	const cloth = {
		mobility: [ ...mobility ],
		pins: [ ...pins ],
		constraints: [ [ 0, 1, 1 ], [ 1, 2, 1 ] ],
		order: [ 0, 1 ],
		force: null,
		gravity: 1,
		gravityMobility: .5,
		windMobility: .3,
		damping: .98,
		windPeriod: 3
	};
	return { geometry, cloth };
}

/*
================
counter

A deterministic random source; a gust fires every third draw, so any
change to the number or order of steps changes the vertices.
================
*/
function counter() {
	let n = 0;
	return () => n++;
}

// Pins other than 1 integrate like free vertices, and a pin 1 with mobility
// is pulled by constraints: a step moves them and the next frame's
// re-anchoring restores them, so that frame must not be held.
const PIN_CASES = [
	{ name: "fixed pin", pins: [ 1, 0, 0 ], mobility: [ 0, 1, 1 ] },
	{ name: "pin 2", pins: [ 2, 0, 0 ], mobility: [ .5, 1, 1 ] },
	{ name: "moving pin 1", pins: [ 1, 0, 0 ], mobility: [ .4, 1, 1 ] }
];

for ( const skinned of [ false, true ] ) {
	for ( const { name, pins, mobility } of PIN_CASES ) {
		test(`held cloth frames can be skipped without changing any frame (${skinned ? "skinned" : "unskinned"}, ${name})`, () => {
			const calls = [ 0, 0 ];
			const every = createClothVertices( clothPrimitive( skinned, pins, mobility ), () => calls[0]++ );
			const skipping = createClothVertices( clothPrimitive( skinned, pins, mobility ), () => calls[1]++ );
			const palette = identity(), motion = { direction: [ .3, 0, 1 ], speed: 0 };
			palette[13] = 2;
			let seconds = 0, shown = null, skipped = 0;
			for ( let frame = 0; frame < 600; frame++ ) {
				seconds += FRAME_SECONDS[frame % FRAME_SECONDS.length];
				const enabled = frame % 97 < 80;
				const expected = every.update( palette, seconds, enabled, motion ).slice();
				if ( shown && skipping.hold( seconds, enabled ) ) {
					skipped++;
				} else {
					shown = skipping.update( palette, seconds, enabled, motion ).slice();
				}
				assert.deepEqual(
					new Uint8Array( shown.buffer ),
					new Uint8Array( expected.buffer ),
					`frame ${frame} at ${seconds.toFixed( 4 )} s`
				);
				assert.equal( calls[1], calls[0], "shared RNG consumption must match on every frame" );
			}
			assert.ok( skipped > 200, `only ${skipped} of 600 frames were held` );
		});
	}
}

test("a cloth stream is never held before its first update or across an option change", () => {
	const cloth = createClothVertices( clothPrimitive( false ), counter() );
	const motion = { direction: [ 0, 0, 1 ], speed: 0 };
	assert.equal( cloth.hold( 0, true ), false );
	cloth.update( identity(), 0, true, motion );
	assert.equal( cloth.hold( .01, true ), true );
	assert.equal( cloth.hold( .01, false ), false );
	assert.equal( cloth.hold( .05, true ), false );
});

test("unchanged pins hold immediately after a solver step; moved pins publish their re-anchoring", () => {
	for ( const { name, pins, mobility } of PIN_CASES ) {
		const every = createClothVertices( clothPrimitive( false, pins, mobility ), counter() );
		const skipping = createClothVertices( clothPrimitive( false, pins, mobility ), counter() );
		const palette = identity(), motion = { direction: [ .3, 0, 1 ], speed: 0 };
		let shown;
		for ( const seconds of [ 0, .05 ] ) {
			every.update( palette, seconds, true, motion );
			shown = skipping.update( palette, seconds, true, motion ).slice();
		}
		const held = skipping.hold( .052, true );
		assert.equal( held, name === "fixed pin", name );
		if ( !held ) shown = skipping.update( palette, .052, true, motion ).slice();
		const expected = every.update( palette, .052, true, motion );
		assert.ok( shown );
		assert.deepEqual( new Uint8Array( shown.buffer ), new Uint8Array( expected.buffer ), name );
		assert.equal( skipping.hold( .099, true ), true, name );
		assert.equal( skipping.hold( .1, true ), false, "the next simulation step stays due" );
	}
});

test("moving skeletal pins publish on every substep while free vertices retain their solver positions", () => {
	let randomCalls = 0;
	const cloth = createClothVertices( clothPrimitive( true ), () => ++randomCalls );
	const palette = identity(), motion = { direction: [ 0, 0, 1 ], speed: 0 };
	for ( let frame = 0; frame < 25; frame++ ) {
		palette[12] = frame;
		const vertices = cloth.update( palette, frame * .002, true, motion );
		assert.equal( vertices[0], frame, "pins must not wait for the 50 ms solver step" );
		assert.equal( vertices[14], 1 );
		assert.equal( vertices[28], 2 );
		assert.equal( randomCalls, 0 );
	}
	const stepped = cloth.update( palette, .05, true, motion );
	assert.notEqual( stepped[14], 1, "the solver step must publish free-vertex changes" );
	palette[12] = 30;
	const reset = cloth.update( palette, .052, false, motion );
	assert.equal( reset[0], 30 );
	assert.equal( reset[14], 31 );
	assert.equal( reset[28], 32 );
});
