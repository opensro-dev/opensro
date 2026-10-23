/*
===========================================================================

restriction-notices.test.mjs - tests for system-notices.ts, notice-text.ts,
gameplay.ts, unique-banner.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { defined } from "../helpers/defined.mjs";
import { sourceDataRoot } from "../../../../scripts/build/world/paths.mjs";
const { restrictionNotice } = await import( "../../src/engine/foundation/gameplay/system-notices.ts" );
const { noticeText } = await import( "../../src/engine/foundation/ui/notice-text.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createNoticeBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
function packet( kind, date ) {
	const p = new Uint8Array( 5 );
	p[0] = kind;
	new DataView( p.buffer ).setUint32( 1, date, true );
	return p;
}
const lookup = key =>
	key === "UIIT_STT_LETTER_FORENOON" ?
		"AM" :
		key === "UIIT_STT_LETTER_AFTEMOON" ?
		"PM" :
		key.includes( "PUNISHMENT" ) ?
		"%d-%d-%d %s %d:%d" :
		"";
// The retail English oracle lives in the operator's source-data, never in
// this repository; the two tests that read it skip without it.
const restrictionCatalogPath = join(
	sourceDataRoot,
	"fixtures/native/system-notification/restriction-english-catalog.json"
);
const restrictionCatalog = existsSync( restrictionCatalogPath ) ?
	JSON.parse( readFileSync( restrictionCatalogPath, "utf8" ) ).entries :
	null;
test("restriction packet retains native masks, noon boundary and both message destinations", () => {
	for ( const kind of [ 0, 1 ] ) {
		for ( let hour = 0; hour < 32; hour++ ) {
			for ( const dateBits of [ 0, 63 | (15 << 6) | (31 << 10) | (63 << 20) | (63 << 26) ] ) {
				const n = restrictionNotice( 0x36ea, packet( kind, dateBits | (hour << 15) ) );
				assert.equal( defined( n ).nativeType, 5 );
				assert.equal( defined( n ).notificationBanner, true );
				assert.equal( defined( n ).bannerOnly, undefined );
				assert.equal(
					defined( n ).key,
					kind === 0 ? "UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK" : "UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK"
				);
				const max = dateBits !== 0;
				assert.equal(
					noticeText( lookup, n ),
					`${max ? 2063 : 2000}-${max ? 15 : 0}-${max ? 31 : 0} ${hour > 12 ? "PM" : "AM"} ${
						hour > 12 ? hour - 12 : hour
					}:${max ? 63 : 0}`
				);
				assert.equal( noticeText( lookup, n, "banner" ), noticeText( lookup, n ) );
			}
		}
	}
	assert.equal( restrictionNotice( 0x3405, packet( 0, 0 ) ), null, "research opcode is not client opcode" );
	for ( let kind = 2; kind < 256; kind++ ) assert.equal( restrictionNotice( 0x36ea, packet( kind, 0 ) ), null );
});
test("restriction ingress is atomic and independent of academy bootstrap", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 7, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	const before = g.take();
	for ( let length = 0; length < 8; length++ ) {
		if ( length !== 5 ) {
			assert.throws( () => g.receive( { opcode: 0x36ea, payload: new Uint8Array( length ) }, 0 ), /restriction/ );
			assert.equal( g.take(), null );
		}
	}
	for ( const kind of [ 0, 1 ] ) {
		g.receive( { opcode: 0x36ea, payload: packet( kind, 0xffffffff ) }, 0 );
		const next = g.take();
		assert.equal( defined( defined( defined( next ).notices ).at( -1 ) ).sequence, kind + 1 );
		assert.deepEqual( defined( next ).academy, defined( before ).academy );
		assert.deepEqual( defined( next ).social, defined( before ).social );
	}
});
test("secure notice projection does not invent missing translations or render invalid argument types", ( t ) => {
	if ( !restrictionCatalog ) {
		t.skip( "source-data restriction catalogue unavailable" );
		return;
	}
	const n = restrictionNotice( 0x36ea, packet( 0, 0 ) ),
		entries = restrictionCatalog;
	assert.match( entries[defined( n ).key], /%year/ );
	assert.equal(
		noticeText( k => entries[k] ?? "", n ),
		"",
		"bundled chat format reaches native secure-CRT invalid-parameter path"
	);
	const trade = restrictionNotice( 0x36ea, packet( 1, 0 ) );
	assert.equal( entries[defined( trade ).key], undefined );
	assert.ok( entries["UIIT_MSG_GM_PUNISHMENT_TRADE _BLOCK"] );
	assert.equal( noticeText( k => entries[k] ?? "", trade ), "" );
	for ( const text of [ "%s", "%y", "%", "%d %d %d %d", "%ld", "%d %d %d %s %d %d %d" ] ) {
		assert.equal( noticeText( () => text, n ), "" );
	}
	assert.equal( noticeText( () => "x".repeat( 510 ), n ), "x".repeat( 510 ) );
	assert.equal( noticeText( () => "x".repeat( 511 ), n ), "" );
	assert.equal( noticeText( () => "%%", n ), "%" );
	const banner = createNoticeBanner( "notificationBanner" );
	banner.step( [ { key: "", text: "Earlier", value: 0, sequence: 1, notificationBanner: true } ], 0, true, lookup );
	banner.step( [ { ...n, sequence: 2 } ], 100, true, k => entries[k] ?? "" );
	assert.equal( banner.value( lookup ), "Earlier", "empty text does not erase an earlier native notice" );
});

test("product localization correction makes both restriction channels usable without changing the native oracle", async ( t ) => {
	if ( !restrictionCatalog ) {
		t.skip( "source-data restriction catalogue unavailable" );
		return;
	}
	const { completeRestrictionText } = await import( "../../../../scripts/build/shared/textResources.mjs" );
	const captured = restrictionCatalog;
	const entries = { ...captured };
	completeRestrictionText( entries );
	const first = { ...entries };
	completeRestrictionText( entries );
	assert.deepEqual( entries, first );
	assert.match( captured.UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK, /%year/ );
	assert.equal( captured.UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK, undefined );
	const custom = {
		UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK: "custom chat",
		UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK: "custom trade"
	};
	completeRestrictionText( custom );
	assert.deepEqual( custom, {
		UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK: "custom chat",
		UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK: "custom trade"
	} );
	const empty = {};
	completeRestrictionText( empty );
	assert.deepEqual( empty, {} );
	const packed = 26 | (9 << 6) | (22 << 10) | (15 << 15) | (7 << 20);
	for ( const kind of [ 0, 1 ] ) {
		const n = { ...restrictionNotice( 0x36ea, packet( kind, packed ) ), sequence: kind + 1 };
		const expected = kind === 0 ?
			"Chat restricted by GM. Can chat from 2026-9-22 Afternoon 3hour 7minute(s)." :
			"Trading restricted by GM. Can trade from 2026-9-22 Afternoon 3hour 7minute(s).";
		const copy = k => entries[k] ?? "";
		assert.equal( noticeText( copy, n ), expected );
		const banner = createNoticeBanner( "notificationBanner" );
		banner.step( [ n ], 0, true, copy );
		assert.equal( banner.value( copy ), expected );
		const { createHudMessages } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );
		const hud = createHudMessages( () => 0 );
		assert.deepEqual( hud.step( 0, [], 1, 0, [ n ], copy ), [ {
			value: expected,
			category: "game",
			colorArgb: 0xffdbc99b
		} ] );
	}
});
