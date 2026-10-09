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

const { defaultVideoOptions, changeVideo, videoOptions, backgroundDrawDistance, uiPixelScaleFor } = await import(
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

/*
================
UI scale by screen

Physical size and pixel ratio of maximized browser windows. A window at
least 800x600 CSS pixels keeps the desktop HUD (UI at least 800x600);
a smaller one keeps its whole-number enlargement and goes compact.
================
*/
test("a desktop window keeps the desktop HUD and only small windows go compact", () => {
	const cases = [
		{ name: "1080p at 100%", width: 1920, height: 945, ratio: 1, scale: 1, desktop: true },
		{ name: "1080p at 125%", width: 1920, height: 913, ratio: 1.25, scale: 1, desktop: true },
		{ name: "1080p at 150%", width: 1920, height: 926, ratio: 1.5, scale: 1, desktop: true },
		{ name: "1440p at 150%", width: 2561, height: 1313, ratio: 1.5, scale: 2, desktop: true },
		{ name: "MacBook Air", width: 2940, height: 1660, ratio: 2, scale: 2, desktop: true },
		{ name: "1366x768 at 100%", width: 1366, height: 625, ratio: 1, scale: 1, desktop: true },
		{ name: "phone landscape", width: 2532, height: 1170, ratio: 3, scale: 3, desktop: false },
		{ name: "phone portrait", width: 1170, height: 2532, ratio: 3, scale: 3, desktop: false },
		{ name: "small window at 200%", width: 1400, height: 1000, ratio: 2, scale: 2, desktop: false }
	];
	for ( const { name, width, height, ratio, scale, desktop } of cases ) {
		assert.equal( uiPixelScaleFor( width, height, ratio ), scale, name );
		assert.equal( width / scale >= 800 && height / scale >= 600, desktop, name + " layout" );
	}
});
