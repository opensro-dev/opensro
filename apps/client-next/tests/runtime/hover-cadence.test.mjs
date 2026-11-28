/*
===========================================================================

hover-cadence.test.mjs - tests for hover-cadence.ts

The world cursor's hover pick runs at the native 60 Hz whatever the frame
rate, and a reset (hover not allowed) makes the next frame pick at once.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createHoverCadence, HOVER_PICK_INTERVAL_MS } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/hover-cadence.ts" ).href
);

test("a 500 fps frame clock picks at the native 60 Hz, never twice in one interval", () => {
	const cadence = createHoverCadence();
	let picks = 0;
	for ( let frame = 0; frame < 500; frame++ ) if ( cadence.due( frame * 2 ) ) picks++;
	// One second of 2 ms frames: one pick per 16.7 ms interval.
	assert.ok( picks >= 59 && picks <= 61, `picks ${picks}` );
	assert.equal( HOVER_PICK_INTERVAL_MS, 1000 / 60 );
});

test("a slower frame than the interval picks every frame", () => {
	const cadence = createHoverCadence();
	for ( let frame = 0; frame < 20; frame++ ) assert.equal( cadence.due( frame * 20 ), true );
});

test("a reset picks on the very next frame, and a clock that steps back does not stall", () => {
	const cadence = createHoverCadence();
	assert.equal( cadence.due( 100 ), true );
	assert.equal( cadence.due( 101 ), false );
	cadence.reset();
	assert.equal( cadence.due( 102 ), true );
	assert.equal( cadence.due( 50 ), true );
});
