/*
===========================================================================

walking-history.test.mjs - accepted ground history retains terrain and bounds

Exercise the shared cosmetic query owner independently of authoritative
movement. A missing surface proof never becomes a chord across a floor.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
const { extendWalkingHistory, rewindWalkingHistory } = await import(
	"../../src/engine/foundation/gameplay/walking-history.ts"
);
const START = { regionId: 257, x: 0, y: 0, z: 100, angle: 0 };

test("a long accepted step samples the connected terrain without changing the accepted destination", () => {
	const to = { ...START, x: 12 }, queries = [];
	const points = extendWalkingHistory( {
		from: START,
		to,
		clip( from, candidate ) {
			queries.push( [ from.x, candidate.x ] );
			return { ...candidate, y: 12 - Math.abs( candidate.x - 6 ) * 2 };
		}
	} );
	assert.equal( points.length, 7 );
	assert.equal( defined( points[3] ).y, 12 );
	assert.deepEqual( points.at( -1 ), to );
	assert.deepEqual( queries, [ [ 0, 2 ], [ 2, 4 ], [ 4, 6 ], [ 6, 8 ], [ 8, 10 ], [ 10, 12 ] ] );
});

test("height changes never reuse the history of another floor", () => {
	const points = [ START, { ...START, x: 1 } ];
	const upper = { ...defined( points[1] ), y: 20 };
	const next = extendWalkingHistory( { points, from: upper, to: upper, clip: () => null } );
	assert.notEqual( next, points );
	assert.ok( next.every( point => point.y === 20 ) );
});

test("an extrapolated turn returns along the admitted hill samples to its actual corner", () => {
	const hill = { ...START, x: 5, y: 10 }, end = { ...START, x: 10 };
	const points = [ START, hill, end ];
	assert.deepEqual( rewindWalkingHistory( points, START ), [ START, hill, end, hill, START ] );
	assert.equal(
		rewindWalkingHistory( points, { ...hill, y: 0 } ),
		undefined,
		"the chord beneath the hill is not an edge"
	);
});

test("unknown intermediate terrain and disconnected spaces discard the unproven chord", () => {
	const to = { ...START, x: 20 };
	const unknown = extendWalkingHistory( { from: START, to, clip: () => null } );
	assert.deepEqual( unknown, [ to ] );
	const dungeon = { ...to, regionId: 0x8001 };
	assert.deepEqual( extendWalkingHistory( { from: START, to: dungeon, clip: () => null } ), [ dungeon ] );
	const from = { ...START, regionId: 0x8002 }, next = { ...from, x: 1 };
	assert.deepEqual( extendWalkingHistory( { points: [ dungeon ], from, to: next, clip: () => null } ), [
		from,
		next
	] );
	assert.equal( rewindWalkingHistory( [ dungeon ], from ), undefined );
});

test("history stays bounded and excludes actor metadata", () => {
	/** @type {readonly import("../../src/engine/contracts/gameplay.ts").Pose[]} */
	let points = [];
	for ( let x = 0; x < 400; x++ ) {
		const from = { ...START, x, name: "private", avatars: [ 1 ] };
		const to = { ...START, x: x + 1, name: "private", avatars: [ 1 ] };
		points = extendWalkingHistory( {
			points,
			from,
			to,
			clip: () => null
		} );
	}
	assert.equal( points.length, 256 );
	assert.deepEqual( Object.keys( defined( points[0] ) ), [ "regionId", "x", "y", "z", "angle" ] );
	assert.equal( defined( points.at( -1 ) ).x, 400 );
});

test("query work fits the history capacity and cannot overflow it", () => {
	let queries = 0;
	const full = extendWalkingHistory( {
		from: START,
		to: { ...START, x: 510 },
		clip( _from, to ) {
			queries++;
			return to;
		}
	} );
	assert.equal( queries, 255 );
	assert.equal( full.length, 256 );
	const outside = { ...START, x: 512 };
	assert.deepEqual(
		extendWalkingHistory( {
			from: START,
			to: outside,
			clip() {
				assert.fail( "an unrepresentable path must not start unbounded work" );
			}
		} ),
		[ outside ]
	);
});
