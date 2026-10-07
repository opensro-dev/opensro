/*
===========================================================================

walking-history-presentation.test.mjs - sampled navigation recovery geometry

Exercises the public presentation owner with hills, corners and disconnected
spaces. Recovery may change timing but cannot replace the accepted surface
chain with a straight endpoint chord.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );

/*
================
pose
================
*/
function pose( x, y, z = 100, regionId = 257 ) {
	return { regionId, x, y, z, angle: 0 };
}

/*
================
publish
================
*/
function publish( presentation, input ) {
	presentation.samples( new Map( [ [ 7, input ] ] ) );
}

/*
================
onSegment
================
*/
function onSegment( value, from, to ) {
	const span = [ to.x - from.x, to.y - from.y, to.z - from.z ];
	const point = [ value.x - from.x, value.y - from.y, value.z - from.z ];
	const length2 = span.reduce( ( sum, component ) => sum + component ** 2, 0 );
	const fraction = Math.max(
		0,
		Math.min( 1, point.reduce( ( sum, component, i ) => sum + component * span[i], 0 ) / length2 )
	);
	return Math.hypot( ...point.map( ( component, i ) => component - span[i] * fraction ) ) < 1e-7;
}

for (
	const [name, path] of [
		[ "hill", [ pose( 100, 0 ), pose( 105, 10 ), pose( 110, 0 ) ] ],
		[ "obstacle corner", [ pose( 100, 0 ), pose( 110, 0 ), pose( 110, 0, 110 ) ] ]
	]
) {
	test(`delayed ${name} recovery retains the displayed pose and follows accepted sample edges`, () => {
		const presentation = createPosePresentation(), start = path[0], end = path.at( -1 );
		presentation.origin( 0 );
		publish( presentation, { atMs: 0, revision: 1, moving: false } );
		assert.deepEqual( presentation.pose( 7, start, 0 ), start );
		publish( presentation, {
			atMs: 1000,
			revision: 2,
			moving: false,
			walkingPath: path,
			transition: { relocation: 0, reason: "receipt", eligible: true }
		} );
		assert.deepEqual(
			presentation.pose( 7, end, 1 ),
			start,
			"first delayed publication consumes no invisible travel"
		);
		let shown = start, reachedInterior = false;
		for ( let frame = 1; frame <= 120; frame++ ) {
			shown = presentation.pose( 7, end, 1 + frame / 120 );
			assert.ok( path.slice( 1 ).some( ( to, i ) => onSegment( shown, path[i], to ) ), JSON.stringify( shown ) );
			reachedInterior ||= name === "hill" ? shown.y > 5 : shown.x >= 110 && shown.z > 100;
		}
		assert.ok( reachedInterior, "recovery traverses the hill or corner instead of its endpoint chord" );
		assert.ok( Math.hypot( shown.x - end.x, shown.y - end.y, shown.z - end.z ) < .01 );
	});
}

test("a sampled path cannot join disconnected dungeon spaces", () => {
	const presentation = createPosePresentation(),
		start = pose( 100, 0, 100, 0x8001 ),
		end = pose( 110, 10, 100, 0x8002 );
	presentation.origin( 0 );
	publish( presentation, { atMs: 0, revision: 1, moving: false } );
	presentation.pose( 7, start, 0 );
	publish( presentation, {
		atMs: 1000,
		revision: 2,
		moving: false,
		walkingPath: [ start, end ],
		transition: { relocation: 0, reason: "receipt", eligible: true }
	} );
	assert.deepEqual( presentation.pose( 7, end, 1 ), end );
});

test("an overlapping floor is not an admitted recovery start", () => {
	const presentation = createPosePresentation(), start = pose( 105, 0 ), end = pose( 110, 10 );
	presentation.origin( 0 );
	publish( presentation, { atMs: 0, revision: 1, moving: false } );
	presentation.pose( 7, start, 0 );
	publish( presentation, {
		atMs: 1000,
		revision: 2,
		moving: false,
		walkingPath: [ pose( 100, 10 ), end ],
		transition: { relocation: 0, reason: "receipt", eligible: true }
	} );
	assert.deepEqual( presentation.pose( 7, end, 1 ), end, "planar overlap cannot authorize a glide between floors" );
});

test("a retraced lookahead consumes its returning edge without oscillation", () => {
	const presentation = createPosePresentation(), corner = pose( 100, 0 ), beyond = pose( 110, 0 );
	presentation.origin( 0 );
	publish( presentation, { atMs: 0, revision: 1, moving: false } );
	presentation.pose( 7, beyond, 0 );
	publish( presentation, {
		atMs: 1000,
		revision: 2,
		moving: false,
		walkingPath: [ corner, beyond, corner ],
		transition: { relocation: 0, reason: "receipt", eligible: true }
	} );
	let previous = presentation.pose( 7, corner, 1 );
	assert.deepEqual( previous, beyond );
	for ( let frame = 1; frame <= 120; frame++ ) {
		const shown = presentation.pose( 7, corner, 1 + frame / 120 );
		assert.ok(
			shown.x <= previous.x && shown.x >= corner.x,
			"return edge never reprojects onto the outbound edge"
		);
		previous = shown;
	}
	assert.ok( Math.abs( previous.x - corner.x ) < .01 );
});

test("a replacement command cannot borrow a previous command's recovery edge", () => {
	const presentation = createPosePresentation(), start = pose( 100, 0 ), end = pose( 110, 0 );
	presentation.origin( 0 );
	publish( presentation, { atMs: 0, revision: 1, moving: false, from: start, to: end } );
	presentation.pose( 7, start, 0 );
	publish( presentation, {
		atMs: 1000,
		revision: 2,
		moving: false,
		walkingPath: [ pose( 110, 0, 110 ), end ],
		transition: { relocation: 0, reason: "receipt", eligible: true }
	} );
	assert.deepEqual(
		presentation.pose( 7, end, 1 ),
		end,
		"unrelated retained endpoint chords do not authorize recovery"
	);
});

test("authored displacement ignores a stale ground history", () => {
	const start = pose( 100, 0 ), end = pose( 110, 10 );
	const outputs = [ undefined, [ start, pose( 105, 30 ), end ] ].map( walkingPath => {
		const presentation = createPosePresentation();
		presentation.origin( 0 );
		publish( presentation, { atMs: 0, revision: 1, moving: false } );
		presentation.pose( 7, start, 0 );
		publish( presentation, {
			atMs: 100,
			revision: 2,
			moving: true,
			from: start,
			to: end,
			durationMs: 1000,
			startedAtMs: 100,
			displacement: true,
			walkingPath,
			transition: { relocation: 1, reason: "displacement", eligible: false }
		} );
		return [ .1, .116, .15, .2 ].map( now => presentation.pose( 7, start, now ) );
	} );
	assert.deepEqual( outputs[0], outputs[1] );
});
