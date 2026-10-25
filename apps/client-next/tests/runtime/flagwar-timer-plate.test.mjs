/*
===========================================================================

flagwar-timer-plate.test.mjs - the flag war timer's digit reading

The native painter (CIFEventTimer_UpdateDigitTextures 0x5182C0) reads
the remainder as MM:SS, one timer_<d> texture per digit. Its red
urgent tint under a minute is behind the timer's mode byte
(0x5184DD: the block runs only when +0x37D == 2), and the flag war
arms mode 1, so these tests pin plain digits at every remainder:
the full match, the last minute, the expiry instant and a remainder
past the plate's two minute digits.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { flagWarTimerReading, flagWarTimerDigitTexture } = await import(
	"../../src/engine/foundation/ui/flagwar-timer-plate.ts"
);

test("a fresh match reads 20:00", () => {
	assert.deepEqual( flagWarTimerReading( 0x124f80 ), {
		minTens: 2,
		minOnes: 0,
		secTens: 0,
		secOnes: 0
	} );
	// Sub-second remainders floor into the running second.
	assert.equal( flagWarTimerReading( 0x124f80 - 1 ).secOnes, 9 );
});

test("the last minute reads plain digits down to expiry", () => {
	assert.deepEqual( flagWarTimerReading( 60000 ), {
		minTens: 0,
		minOnes: 1,
		secTens: 0,
		secOnes: 0
	} );
	// Under a minute nothing changes but the digits: the painter's
	// red block is mode-2 only and the flag war arms mode 1.
	assert.deepEqual( flagWarTimerReading( 59999 ), {
		minTens: 0,
		minOnes: 0,
		secTens: 5,
		secOnes: 9
	} );
	assert.deepEqual( flagWarTimerReading( 5000 ), {
		minTens: 0,
		minOnes: 0,
		secTens: 0,
		secOnes: 5
	} );
	assert.equal( flagWarTimerReading( 0 ).secOnes, 0 );
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
