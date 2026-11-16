/*
===========================================================================

world-vector.test.mjs - walking-chain.ts writeWorldVector

Two poses of one region take a fast path that skips the coordinate-space
check and the sector offset. It must write the general path's own bits:
sameCoordinateSpace, then writeWorldDelta( b, a ), a difference of negative
zero included.

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { writeWorldVector } = await import( "../../src/engine/foundation/gameplay/walking-chain.ts" );
const { DUNGEON_REGION_BIT, REGION_SIZE, sameCoordinateSpace, writeWorldDelta } = await import(
	"../../src/engine/foundation/world/world-space.ts"
);

/** The general path, as writeWorldVector ran before the fast path. */
function general( a, b, out ) {
	if ( !sameCoordinateSpace( a.regionId, b.regionId ) ) return false;
	/** @type {[number, number]} */
	const plane = [ NaN, NaN ];
	writeWorldDelta( b, a, plane );
	out[0] = plane[0];
	out[1] = a.y - b.y;
	out[2] = plane[1];
	return true;
}

function same( a, b ) {
	/** @type {[number, number, number]} */
	const fast = [ NaN, NaN, NaN ], slow = [ NaN, NaN, NaN ];
	const answer = writeWorldVector( a, b, fast );
	assert.equal( answer, general( a, b, slow ), JSON.stringify( { a, b } ) );
	if ( !answer ) return;
	for ( let axis = 0; axis < 3; axis++ ) {
		assert.ok(
			Object.is( fast[axis], slow[axis] ),
			`axis ${axis}: ${fast[axis]} != ${slow[axis]} ${JSON.stringify( { a, b } )}`
		);
	}
}

test("writeWorldVector writes the general path's bits inside one region, outdoors and in a dungeon", () => {
	let seed = 11;
	const random = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
	const coordinate = () => {
		const kind = random();
		if ( kind < .1 ) return 0;
		if ( kind < .2 ) return -0;
		if ( kind < .6 ) return Math.fround( random() * REGION_SIZE );
		return (random() - .5) * 4 * REGION_SIZE;
	};
	const pose = ( regionId ) => ({ regionId, x: coordinate(), y: coordinate(), z: coordinate(), angle: 0 });
	for ( let i = 0; i < 100000; i++ ) {
		const regionId = (random() < .25 ? DUNGEON_REGION_BIT : 0) | Math.floor( random() * 0x8000 );
		same( pose( regionId ), pose( regionId ) );
	}
});

test("writeWorldVector reads a difference of negative zero as zero, as the general path did", () => {
	/** @type {[number, number, number]} */
	const out = [ NaN, NaN, NaN ];
	assert.equal(
		writeWorldVector( { regionId: 0x64a8, x: -0, y: 0, z: -0, angle: 0 }, {
			regionId: 0x64a8,
			x: 0,
			y: 0,
			z: 0,
			angle: 0
		}, out ),
		true
	);
	assert.ok( Object.is( out[0], 0 ) && Object.is( out[2], 0 ) );
	same( { regionId: 0x64a8, x: -0, y: -0, z: -0, angle: 0 }, { regionId: 0x64a8, x: 0, y: 0, z: 0, angle: 0 } );
});

test("writeWorldVector across regions still takes the general path", () => {
	// Neighbouring outdoor regions share a space; a dungeon and the field do not.
	same( { regionId: 0x64a8, x: 10, y: 1, z: 20, angle: 0 }, { regionId: 0x64a9, x: 1900, y: 2, z: 5, angle: 0 } );
	same( { regionId: 0x64a8, x: 10, y: 1, z: 20, angle: 0 }, { regionId: 0x6528, x: 3, y: 2, z: 1910, angle: 0 } );
	same( { regionId: DUNGEON_REGION_BIT | 1, x: 10, y: 1, z: 20, angle: 0 }, {
		regionId: 0x64a8,
		x: 3,
		y: 2,
		z: 1,
		angle: 0
	} );
	same( { regionId: DUNGEON_REGION_BIT | 1, x: 10, y: 1, z: 20, angle: 0 }, {
		regionId: DUNGEON_REGION_BIT | 2,
		x: 3,
		y: 2,
		z: 1
	} );
});
