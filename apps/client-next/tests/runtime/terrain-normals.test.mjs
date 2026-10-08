/*
===========================================================================

terrain-normals.test.mjs - heightfield normals for the terrain relief stage

Pins the shared per-block normal table the worker now writes into every
terrain pass: flat ground stays (0,1,0), slopes tilt by their gradient,
borders go one-sided, and the values stay unit length. The retail look
ignores them (terrain is NOLIGHT); only the terrain-relief stage reads
them, so these are data contracts, not look pins.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { terrainBlockNormals } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/rendering/terrain-associations.ts" ) ).href
);

const SAMPLES = 17;

function flatBlock( height = 10 ) {
	return Array.from( { length: SAMPLES * SAMPLES }, () => height );
}

test("flat blocks keep the up normal and every entry is unit length", () => {
	const normals = terrainBlockNormals( flatBlock() );
	assert.equal( normals.length, SAMPLES * SAMPLES * 3 );
	// +0 folds the -0 a zero gradient produces.
	for ( let i = 0; i < normals.length; i += 3 ) {
		assert.deepEqual( [ ...normals.slice( i, i + 3 ) ].map( v => v + 0 ), [ 0, 1, 0 ] );
	}
});

test("a constant ramp tilts every interior normal by its gradient", () => {
	// 20 units per cell: a 1-in-2 slope along X tilts the normal by
	// atan(0.5) from vertical, toward -X (downhill faces the rise).
	const heights = flatBlock();
	for ( let z = 0; z < SAMPLES; z++ ) {
		for ( let x = 0; x < SAMPLES; x++ ) heights[z * SAMPLES + x] = x * 10;
	}
	const normals = terrainBlockNormals( heights );
	const expected = [ -0.5, 1, 0 ].map( v => v / Math.hypot( 0.5, 1, 0 ) );
	for ( let z = 1; z < SAMPLES - 1; z++ ) {
		const at = (z * SAMPLES + 8) * 3;
		assert.ok( [ ...normals.slice( at, at + 3 ) ].every( ( v, i ) => Math.abs( v - expected[i] ) < 1e-6 ) );
	}
	// Border columns use one-sided differences over the same 40-unit span,
	// so a linear ramp reads identically at the edge.
	const edge = (8 * SAMPLES) * 3;
	assert.ok( [ ...normals.slice( edge, edge + 3 ) ].every( ( v, i ) => Math.abs( v - expected[i] ) < 1e-6 ) );
});

test("a peak diverges its slopes and keeps unit length everywhere", () => {
	const heights = flatBlock();
	for ( let z = 0; z < SAMPLES; z++ ) {
		for ( let x = 0; x < SAMPLES; x++ ) {
			heights[z * SAMPLES + x] = 100 - 5 * (Math.abs( x - 8 ) + Math.abs( z - 8 ));
		}
	}
	const normals = terrainBlockNormals( heights );
	const center = (8 * SAMPLES + 8) * 3;
	// The peak itself is flat again (symmetric differences cancel).
	assert.deepEqual(
		[ ...normals.slice( center, center + 3 ) ].map( v => Math.round( v * 1e6 ) / 1e6 + 0 ),
		[ 0, 1, 0 ]
	);
	const slope = (8 * SAMPLES + 6) * 3;
	const tilted = [ ...normals.slice( slope, slope + 3 ) ];
	// The rise is toward +X, so the normal leans downhill (-X) and stays steep.
	assert.ok( tilted[0] < -0.1 && tilted[1] > 0.9 && tilted[2] + 0 === 0 );
	for ( let i = 0; i < normals.length; i += 3 ) {
		assert.ok( Math.abs( Math.hypot( normals[i], normals[i + 1], normals[i + 2] ) - 1 ) < 1e-6 );
	}
});
