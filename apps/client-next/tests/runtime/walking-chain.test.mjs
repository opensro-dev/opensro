/*
===========================================================================

walking-chain.test.mjs - a measured chain answers as the full scan did

walking-chain.ts measures a walking path once and skips the blocks of edges
a pose cannot lie on. These tests keep the full scans it replaced (the
pose-presentation.ts functions as they were) and require every answer to be
the same value, bit for bit: on paths that cross sector borders, climb, stop
on one spot, rise straight up, fold back over themselves, live in a dungeon
or break between coordinate spaces.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const chainModule = await import( "../../src/engine/foundation/gameplay/walking-chain.ts" );
const { measureWalkingChain, locateOnWalkingChain, nearestOnWalkingChain, walkingChainPose, writeWorldVector } =
	chainModule;
const { interpolateMovement } = await import( "../../src/engine/foundation/gameplay/native-movement.ts" );
const { hypot2, hypot3 } = await import( "../../src/engine/foundation/math/hypot.ts" );
const { normalizeRegionXZ } = await import( "../../src/engine/foundation/world/world-space.ts" );

const MIN_CORRECTION_DISTANCE = 0.01;
const DUNGEON = 0x8006;

// ============================================================================
// The full scans walking-chain.ts replaced, as pose-presentation.ts had them.

/** @type {[number, number, number]} */
const spanVector = [ 0, 0, 0 ];
/** @type {[number, number, number]} */
const pointVector = [ 0, 0, 0 ];

/*
================
scanLocation
================
*/
function scanLocation( pose, path, latest = false ) {
	let distance = 0;
	let hasBest = false, bestEdge = 0, bestFraction = 0, bestDistance = 0, bestError = 0;
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1], to = path[i];
		if ( !writeWorldVector( to, from, spanVector ) || !writeWorldVector( pose, from, pointVector ) ) return null;
		const planar2 = spanVector[0] ** 2 + spanVector[2] ** 2;
		const fraction = planar2 ?
			Math.max( 0, Math.min( 1, (pointVector[0] * spanVector[0] + pointVector[2] * spanVector[2]) / planar2 ) ) :
			spanVector[1] ?
			Math.max( 0, Math.min( 1, pointVector[1] / spanVector[1] ) ) :
			0;
		const length = hypot3( spanVector[0], spanVector[1], spanVector[2] );
		const miss = hypot2( pointVector[0] - spanVector[0] * fraction, pointVector[2] - spanVector[2] * fraction );
		if ( miss <= MIN_CORRECTION_DISTANCE ) {
			const error = miss + Math.abs( pointVector[1] - spanVector[1] * fraction );
			if (
				!hasBest ||
				(latest && error <= MIN_CORRECTION_DISTANCE && bestError <= MIN_CORRECTION_DISTANCE) ||
				error < bestError
			) {
				hasBest = true;
				bestEdge = i;
				bestFraction = fraction;
				bestDistance = distance + length * fraction;
				bestError = error;
			}
		}
		distance += length;
	}
	return hasBest ?
		{
			edge: bestEdge,
			fraction: bestFraction,
			distance: bestDistance,
			error: bestError,
			pose: interpolateMovement( path[bestEdge - 1], path[bestEdge], bestFraction )
		} :
		null;
}

/*
================
scanNearest
================
*/
function scanNearest( pose, path ) {
	let hasBest = false, bestMiss = 0, bestEdge = 0, bestFraction = 0;
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1], to = path[i];
		if ( !writeWorldVector( to, from, spanVector ) || !writeWorldVector( pose, from, pointVector ) ) return null;
		const planar2 = spanVector[0] ** 2 + spanVector[2] ** 2;
		const fraction = planar2 ?
			Math.max( 0, Math.min( 1, (pointVector[0] * spanVector[0] + pointVector[2] * spanVector[2]) / planar2 ) ) :
			0;
		const miss = hypot2( pointVector[0] - spanVector[0] * fraction, pointVector[2] - spanVector[2] * fraction );
		if ( !hasBest || miss < bestMiss ) {
			hasBest = true;
			bestMiss = miss;
			bestEdge = i;
			bestFraction = fraction;
		}
	}
	return hasBest ? interpolateMovement( path[bestEdge - 1], path[bestEdge], bestFraction ) : null;
}

/*
================
scanPose
================
*/
function scanPose( path, distance ) {
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1], to = path[i];
		if ( !writeWorldVector( to, from, spanVector ) ) throw Error( "Invalid walking path edge" );
		const length = hypot3( spanVector[0], spanVector[1], spanVector[2] );
		if ( distance <= length ) return interpolateMovement( from, to, length ? distance / length : 0 );
		distance -= length;
	}
	return path[path.length - 1];
}

// ============================================================================

/*
================
random

A small deterministic generator (mulberry32), so a failure replays.
================
*/
function random( seed ) {
	return () => {
		seed = (seed + 0x6d2b79f5) | 0;
		let t = Math.imul( seed ^ (seed >>> 15), 1 | seed );
		t = (t + Math.imul( t ^ (t >>> 7), 61 | t )) ^ t;
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

/*
================
walk

A walking path of n poses from a start: steps of a few units in a wandering
direction, with the shapes the chain must keep (a stop that repeats a pose,
a straight climb, a fold back over earlier ground). Outdoors the poses are
normalized into their sector, so a long walk crosses sector borders.
================
*/
function walk( next, n, dungeon ) {
	const path = [], raw = [];
	let x = dungeon ? -10468 : 1900, y = 10, z = dungeon ? 5596 : 15, heading = next() * Math.PI * 2;
	const place = () => {
		const at = dungeon ? { regionId: DUNGEON, x, z } : normalizeRegionXZ( 0x6a48, x, z );
		path.push( { regionId: at.regionId, x: at.x, y, z: at.z, angle: 0 } );
		raw.push( [ x, y, z ] );
	};
	place();
	for ( let i = 1; i < n; i++ ) {
		const shape = next();
		if ( shape < .04 ) {
			// A stop: the same pose again.
			path.push( { ...path[path.length - 1] } );
			raw.push( raw[raw.length - 1] );
			continue;
		}
		if ( shape < .07 ) {
			// A straight climb: height alone changes.
			y += 2;
			place();
			continue;
		}
		if ( shape < .1 && path.length > 8 ) {
			// A fold: walk back onto a pose already on the chain.
			[x, y, z] = raw[raw.length - 1 - Math.floor( next() * 8 )];
			place();
			continue;
		}
		heading += (next() - .5) * .8;
		const step = .5 + next() * 4;
		x += Math.cos( heading ) * step;
		z += Math.sin( heading ) * step;
		y += (next() - .5) * .6;
		place();
	}
	return path;
}

/*
================
probes

Poses to locate: every pose, points on edges, points just inside and just
outside the distance an on-chain pose may lie off its edge, and points far
off the chain.
================
*/
function probes( next, path ) {
	const out = [];
	for ( let i = 0; i < path.length; i++ ) {
		out.push( path[i] );
		if ( i ) {
			const on = interpolateMovement( path[i - 1], path[i], next() );
			out.push( on );
			for ( const off of [ .004, .0099, .0101, .3, 40 ] ) {
				const angle = next() * Math.PI * 2;
				out.push( { ...on, x: on.x + Math.cos( angle ) * off, z: on.z + Math.sin( angle ) * off } );
			}
			out.push( { ...on, y: on.y + (next() - .5) * 3 } );
		}
	}
	return out;
}

/*
================
assertSameAnswers
================
*/
function assertSameAnswers( path, poses, next ) {
	const chain = measureWalkingChain( path );
	for ( const pose of poses ) {
		for ( const latest of [ false, true ] ) {
			assert.deepEqual(
				locateOnWalkingChain( pose, chain, latest ),
				scanLocation( pose, path, latest ),
				`location of ${JSON.stringify( pose )} latest ${latest}`
			);
		}
		assert.deepEqual( nearestOnWalkingChain( pose, chain ), scanNearest( pose, path ), "nearest" );
	}
	let total = 0;
	for ( let i = 1; i < path.length; i++ ) {
		writeWorldVector( path[i], path[i - 1], spanVector );
		total += hypot3( spanVector[0], spanVector[1], spanVector[2] );
	}
	for ( const distance of [ 0, total * next(), total * next(), total, total + 5 ] ) {
		assert.deepEqual( walkingChainPose( chain, distance ), scanPose( path, distance ), `pose at ${distance}` );
	}
}

test("a measured chain locates every pose as the full scan did, outdoors and in a dungeon", () => {
	const next = random( 3563 );
	for ( const dungeon of [ false, true ] ) {
		for ( const n of [ 2, 3, 17, 33, 250 ] ) {
			for ( let run = 0; run < 3; run++ ) {
				const path = walk( next, n, dungeon );
				assertSameAnswers( path, probes( next, path ), next );
			}
		}
	}
});

test("a long outdoor walk crosses sector borders and still answers as the full scan did", () => {
	const next = random( 2788 ), path = walk( next, 400, false );
	assert.ok( new Set( path.map( pose => pose.regionId ) ).size > 1, "the walk crosses a sector border" );
	assertSameAnswers( path, probes( next, path ), next );
});

test("a pose in another coordinate space, or a chain broken between spaces, finds nothing", () => {
	const next = random( 1 ), outdoor = walk( next, 40, false ), dungeon = walk( next, 40, true );
	// Region offsets to a dungeon are zero, so this dungeon pose's planar
	// position lands right on the outdoor chain: only its space differs.
	for ( const elsewhere of [ { ...dungeon[5] }, { ...outdoor[5], regionId: DUNGEON } ] ) {
		assert.equal( locateOnWalkingChain( elsewhere, measureWalkingChain( outdoor ), true ), null );
		assert.equal( scanLocation( elsewhere, outdoor, true ), null );
		assert.equal( nearestOnWalkingChain( elsewhere, measureWalkingChain( outdoor ) ), null );
	}
	const broken = [ ...outdoor.slice( 0, 20 ), ...dungeon.slice( 0, 20 ) ], chain = measureWalkingChain( broken );
	for ( const pose of [ outdoor[3], dungeon[3], outdoor[19] ] ) {
		assert.equal( locateOnWalkingChain( pose, chain, true ), scanLocation( pose, broken, true ) );
		assert.equal( nearestOnWalkingChain( pose, chain ), scanNearest( pose, broken ) );
	}
	// Spending distance throws only on reaching the broken edge, as before.
	assert.deepEqual( walkingChainPose( chain, 1 ), scanPose( broken, 1 ) );
	assert.throws( () => walkingChainPose( chain, 1e9 ), /Invalid walking path edge/ );
	assert.throws( () => scanPose( broken, 1e9 ), /Invalid walking path edge/ );
});

test("a path measured with the path it replaced has the same measures as one measured alone", () => {
	const next = random( 3584 );
	for ( const dungeon of [ false, true ] ) {
		const long = walk( next, 300, dungeon ), other = walk( next, 80, dungeon );
		const before = long.slice( 0, 250 );
		const slid = [
			// The window slid on: a few poses dropped at the head, a few added.
			long.slice( 4, 254 ),
			// Re-anchored halfway: the tail is another walk's.
			[ ...long.slice( 4, 120 ), ...other.slice( 0, 60 ) ],
			// Nothing in common.
			other,
			// The predecessor itself.
			before
		];
		for ( const path of slid ) {
			assert.deepEqual( measureWalkingChain( path, measureWalkingChain( before ) ), measureWalkingChain( path ) );
		}
		// A predecessor broken between spaces lends only the edges before the break.
		const broken = [ ...long.slice( 0, 30 ), ...walk( next, 10, !dungeon ) ];
		const path = long.slice( 2, 60 );
		assert.deepEqual( measureWalkingChain( path, measureWalkingChain( broken ) ), measureWalkingChain( path ) );
	}
});
