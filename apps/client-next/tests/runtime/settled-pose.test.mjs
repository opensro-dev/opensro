/*
===========================================================================

settled-pose.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( file ) {
	return (await import( sourceFileUrl( file ).href )).createPosePresentation;
}
const current = await load( "src/engine/runtime/characters/pose-presentation.ts" ),
	reference = await load( "tests/fixtures/pose-presentation-before-settled.ts" );
test("settled pose reuse preserves exact interpolation across height, heading, reset, reuse and sector changes", () => {
	const a = current(), b = reference();
	let time = 0;
	const poses = Array.from(
		{ length: 12 },
		( _, gid ) => ({ regionId: 24744, x: 10.123456789 + gid, y: gid / 7, z: 1919.9999999, angle: gid * 100 })
	);
	for ( let frame = 0; frame < 1000; frame++ ) {
		// Stall continuity intentionally differs from the legacy reset; the
		// dedicated stall-recovery suite owns that behavior. Compare ordinary
		// frames here to retain the settled-result optimization's exactness.
		time += 1 / 240;
		if ( frame % 97 === 0 ) time -= .2;
		if ( frame % 127 === 0 ) {
			a.reset();
			b.reset();
		}
		if ( frame % 79 === 0 ) {
			const keep = new Set( [ 0, 2, 4, 6, 8, 10 ] );
			a.retain( keep );
			b.retain( keep );
		}
		for ( let gid = 0; gid < poses.length; gid++ ) {
			const p = poses[gid];
			if ( frame % 53 === 0 ) p.y += .5;
			if ( frame % 67 === 0 ) p.angle = (p.angle + 50000) % 65536;
			if ( gid % 3 === 0 && frame % 2 === 0 ) p.x += .3;
			if ( frame === 400 ) {
				p.regionId = 0x8001;
				p.x = 2;
				p.z = 3;
			}
			if ( frame === 700 ) p.regionId = 0x8002;
			const actual = a.pose( gid, p, time ), expected = b.pose( gid, p, time );
			assert.deepEqual( actual, expected, `frame ${frame}, actor ${gid}` );
			assert.equal( a.moving( gid ), b.moving( gid ) );
			actual.x = 99999; // A caller cannot modify the retained normalized result.
		}
	}
});

test("stationary death discards residual translation but permits correction, revival, and moving displacement", () => {
	const p = current(), start = { regionId: 257, x: 0, y: 0, z: 0, angle: 0 }, end = { ...start, x: 10 };
	p.pose( 7, start, 0 );
	p.pose( 7, end, .016 );
	assert.ok( p.pose( 7, end, .02 ).x < 10, "fixture must still be interpolating" );
	const dead = p.pose( 7, end, .021, true );
	assert.equal( dead.x, 10 );
	assert.equal( p.moving( 7 ), false );
	for ( const now of [ .022, .03, .05, .1 ] ) assert.deepEqual( p.pose( 7, end, now, true ), dead );
	assert.equal(
		p.pose( 7, { ...end, x: 30 }, .11, true ).x,
		30,
		"authoritative corpse correction is not a cached death pose"
	);
	p.pose( 7, { ...end, x: 40 }, .12 );
	assert.ok( p.pose( 7, { ...end, x: 40 }, .125 ).x < 40, "revival restores normal interpolation" );
	const moving = current();
	moving.pose( 8, start, 0 );
	moving.pose( 8, end, .016 );
	assert.ok( moving.pose( 8, end, .02, false ).x < 10, "moving forced displacement is not settled" );
});
