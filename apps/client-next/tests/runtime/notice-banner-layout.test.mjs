/*
===========================================================================

notice-banner-layout.test.mjs - the top-centre notice plate and tiers

Port-only (#1976): a seal drop's banner wears its tier's weight, Star low
key, Moon mid, Sun high. The native plate (no padding, the shipped alpha
curve, the native chrome) is unchanged.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { uniqueBannerQuads } = await import( "../../src/engine/foundation/ui/unique-banner.ts" );
const {
	createNoticeBanner,
	noticeBannerRow,
	noticeTiming,
	notificationBannerPaths,
	sealBannerStyle,
	uniqueBannerPaths
} = await import(
	"../../src/engine/runtime/ui/hud/unique-banner.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

const PATHS = [ "corner", "edge", "edge2" ];

/** @param {string} _path @returns {[number, number]} */
function size( _path ) {
	return [ 40, 10 ];
}

test("the native plate hugs the caption with the shipped alpha", () => {
	const quads = uniqueBannerQuads( "notice", 1, 1024, 768, 200, 16, PATHS, size, 100, [ 128, 45, 67 ] );
	assert.deepEqual( quads[0].rect, [ 412, 100, 200, 16 ] );
	assert.equal( quads[0].color[3], 128 / 255 );
});

/**
 * @param {string} text
 * @param {number} [sequence]
 * @returns {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice}
 */
const notice = ( text, sequence = 1 ) => ({
	key: "",
	text,
	value: 0,
	notificationBanner: true,
	bannerOnly: true,
	dropPlus: true,
	sequence
});
const STAR = "Looter has dropped D6 Star Blade (Seal Of Star) from Tiger Girl";
const MOON = "Looter has dropped D6 Moon Blade (Seal Of Moon) from Tiger Girl";
const SUN = "SEAL OF SUN: Looter has dropped D8 Sun Blade from Tiger Girl";
const GUILD_WAR = "The guild AzureSky has declared war on the guild CrimsonFang";

test("a style grows the plate and sets its opacity, inside the native chrome", () => {
	const frame = { padX: 16, padY: 6, plateAlpha: 0.8 };
	const quads = uniqueBannerQuads( "notice", 1, 1024, 768, 200, 16, PATHS, size, 100, [ 128, 45, 67 ], frame );
	assert.deepEqual( quads[0].rect, [ 396, 94, 232, 28 ] );
	assert.equal( quads[0].color[3], Math.round( 0.8 * 255 ) / 255 );
	assert.deepEqual( quads[1].rect, [ 396 - 40, 94 - 10, 40, 10 ], "the chrome frames the padded plate" );
});

test("each louder tier is bigger and stays longer, and every tier wears native art", () => {
	const [star, moon, sun] = [ sealBannerStyle( 0 ), sealBannerStyle( 1 ), sealBannerStyle( 2 ) ];
	for ( const [quiet, loud] of [ [ star, moon ], [ moon, sun ] ] ) {
		assert.ok( (quiet.frame.padY ?? 0) < (loud.frame.padY ?? 0) );
		assert.ok( quiet.timing.showMs < loud.timing.showMs );
	}
	assert.deepEqual( star.frame, {}, "Star is the native plate" );
	assert.deepEqual( star.paths, notificationBannerPaths );
	assert.deepEqual( moon.paths, uniqueBannerPaths );
	assert.deepEqual( sun.paths, notificationBannerPaths );
});

test("the runtime row takes a tier's weight only for a seal drop", () => {
	/** @param {string} text */
	const row = text => {
		const banner = createNoticeBanner( "notificationBanner" );
		banner.step( [ notice( text ) ], 0, true );
		return { tier: banner.sealTier(), row: noticeBannerRow( banner ) };
	};
	assert.equal( row( STAR ).tier, 0 );
	assert.equal( row( MOON ).tier, 1 );
	assert.equal( row( SUN ).tier, 2 );
	assert.equal( row( "Looter has forged Blade to +8" ).tier, undefined );
	const native = row( GUILD_WAR ).row;
	assert.deepEqual( native.color, [ 128, 45, 67 ] );
	assert.deepEqual( native.textRgb, [ 1, 1, 1 ] );
	assert.deepEqual( native.frame, {}, "a native notice keeps the native plate" );
	assert.deepEqual( native.paths, notificationBannerPaths );
	assert.deepEqual( row( STAR ).row.frame, sealBannerStyle( 0 ).frame );
	assert.deepEqual( row( SUN ).row.color, sealBannerStyle( 2 ).plate );
});

test("a quieter seal never cuts off a louder one, and a louder one always does", () => {
	const banner = createNoticeBanner( "notificationBanner" );
	banner.step( [ notice( SUN, 1 ) ], 0, true );
	banner.step( [ notice( SUN, 1 ), notice( STAR, 2 ) ], 100, true );
	assert.equal( banner.sealTier(), 2, "the Star waits out the Sun" );
	banner.step( [ notice( GUILD_WAR, 3 ) ], 200, true );
	assert.equal( banner.sealTier(), undefined, "a native notice still replaces at once" );
	const quiet = createNoticeBanner( "notificationBanner" );
	quiet.step( [ notice( STAR, 1 ) ], 0, true );
	quiet.step( [ notice( SUN, 2 ) ], 100, true );
	assert.equal( quiet.sealTier(), 2 );
	const faded = createNoticeBanner( "notificationBanner" );
	faded.step( [ notice( SUN, 1 ) ], 0, true );
	faded.step( [], 8000, true );
	faded.step( [ notice( STAR, 2 ) ], 8100, true );
	assert.equal( faded.sealTier(), 0, "a Sun that has left the screen holds nothing back" );
});

test("a tier's timing runs its line, within the player's Short notices setting", () => {
	const star = createNoticeBanner( "notificationBanner" );
	star.step( [ notice( STAR ) ], 0, true );
	star.step( [], 4000, true );
	assert.equal( star.alpha(), 0, "Star leaves at its own four seconds" );
	const sun = createNoticeBanner( "notificationBanner" );
	sun.step( [ notice( SUN ) ], 0, true );
	sun.step( [], 4000, true );
	assert.equal( sun.alpha(), 1, "Sun holds the native time" );
	const short = noticeTiming( true );
	sun.step( [], 5000, true, undefined, short );
	assert.equal( sun.alpha(), 0, "Short notices still shortens a Sun" );
	const native = createNoticeBanner( "notificationBanner" );
	native.step( [ notice( GUILD_WAR ) ], 0, true );
	native.step( [], 4000, true );
	assert.equal( native.alpha(), 1, "a native notice keeps the native timing" );
});

test("the Event chat tab is on unless explicitly turned off", () => {
	for ( const value of [ undefined, null, {}, { eventChatTab: 1 }, { eventChatTab: "true" } ] ) {
		assert.equal( experimentalOptions( value ).eventChatTab, true );
	}
	assert.equal( experimentalOptions( { eventChatTab: false } ).eventChatTab, false );
});
