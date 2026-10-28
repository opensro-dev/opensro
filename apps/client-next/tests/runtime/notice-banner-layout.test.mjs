/*
===========================================================================

notice-banner-layout.test.mjs - the top-centre notice plate and tiers

Port-only (#1976): the notice plate pads past its caption and sits more
opaque, and a seal drop's banner wears its tier's colours. The native
plate (no padding, the shipped alpha curve) is unchanged.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { uniqueBannerQuads } = await import( "../../src/engine/foundation/ui/unique-banner.ts" );
const { createNoticeBanner, sealBannerColors, SUN_DROP_BANNER_COLOR, SUN_DROP_BANNER_TEXT_RGB } = await import(
	"../../src/engine/runtime/ui/hud/unique-banner.ts"
);
const { SEAL_NOTICE_COLOR_MOON, SEAL_NOTICE_COLOR_STAR } = await import(
	"../../src/engine/foundation/gameplay/system-notices.ts"
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

test("padding grows the plate past the caption and raises its opacity", () => {
	const quads = uniqueBannerQuads( "notice", 1, 1024, 768, 200, 16, PATHS, size, 100, [ 128, 45, 67 ], 16, 6, 0.8 );
	assert.deepEqual( quads[0].rect, [ 396, 94, 232, 28 ] );
	assert.equal( quads[0].color[3], Math.round( 0.8 * 255 ) / 255 );
	// The chrome frames the padded plate, not the caption.
	assert.deepEqual( quads[1].rect, [ 396 - 40, 94 - 10, 40, 10 ] );
});

test("seal tiers colour the banner; Sun keeps its shipped colours", () => {
	assert.deepEqual( sealBannerColors( 2 ).plate, SUN_DROP_BANNER_COLOR );
	assert.deepEqual( sealBannerColors( 2 ).textRgb, SUN_DROP_BANNER_TEXT_RGB );
	assert.deepEqual( sealBannerColors( 0 ).textRgb, [ 1, 217 / 255, 83 / 255 ] );
	assert.equal( SEAL_NOTICE_COLOR_STAR, 0xffffd953 );
	assert.deepEqual( sealBannerColors( 1 ).textRgb, [ 201 / 255, 217 / 255, 242 / 255 ] );
	assert.equal( SEAL_NOTICE_COLOR_MOON, 0xffc9d9f2 );
});

test("the banner reads its seal tier off the current line", () => {
	const banner = createNoticeBanner( "notificationBanner" );
	/** @param {string} text @returns {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice} */
	const notice = text => ({
		key: "",
		text,
		value: 0,
		notificationBanner: true,
		bannerOnly: true,
		dropPlus: true,
		sequence: 1
	});
	banner.step( [ notice( "Looter has dropped D6 Star Blade (Seal Of Star) from Tiger Girl" ) ], 0, true );
	assert.equal( banner.sealTier(), 0 );
	const moon = createNoticeBanner( "notificationBanner" );
	moon.step( [ notice( "Looter has dropped D6 Moon Blade (Seal Of Moon) from Tiger Girl" ) ], 0, true );
	assert.equal( moon.sealTier(), 1 );
	const plain = createNoticeBanner( "notificationBanner" );
	plain.step( [ notice( "Looter has forged Blade to +8" ) ], 0, true );
	assert.equal( plain.sealTier(), undefined );
});

test("the Event chat tab is on unless explicitly turned off", () => {
	for ( const value of [ undefined, null, {}, { eventChatTab: 1 }, { eventChatTab: "true" } ] ) {
		assert.equal( experimentalOptions( value ).eventChatTab, true );
	}
	assert.equal( experimentalOptions( { eventChatTab: false } ).eventChatTab, false );
});
