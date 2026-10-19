/*
===========================================================================

dungeon-object-matrix.test.mjs - the world matrix of a DOF block object

The expected rotation rows come from running the instruction sequence of
SRO_Client.exe 0xAD05F0 (the rotation builder CRTDunBlock's object creator
0xAD4DC0 calls) on each angle triple, not from a formula. Scale joins as
scale x rotation (0x926EA4, a matrix multiply) and the translation is the
record's position.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { dungeonObjectMatrix } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

// [ rotation vector, the native 3x3 row by row ]
/** @type {[ [ number, number, number ], number[] ][]} */
const NATIVE_ROTATIONS = [
	[ [ 0.3, 0.5, 0.7 ], [
		0.6712122,
		-0.5653542,
		-0.4794255,
		0.5070819,
		0.8219544,
		-0.2593434,
		0.5406868,
		-0.0690336,
		0.8383867
	] ],
	[ [ 1.0, -0.4, 2.2 ], [
		-0.5420455,
		-0.7446744,
		0.3894183,
		0.2439899,
		-0.5829,
		-0.7750461,
		0.804149,
		-0.3250961,
		0.4976513
	] ],
	[ [ 2.1, 0.9, -1.3 ], [
		0.16628,
		0.5989574,
		-0.7833269,
		0.3055724,
		-0.7865799,
		-0.5365796,
		-0.9375375,
		-0.1501407,
		-0.3138174
	] ],
	[ [ 6.283, 0.908, 1.222 ], [
		0.2102972,
		-0.5782717,
		-0.7882746,
		0.9398347,
		0.3416296,
		0.000114,
		0.269232,
		-0.7408718,
		0.6153235
	] ],
	[ [ 0, 4.71, 0 ], [ -0.0023889, 0, 0.9999971, 0, 1, 0, -0.9999971, 0, -0.0023889 ] ]
];
const rows = m => [ 0, 1, 2, 4, 5, 6, 8, 9, 10 ].map( i => m[i] );

test("the rotation rows match the native rotation builder", () => {
	for ( const [angles, expected] of NATIVE_ROTATIONS ) {
		const got = rows( dungeonObjectMatrix( 0, 0, 0, angles, [ 1, 1, 1 ] ) );
		for ( let i = 0; i < 9; i++ ) {
			assert.ok( Math.abs( got[i] - expected[i] ) < 2e-6, `${angles}: element ${i} ${got[i]} vs ${expected[i]}` );
		}
	}
});

test("no rotation is the identity, scaled per axis, with the position as translation", () => {
	const m = dungeonObjectMatrix( 10, 20, 30, [ 0, 0, 0 ], [ 2, 3, 4 ] );
	// "+ 0" folds the -0 a product of zeros can leave.
	assert.deepEqual( [ ...m ].map( v => v + 0 ), [ 2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 4, 0, 10, 20, 30, 1 ] );
});

test("scale applies before rotation: each rotation row is scaled by its axis", () => {
	const [angles, expected] = NATIVE_ROTATIONS[0];
	const m = dungeonObjectMatrix( 0, 0, 0, angles, [ 2, 3, 4 ] );
	for ( let row = 0; row < 3; row++ ) {
		for ( let col = 0; col < 3; col++ ) {
			const want = expected[row * 3 + col] * [ 2, 3, 4 ][row];
			assert.ok( Math.abs( m[row * 4 + col] - want ) < 2e-6, `row ${row} column ${col}` );
		}
	}
	assert.deepEqual( [ m[3], m[7], m[11], m[15] ], [ 0, 0, 0, 1 ] );
});

test("the rotation part is orthonormal for every angle triple", () => {
	for ( const [angles] of NATIVE_ROTATIONS ) {
		const m = dungeonObjectMatrix( 0, 0, 0, angles, [ 1, 1, 1 ] );
		for ( let a = 0; a < 3; a++ ) {
			for ( let b = 0; b < 3; b++ ) {
				let dot = 0;
				for ( let k = 0; k < 3; k++ ) dot += m[a * 4 + k] * m[b * 4 + k];
				assert.ok( Math.abs( dot - (a === b ? 1 : 0) ) < 1e-6, `${angles} rows ${a},${b}` );
			}
		}
	}
});
