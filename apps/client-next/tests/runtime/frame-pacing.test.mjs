/*
===========================================================================

frame-pacing.test.mjs - display deadlines, overload recovery and saved limits

Runs the shipping presentation scheduler against synthetic display clocks.
No simulation clock or native detail bank is changed by a render preference.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { createFramePacing } = await import( "../../src/engine/runtime/frame-pacing.ts" );
const { defaultVideoOptions, videoOptions, changeVideo, resetVideoRecord } = await import(
	"../../src/engine/foundation/rendering/video-options.ts"
);

for ( const refresh of [ 60, 120, 144, 240 ] ) {
	for ( const limit of [ 60, 120, 240, 0 ] ) {
		test(`${limit || "uncapped"} FPS at ${refresh} Hz admits only current deadlines`, () => {
			const pacing = createFramePacing();
			pacing.setFrameLimit( limit );
			let frames = 0;
			for ( let tick = 0; tick < refresh * 10; tick++ ) {
				if ( pacing.admit( Math.round( tick * 10000 / refresh ) / 10, true ) ) frames++;
			}
			assert.ok( Math.abs( frames - Math.min( limit || refresh, refresh ) * 10 ) <= 1, `${frames} frames` );
		});
	}
}

test("stalls discard expired deadlines and never replay a burst", () => {
	const pacing = createFramePacing();
	assert.equal( pacing.admit( 0, true ), true );
	assert.equal( pacing.admit( 1000, true ), true );
	assert.equal( pacing.admit( 1004, true ), false );
	assert.equal( pacing.admit( 1008, true ), false );
	assert.equal( pacing.admit( 1016.7, true ), true );
});

test("hidden maintenance and limit changes do not delay foreground recovery", () => {
	const pacing = createFramePacing();
	pacing.admit( 0, true );
	assert.equal( pacing.admit( 2, false ), true );
	assert.equal( pacing.admit( 3, true ), true );
	assert.equal( pacing.admit( 4, true ), false );
	pacing.setFrameLimit( 120 );
	assert.equal( pacing.admit( 5, true ), true );
	assert.equal( pacing.admit( 9, true ), false );
	assert.equal( pacing.admit( 13.4, true ), true );
});

test("saved native video banks migrate to 60 FPS and preserve explicit limits", () => {
	const defaults = defaultVideoOptions();
	assert.equal( defaults.frameLimit, 60 );
	const { frameLimit, ...legacy } = defaults;
	assert.equal( videoOptions( legacy ).frameLimit, 60 );
	for ( const limit of [ 60, 120, 240, 0 ] ) {
		const saved = videoOptions( JSON.parse( JSON.stringify( { ...legacy, frameLimit: limit } ) ) );
		assert.equal( saved.frameLimit, limit );
		assert.deepEqual( saved.records, legacy.records );
		assert.equal( changeVideo( saved, 2, 3 ).frameLimit, limit );
		assert.equal( resetVideoRecord( saved ).frameLimit, 60 );
	}
	for ( const invalid of [ -1, 59, 120.5, NaN, "60", null ] ) {
		assert.throws( () => videoOptions( { ...legacy, frameLimit: invalid } ), /Invalid frame limit/ );
	}
});
