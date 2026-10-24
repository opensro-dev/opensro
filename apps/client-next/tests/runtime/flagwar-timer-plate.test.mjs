/*
===========================================================================

flagwar-timer-plate.test.mjs - the flag war timer's digit reading

The native painter (CIFEventTimer_UpdateDigitTextures 0x5182C0) reads
the remainder as MM:SS, one timer_<d> texture per digit, and turns
all five statics red under a minute left. These tests pin that
arithmetic at its boundaries: the full match, the urgent edge, the
expiry instant and a remainder past the plate's two minute digits.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { flagWarTimerReading, flagWarTimerDigitTexture, FLAG_WAR_TIMER_URGENT_MS } = await import(
	"../../src/engine/foundation/ui/flagwar-timer-plate.ts"
);

test("a fresh match reads 20:00 and is not urgent", () => {
	assert.deepEqual( flagWarTimerReading( 0x124f80 ), {
		minTens: 2,
		minOnes: 0,
		secTens: 0,
		secOnes: 0,
		urgent: false
	} );
	// Sub-second remainders floor into the running second.
	assert.equal( flagWarTimerReading( 0x124f80 - 1 ).secOnes, 9 );
});

test("the urgent edge is a strict minute, and expiry restores the color", () => {
	assert.equal( FLAG_WAR_TIMER_URGENT_MS, 60000 );
	assert.equal( flagWarTimerReading( 60000 ).urgent, false );
	assert.deepEqual( flagWarTimerReading( 59999 ), {
		minTens: 0,
		minOnes: 0,
		secTens: 5,
		secOnes: 9,
		urgent: true
	} );
	// At zero the painter's expiry branch restores the normal color.
	assert.equal( flagWarTimerReading( 0 ).urgent, false );
	assert.equal( flagWarTimerReading( -5000 ).secOnes, 0 );
});

test("minutes clamp at the plate's two digits", () => {
	const reading = flagWarTimerReading( 100 * 60 * 1000 );
	assert.equal( reading.minTens, 9 );
	assert.equal( reading.minOnes, 9 );
});

test("each digit names its own texture", () => {
	assert.equal(
		flagWarTimerDigitTexture( 7 ),
		"/assets/images/Media_extracted/interface/timer/timer_7.png"
	);
});
