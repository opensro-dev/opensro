/*
===========================================================================

native-notice-dispatch.test.mjs - tests for native-notice-data.ts,
native-notice.ts, gameplay.ts, notice-text.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { nativeNoticeRoute } = await import( "../../src/engine/foundation/gameplay/native-notice-data.ts" );
const { resolveNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { noticeText } = await import( "../../src/engine/foundation/ui/notice-text.ts" );
const evidence = JSON.parse( readFileSync( "tests/fixtures/native-notice-dispatch.json", "utf8" ) );
const indexed = new Map( evidence.rows.map( r => [ r.category * 256 + r.code, r ] ) );
test("all 8448 dispatch inputs preserve native constant routes or explicit context boundaries", () => {
	let visible = 0, context = 0;
	for ( let category = 0; category < 33; category++ ) {
		for ( let code = 0; code < 256; code++ ) {
			const row = indexed.get( category * 256 + code ), actual = nativeNoticeRoute( category, code );
			if ( row?.unresolved ) {
				assert.deepEqual( actual, { kind: "context", category, code } );
				context++;
				continue;
			}
			const events = row?.events.filter( e => e[2] ) ?? [];
			if ( !events.length ) {
				assert.deepEqual( actual, { kind: "silent" } );
				continue;
			}
			assert.deepEqual( actual, {
				kind: "notice",
				key: events[0][2],
				guideType: events.find( e => e[0] === "guide" )?.[1] ?? null,
				banner: events.some( e => e[0] === "banner" )
			} );
			visible++;
		}
	}
	assert.equal( visible, 561 );
	assert.equal( context, 8 );
	for ( const pair of [ [ -1, 0 ], [ 1, 256 ], [ NaN, 1 ], [ 1, 1.5 ] ] ) {
		assert.throws( () => nativeNoticeRoute( ...pair ) );
	}
	assert.equal( resolveNativeNotice( 1, 50 ).kind, "context" );
	assert.equal( resolveNativeNotice( 4, 22 ).kind, "context" );
	const modal = resolveNativeNotice( 16, 67 );
	assert.equal( modal.kind, "notice" );
	assert.equal( modal.notice.bannerOnly, true );
	assert.equal( modal.notice.banner, undefined );
	assert.deepEqual( modal.notice.dialog, {
		title: "UIIT_STT_EVENTGUIDE",
		lines: [ "UIIT_MSG_GUILDWAR_SUGGESTIONS_01", "UIIT_MSG_GUILDWAR_SUGGESTIONS_02" ]
	} );
});
test("learning, matching and guild refusals reach gameplay notices without placeholder text", () => {
	const bindings = [
		[ 0xb2cb, 5 ],
		[ 0xb165, 7 ],
		[ 0xb6ff, 2 ],
		[ 0xb3dc, 2 ],
		[ 0xb535, 2 ],
		[ 0xb588, 2 ],
		[ 0xb5bf, 2 ],
		[ 0xb663, 16 ],
		[ 0xb56e, 16 ],
		[ 0xb66e, 16 ],
		[ 0xb77a, 16 ],
		[ 0xb40f, 16 ],
		[ 0xb2bc, 16 ],
		[ 0xb65f, 16 ],
		[ 0xb701, 29 ],
		[ 0xb592, 29 ],
		[ 0xb05b, 12 ]
	];
	for ( const [opcode, category] of bindings ) {
		const g = createGameplay( () => {} );
		g.bootstrap( { academyMember: false } );
		g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
		let previous = 0;
		for ( let code = 0; code < 256; code++ ) {
			if ( opcode === 0xb663 && code === 0x3c ) continue; // dedicated duration payload below
			assert.equal( g.receive( { opcode, payload: Uint8Array.of( 2, code ) }, 0 ), true );
			const state = g.take(),
				expected = resolveNativeNotice( category, code ),
				last = defined( defined( state ).notices ).at( -1 );
			assert.equal( defined( state ).error, null, `${opcode.toString( 16 )}:${code}` );
			if ( expected.kind === "notice" ) {
				assert.ok( defined( defined( last ).sequence ) > previous );
				previous = defined( last ).sequence;
				assert.deepEqual( { ...last, sequence: undefined }, { ...expected.notice, sequence: undefined } );
			} else assert.equal( last?.sequence ?? 0, previous, "silent/context rows do not fabricate messages" );
		}
	}
});
test("guild penalty consumes duration and formats independent numeric arguments", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( { academyMember: false } );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	const payload = Buffer.alloc( 6 );
	payload[0] = 2;
	payload[1] = 0x3c;
	payload.writeUInt32LE( 2 * 86400 + 3 * 3600 + 4 * 60 + 59, 2 );
	g.receive( { opcode: 0xb663, payload }, 0 );
	const n = defined( defined( g.take() ).notices ).at( -1 );
	assert.deepEqual( defined( n ).arguments, [ "2", "3", "4" ] );
	assert.equal( defined( n ).banner, undefined );
	assert.equal( defined( n ).nativeType, 0 );
	assert.equal( noticeText( () => "%d days %d hours %d minutes", n ), "2 days 3 hours 4 minutes" );
	assert.equal(
		noticeText( () => "%I64u %% %s", { key: "test", value: 0, arguments: [ "9007199254740993", "gold" ] } ),
		"9007199254740993 % gold"
	);
	assert.throws( () => g.receive( { opcode: 0xb663, payload: payload.subarray( 0, 2 ) }, 0 ) );
});

test("malformed reply framing cannot publish a notice or replace the last valid gameplay state", () => {
	const opcodes = [
		0xb2cb,
		0xb165,
		0xb6ff,
		0xb3dc,
		0xb535,
		0xb588,
		0xb5bf,
		0xb663,
		0xb56e,
		0xb66e,
		0xb77a,
		0xb40f,
		0xb2bc,
		0xb65f,
		0xb701,
		0xb592
	];
	for ( const opcode of opcodes ) {
		const g = createGameplay( () => {} );
		g.bootstrap( { academyMember: false } );
		g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
		g.take();
		for ( const payload of [ Uint8Array.of( 2 ), Uint8Array.of( 2, 3, 99 ), Uint8Array.of( 0, 3 ) ] ) {
			assert.throws(
				() => g.receive( { opcode, payload }, 0 ),
				`${opcode.toString( 16 )}: malformed result must reject`
			);
			assert.equal( g.take(), null, `${opcode.toString( 16 )}: rejected packet must not publish` );
		}
	}
});

test("production Go error writers route every byte without altering owned gameplay data", () => {
	const packets = JSON.parse( readFileSync( "tests/fixtures/server-notice-packets.json", "utf8" ) );
	assert.equal( packets.filter( p => !p.writer.startsWith( "NoticeRefusalPayload:" ) ).length, 1280 );
	for ( const packet of packets ) {
		if ( packet.payload === null ) {
			assert.equal( packet.code, -1 );
			continue;
		}
		assert.deepEqual( packet.payload, [ 2, packet.code ], packet.writer );
		for ( const country of packet.category === 1 && packet.code === 50 ? [ 0, 1 ] : [ 0 ] ) {
			const g = createGameplay( () => {} );
			g.bootstrap( {
				localPlayerEntry: { countryByte9c: country },
				academyMember: false,
				inventorySlotCount: 45,
				equipmentSlotCount: 13,
				refItemSnapshot: [ { refObjId: 1, typeFlags: 0x8ec, name: "Herb" } ],
				equipItems: [ { slot: 20, refObjId: 1, body: [ 1, 0, 0, 0, 10, 0 ] } ]
			} );
			g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: country } );
			const before = g.take();
			assert.equal( g.receive( { opcode: packet.opcode, payload: Uint8Array.from( packet.payload ) }, 0 ), true );
			const after = g.take(), expected = resolveNativeNotice( packet.category, packet.code, { country } );
			const notices = after?.notices ?? defined( before ).notices;
			if ( expected.kind === "notice" ) {
				const last = defined( notices ).at( -1 );
				assert.ok( last, `${packet.writer}:${packet.code}` );
				assert.deepEqual( { ...last, sequence: undefined }, { ...expected.notice, sequence: undefined } );
				assert.equal(
					noticeText( () => "%s %I64u", { ...last, arguments: [ "first", "9007199254740993" ] } ),
					last.dialog ? "" : "first 9007199254740993"
				);
			} else {assert.deepEqual(
					notices,
					defined( before ).notices,
					"native silence or explicit unresolved modal never fabricates a guide"
				);}
			if ( after ) {
				for ( const field of [ "inventory", "vitals", "localGid", "casts" ] ) {
					assert.deepEqual(
						after[field],
						defined( before )[field],
						`${packet.writer}:${packet.code}: ${field} changed on rejection`
					);
				}
			}
		}
	}
});

test("COS formatted warning preserves separate guide and banner lookup semantics", () => {
	const n = resolveNativeNotice( 12, 8 ).notice;
	const copy =
		k => ({ "UIIT_MSG_COSERR_TOO_FAR_FROM_TRADECART": "Distance %d", "Distance 100": "Translated banner" }[k] ??
			"");
	assert.equal( noticeText( copy, n ), "Distance 100" );
	assert.equal( noticeText( copy, n, "banner" ), "Translated banner" );
	const missing = k => k === "UIIT_MSG_COSERR_TOO_FAR_FROM_TRADECART" ? "Distance %d" : "";
	assert.equal( noticeText( missing, n, "banner" ), "" );
	assert.equal( noticeText( missing, n ), "Distance 100" );
});
test("guild donation ack formats its amount without spending points again", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	const before = g.take();
	const payload = Buffer.alloc( 5 );
	payload[0] = 1;
	payload.writeUInt32LE( 4294967295, 1 );
	g.receive( { opcode: 0xb40f, payload }, 0 );
	const after = g.take(), n = defined( defined( after ).notices ).at( -1 );
	assert.equal( defined( n ).key, "UIIT_MSG_GUILD_GP_SUBSCRIPION_RESULT" );
	assert.equal( defined( n ).nativeType, 0 );
	assert.equal( defined( n ).banner, undefined );
	assert.equal( noticeText( () => "Contributed [%d]GP.", n ), "Contributed [-1]GP." );
	assert.deepEqual( defined( after ).progression, defined( before ).progression );
	assert.deepEqual( defined( defined( after ).social ).guild, defined( defined( before ).social ).guild );
	assert.throws( () => g.receive( { opcode: 0xb40f, payload: payload.subarray( 0, 4 ) }, 0 ) );
	assert.equal( g.take(), null );
});

test("notice integer formats preserve native Windows signedness and width", () => {
	const n = {
		key: "fixture",
		value: 0,
		arguments: [ "4294967295", "-1", "4294967295", "18446744073709551615", "18446744073709551615" ]
	};
	assert.equal( noticeText( () => "%d %u %ld %I64d %I64u", n ), "-1 4294967295 -1 -1 18446744073709551615" );
});

test("empty native warning leaves the previous banner and timer intact", async () => {
	const { createNoticeBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
	const b = createNoticeBanner();
	const copy = k =>
		k === "old" ? "Existing warning" : k === "UIIT_MSG_COSERR_TOO_FAR_FROM_TRADECART" ? "Distance %d" : "";
	const first = { key: "old", value: 0, banner: true, sequence: 1 },
		empty = { ...resolveNativeNotice( 12, 8 ).notice, sequence: 2 };
	b.step( [ first ], 0, true, copy );
	assert.equal( b.value( copy ), "Existing warning" );
	b.step( [ first, empty ], 1000, true, copy );
	assert.equal( b.value( copy ), "Existing warning" );
	b.step( [], 7000, true, copy );
	assert.equal( b.alpha(), 0, "ignored notice did not extend the old warning" );
	const cold = createNoticeBanner();
	cold.step( [ first ], 0, false, () => "" );
	assert.equal( cold.value( copy ), "" );
	cold.step( [ first ], 100, true, copy );
	assert.equal( cold.value( copy ), "Existing warning" );
});

test("COS zero-code result discards its tail before selector-specific parsing", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	g.take();
	for ( const selector of [ 2, 8, 99 ] ) {
		for ( const tail of [ [], [ 1, 2, 3, 4, 5 ] ] ) {
			assert.equal(
				g.receive( { opcode: 0xb69e, payload: Uint8Array.from( [ 2, selector, 0, 7, 0, 0, 0, ...tail ] ) }, 0 ),
				true
			);
			assert.equal( g.take(), null, "native no-op must not replace COS result or publish messages" );
		}
	}
	for ( let n = 0; n < 7; n++ ) {
		assert.throws( () =>
			g.receive( { opcode: 0xb69e, payload: Uint8Array.of( 2, 8, 0, 7, 0, 0, 0 ).subarray( 0, n ) }, 0 )
		);
	}
});
