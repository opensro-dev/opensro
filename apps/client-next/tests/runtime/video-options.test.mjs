/*
===========================================================================

video-options.test.mjs - tests for video-options.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { defaultVideoOptions, changeVideo, videoOptions, backgroundDrawDistance } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/video-options.ts" ).href
);
test("background range and effect quality survive the persisted two-record option format", () => {
	let options = defaultVideoOptions();
	const untouched = [ ...options.records[1] ];
	for ( let i = 0; i < 5; i++ ) {
		options = changeVideo( options, 2, i );
		options = videoOptions( JSON.parse( JSON.stringify( options ) ) );
		assert.equal( backgroundDrawDistance( options ), 1500 + i * 1000 );
		assert.equal( options.records[0][0], 4 );
		assert.deepEqual( options.records[1], untouched );
	}
	for ( let i = 0; i < 3; i++ ) {
		options = changeVideo( options, 13, i );
		assert.equal( videoOptions( JSON.parse( JSON.stringify( options ) ) ).records[0][13], i );
	}
	const second = changeVideo( { ...options, active: 1 }, 2, 0 );
	assert.equal( backgroundDrawDistance( second ), 1500 );
	assert.equal( options.records[0][2], 4 );
	assert.equal( changeVideo( options, 2, 5 ), options );
	assert.equal( changeVideo( options, 13, -1 ), options );
});
test("native store-only slots and implemented water/cloth settings persist independently", () => {
	let options = defaultVideoOptions();
	options = changeVideo( options, 3, 4 );
	options = changeVideo( options, 5, 0 );
	const saved = videoOptions( JSON.parse( JSON.stringify( options ) ) );
	assert.equal( saved.records[0][3], 4 );
	assert.equal( saved.records[0][5], 0 );
	assert.equal( saved.records[0][0], 4 );
	assert.equal( backgroundDrawDistance( saved ), backgroundDrawDistance( defaultVideoOptions() ) );
	assert.equal( videoOptions( changeVideo( saved, 4, 1 ) ).records[0][4], 1 );
	assert.equal( videoOptions( changeVideo( saved, 12, 0 ) ).records[0][12], 0 );
});
