/*
===========================================================================

native-client-ground-reference.test.mjs - original client navigation arithmetic

The oracle executes the complete navigation tick with a controlled world
boundary. Geometry response and lifecycle remain tested by runtime fixtures.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
const { clientWalkingStep, clientPlanarDistance, clientWalkingDirection } = await import(
	"../../src/engine/foundation/gameplay/native-movement.ts"
);
const reference = JSON.parse(
	readFileSync( new URL( "../fixtures/native/native-client-ground-reference.json", import.meta.url ), "utf8" )
);
/*
================
bits
================
*/
function bits( value ) {
	const data = new DataView( new ArrayBuffer( 4 ) );
	data.setFloat32( 0, value, true );
	return data.getUint32( 0, true );
}

test("client walking arithmetic agrees bit-for-bit with complete native navigation ticks", () => {
	for ( const row of reference.cases ) {
		const speed = row.overrideSpeed || Math.fround( row.speed * row.scale );
		const remaining = row.waypoint ?
			clientPlanarDistance( [ row.goal[0] - row.start[0], row.goal[2] - row.start[2] ] ) :
			Infinity;
		const result = clientWalkingStep(
			speed,
			row.elapsedSeconds,
			[ row.direction[0], row.direction[2] ],
			remaining
		);
		const position = row.initiallyActive && !(row.status & 0x10000000) ?
			[
				Math.fround( row.start[0] + result.step[0] ),
				row.start[1],
				Math.fround( row.start[2] + result.step[1] )
			] :
			row.start;
		assert.deepEqual( position.map( bits ), row.positionBits, row.name );
		assert.equal( row.initiallyActive && !result.arrived && !row.status, row.active, row.name );
	}
});

test("waypoint direction matches native normalization, yaw and stored vector", () => {
	for ( const row of reference.directions ) {
		const direction = clientWalkingDirection( row.delta );
		assert.deepEqual(
			direction.map( bits ),
			[ row.directionBits[0], row.directionBits[2] ],
			JSON.stringify( row.delta )
		);
	}
});
