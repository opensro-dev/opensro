/*
===========================================================================

short-notices.test.mjs - how long the notice banner shows a line

Native (6B3180, 6B30F0): 7000 ms with the final 2000 ms fading. The port-only
Experimental "Short notices" row: 5000 ms with the final 1000 ms fading.
Either way the latest notice replaces the current one and restarts it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createNoticeBanner, noticeTiming, NATIVE_NOTICE_TIMING, SHORT_NOTICE_TIMING } = await import(
	"../../src/engine/runtime/ui/hud/unique-banner.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/*
================
alphaAt

The banner's alpha at each elapsed time after one notice arrives at 0.
================
*/
function alphaAt( timing, times ) {
	const banner = createNoticeBanner( "notificationBanner" );
	const notices = [
		/** @type {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice} */ ({
			key: "",
			text: "Looter has dropped Blade",
			value: 0,
			notificationBanner: true,
			sequence: 1
		})
	];
	return times.map( t => {
		banner.step( notices, t, true, undefined, timing );
		return banner.alpha();
	} );
}

test("native timing shows 7000 ms and fades over the last 2000", () => {
	assert.deepEqual( NATIVE_NOTICE_TIMING, { showMs: 7000, fadeMs: 2000 } );
	const [start, full, half, gone] = alphaAt( noticeTiming( false ), [ 0, 5000, 6000, 7000 ] );
	assert.equal( start, 1 );
	assert.equal( full, 1, "still opaque when the fade begins" );
	assert.equal( half, Math.floor( 127.5 ) / 255 );
	assert.equal( gone, 0 );
});

test("short notices show 5000 ms and fade over the last 1000", () => {
	assert.deepEqual( SHORT_NOTICE_TIMING, { showMs: 5000, fadeMs: 1000 } );
	const [, full, half, gone, after] = alphaAt( noticeTiming( true ), [ 0, 4000, 4500, 5000, 6000 ] );
	assert.equal( full, 1, "fading starts at 4000 ms" );
	assert.equal( half, Math.floor( 127.5 ) / 255 );
	assert.equal( gone, 0 );
	assert.equal( after, 0 );
});

test("a newer notice replaces the current one and restarts its time", () => {
	const banner = createNoticeBanner( "notificationBanner" );
	const first = /** @type {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice} */ ({
		key: "",
		text: "first",
		value: 0,
		notificationBanner: true,
		sequence: 1
	});
	const second = /** @type {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice} */ ({
		key: "",
		text: "second",
		value: 0,
		notificationBanner: true,
		sequence: 2
	});
	banner.step( [ first ], 0, true, undefined, SHORT_NOTICE_TIMING );
	banner.step( [ first, second ], 2000, true, undefined, SHORT_NOTICE_TIMING );
	assert.equal( banner.value( () => "" ), "second" );
	banner.step( [ first, second ], 6000, true, undefined, SHORT_NOTICE_TIMING );
	assert.equal( banner.alpha(), 1, "the second notice runs its own 5000 ms from 2000" );
});

test("short notices are on unless explicitly false", () => {
	for ( const value of [ undefined, null, {}, { shortNotices: 1 }, { shortNotices: "false" } ] ) {
		assert.equal( experimentalOptions( value ).shortNotices, true );
	}
	assert.equal( experimentalOptions( { shortNotices: false } ).shortNotices, false );
});
