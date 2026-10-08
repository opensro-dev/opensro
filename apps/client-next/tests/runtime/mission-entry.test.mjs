/*
===========================================================================

mission-entry.test.mjs - tests for pick-destination.ts, mission-loading.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { pickDestination } = await import( "../../src/engine/foundation/rendering/pick-destination.ts" );
const { missionLoadingQuads, regionLoadingBackground, loadingScreenQuads, travelLoadingQuads } = await import(
	"../../src/engine/foundation/ui/mission-loading.ts"
);
test("ground picks normalize outdoor sector crossings and retain dungeon coordinates", () => {
	assert.deepEqual( pickDestination( { start: [ 1910, 40, 10 ], delta: [ 40, -40, -40 ] }, .5, 0x60a8 ), {
		regionId: 0x5fa9,
		x: 10,
		y: 20,
		z: 1910
	} );
	assert.deepEqual( pickDestination( { start: [ 1910, 40, 10 ], delta: [ 40, -40, -40 ] }, .5, 0x8001 ), {
		regionId: 0x8001,
		x: 1930,
		y: 20,
		z: -10
	} );
	for ( const depth of [ -1, 2, NaN, Infinity ] ) {
		assert.equal( pickDestination( { start: [ 0, 0, 0 ], delta: [ 1, 1, 1 ] }, depth, 0x60a8 ), null );
	}
	assert.equal( pickDestination( { start: [ 0, 0, 0 ], delta: [ -1, 0, 0 ] }, 1, 0x6000 ), null );
});
test("entry artwork and loading control group preserve their aspect ratios on resize", () => {
	const q = missionLoadingQuads( 1920, 1080, 2, .5 );
	assert.deepEqual( q[1].rect, [ 240, 0, 1440, 1080 ] );
	assert.match( q[1].texture, /europe_2/ );
	assert.deepEqual( q[3].uv, [ 0, 0, .5, 1 ] );
	for ( const [w, h] of [ [ 1600, 1200 ], [ 1920, 1080 ], [ 3440, 1440 ], [ 582, 723 ], [ 320, 720 ] ] ) {
		const quads = missionLoadingQuads( w, h, 1, .5 ),
			frame = quads[2].rect,
			gauge = quads[3].rect,
			label = quads[4].rect;
		assert.ok( Math.abs( label[2] / label[3] - 144 / 20 ) < 1e-10 );
		assert.ok( Math.abs( frame[2] / frame[3] - 1121 / 64 ) < 1e-10 );
		if ( w >= 800 && h >= 600 ) assert.equal( label[0], gauge[0] );
		assert.ok( gauge[0] >= frame[0] && gauge[0] + gauge[2] <= frame[0] + frame[2] );
		assert.ok( label[1] >= gauge[1] + gauge[3] );
		for ( const rect of [ frame, gauge, label ] ) {
			assert.ok( rect[0] >= 0 && rect[1] >= 0 && rect[0] + rect[2] <= w && rect[1] + rect[3] <= h );
		}
	}
	assert.equal( missionLoadingQuads( 1024, 768, 1, 2 )[3].uv[2], 1 );
});

test("compact loading contains the full illustration and reserves readable progress below it", () => {
	for ( const [w, h] of [ [ 360, 858 ], [ 375, 667 ], [ 667, 375 ], [ 320, 568 ], [ 200, 200 ] ] ) {
		for ( const progress of [ -1, 0, .5, 1, 2 ] ) {
			const quads = loadingScreenQuads( w, h, "scene.png", progress );
			const [, art, frame, gauge, label] = quads;
			for ( const { rect: [x, y, width, height] } of quads ) {
				assert.ok( x >= 0 && y >= 0 && x + width <= w + 1e-9 && y + height <= h + 1e-9 );
			}
			assert.deepEqual( art.uv, [ 0, 0, 1, 1 ] );
			assert.ok( Math.abs( art.rect[2] / art.rect[3] - 4 / 3 ) < 1e-10 );
			assert.equal( art.rect[0] + art.rect[2] / 2, w / 2 );
			assert.ok( art.rect[1] + art.rect[3] <= frame.rect[1] );
			assert.ok( frame.rect[1] + frame.rect[3] <= label.rect[1] );
			assert.deepEqual( label.rect, [ (w - 108) / 2, h - 64, 108, 15 ] );
			assert.equal( gauge.uv[2], Math.max( 0, Math.min( 1, progress ) ) );
			assert.deepEqual(
				travelLoadingQuads( w, h, { mode: 6, region: 0x61a8, revision: 1 }, 1, progress ),
				quads.filter( ( _, i ) => i !== 1 )
			);
		}
	}
});

test("destination artwork follows the retail city, river, port and dungeon branches", () => {
	for (
		const [regions, name] of [
			[ [ 0x694f ], "constantinople" ],
			[ [ 0x6a6c ], "samarkand" ],
			[ [ 0x6699 ], "dunwhang" ],
			[ [ 0x61a8 ], "zangan" ],
			[ [ 0x5c87 ], "hotan" ],
			[ [ 0x60b6 ], "thief" ],
			[ [ 0x624b, 0x6559, 0x6759 ], "port2" ],
			[ [ 0x629c, 0x64a1, 0x5b8c, 0x5a8c, 0x5b8f, 0x5a8f, 0x61a1, 0x609e ], "river" ],
			[ [ 0x8001 ], "dungeons_donwhang" ],
			[ [ 0x0101 ], "china_1" ]
		]
	) {
		for ( const region of regions ) {
			assert.ok( regionLoadingBackground( region ).endsWith( "loading_" + name + ".png" ) );
		}
	}
});
