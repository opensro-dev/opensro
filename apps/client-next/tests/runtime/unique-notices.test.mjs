/*
===========================================================================

unique-notices.test.mjs - tests for unique-notices.ts, gameplay.ts,
messages.ts, unique-banner.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
import path from "node:path";
import { serverGameDataRoot } from "../../../../scripts/build/world/paths.mjs";
const { uniqueNotice, uniqueReferences } = await import( "../../src/engine/foundation/gameplay/unique-notices.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { noticeText, createHudMessages } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );
const { createUniqueBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
const symbols = [ "CH_TIGERWOMAN", "KK_ISYUTARU", "OA_URUCHI", "TK_BONELORD", "RM_TAHOMET", "EU_KERBEROS" ];
const refObjSnapshot = symbols.map( ( s, i ) => ({
	refObjId: i + 1,
	kind: "monster",
	nameStrId: "SN_MOB_" + s,
	name: "Monster " + i
}) );
const refs = uniqueReferences( { refObjSnapshot } );
/*
================
packet
================
*/
function packet( kind, id, killer = "asd2" ) {
	const name = new TextEncoder().encode( killer );
	return Uint8Array.from( [ kind, id, 0, 0, 0, ...(kind === 6 ? [ name.length, 0, ...name ] : []) ] );
}

test("every shipped unique reference resolves appearance and death text without a visible entity", async () => {
	const dir = path.join( serverGameDataRoot, "textdata" );
	const names = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/text/textdataname.en.json", "utf8" ) )
		.entries;
	const copy = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) )
		.entries;
	const rows = [];
	for ( const file of (await readdir( dir )).filter( f => /^characterdata_.*\.txt$/.test( f ) ) ) {
		for (
			const line of (await readFile( dir + "/" + file )).toString( "utf16le" ).replace( /^\uFEFF/, "" ).split(
				/\r?\n/
			)
		) {
			const f = line.split( "\t" );
			if ( f[0] === "1" && f[2]?.startsWith( "MOB_" ) && Number( f[15] ) === 3 ) {
				rows.push( { refObjId: Number( f[1] ), kind: "monster", nameStrId: f[5], name: names[f[5]] } );
			}
		}
	}
	assert.equal( rows.length, 19 );
	const refs = uniqueReferences( { refObjSnapshot: rows } );
	for ( const row of rows ) {
		for ( const [kind, killer] of [ [ 5, "" ], [ 6, "asd2" ], [ 6, "???" ] ] ) {
			const p = packet( kind, 0, killer );
			new DataView( p.buffer ).setUint32( 1, row.refObjId, true );
			const n = uniqueNotice( 0x3058, p, refs );
			assert.ok( n, row.nameStrId );
			assert.ok( copy[n.key], n.key );
			const rendered = noticeText( key => copy[key], n );
			assert.ok( rendered.trim() );
			assert.ok( !rendered.includes( "%s" ) );
			assert.ok( !rendered.includes( "undefined" ) );
		}
	}
});
test("all five native special branches and generic appear, kill, disappear wording", () => {
	const suffix = [ "TIGER_GIRL", "IYUTARU", "URRUCHI", "BONELORD", "TAHOMET", "UNIC" ];
	for ( let i = 0; i < 6; i++ ) {
		const born = uniqueNotice( 0x3058, packet( 5, i + 1 ), refs ),
			kill = uniqueNotice( 0x3058, packet( 6, i + 1 ), refs ),
			gone = uniqueNotice( 0x3058, packet( 6, i + 1, "???" ), refs );
		assert.equal( defined( born ).key, "UIIT_MSG_APPEAR_" + suffix[i] );
		assert.equal( defined( kill ).key, "UIIT_MSG_ANYONE_DEAD_" + suffix[i] );
		assert.equal( defined( gone ).key, "UIIT_MSG_DEAD_" + suffix[i] );
		assert.deepEqual( defined( kill ).arguments, i === 5 ? [ "asd2", "Monster 5" ] : [ "asd2" ] );
	}
	assert.equal(
		noticeText( () => "[%s]has killed [%s].", uniqueNotice( 0x3058, packet( 6, 6, "a%s" ), refs ) ),
		"[a%s]has killed [Monster 5]."
	);
	assert.equal( defined( uniqueNotice( 0x3058, packet( 6, 6, "?" ), refs ) ).key, "UIIT_MSG_ANYONE_DEAD_UNIC" );
});
test("truncations and trailing bytes reject before any gameplay notice is published", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( { refObjSnapshot } );
	for ( const full of [ packet( 5, 1 ), packet( 6, 6 ) ] ) {
		for ( let n = 0; n < full.length; n++ ) {
			assert.throws( () => g.receive( { opcode: 0x3058, payload: full.subarray( 0, n ) }, 0 ) );
		}
		assert.throws( () => g.receive( { opcode: 0x3058, payload: Uint8Array.from( [ ...full, 0 ] ) }, 0 ) );
	}
	assert.deepEqual( defined( g.take() ).notices, [] );
	assert.equal( uniqueNotice( 0x3058, packet( 5, 99 ), refs ), null );
	g.receive( { opcode: 0x3058, payload: packet( 6, 6 ) }, 0 );
	const notices = defined( g.take() ).notices;
	assert.equal( defined( notices ).length, 1 );
	assert.equal( defined( notices )[0].banner, true );
	const log = createHudMessages( () => 0 );
	const copy = () => "[%s]has killed [%s].";
	assert.equal( log.step( 0, [], 1, 0, notices, copy ).length, 1 );
	assert.equal( log.step( 100, [], 1, 0, notices, copy ).length, 1 );
});
test("banner waits for assets, replaces rather than queues, fades and resets across sessions", () => {
	const b = createUniqueBanner(), n = { ...uniqueNotice( 0x3058, packet( 5, 1 ), refs ), sequence: 1 };
	b.step( [ n ], 0, false );
	b.step( [ n ], 10000, false );
	assert.equal( b.alpha(), 0 );
	b.step( [ n ], 10000, true );
	assert.equal( b.alpha(), 1 );
	b.step( [ n ], 16000, true );
	assert.equal( b.alpha(), 127 / 255 );
	b.step( [ { ...n, sequence: 2 } ], 16000, true );
	assert.equal( b.alpha(), 1 );
	b.step( [ { ...n, sequence: 2 } ], 23000, true );
	assert.equal( b.alpha(), 0 );
	b.reset();
	assert.equal( b.value( x => x ), "" );
	b.step( [ n ], 24000, true );
	assert.equal( b.alpha(), 1 );
});
test("other 0x3058 kinds show their category 0x11 notice, kind 10 included", () => {
	// 7508C0: kind 1 is the thief's stolen-goods rule (#721).
	assert.equal(
		defined( uniqueNotice( 0x3058, Uint8Array.of( 1 ), refs ) ).key,
		"UIIT_MSG_JSERR_ROBBER_CANT_ENTER_TOWN_WITH_CART"
	);
	// The kind is the whole notice: native reads nothing after it.
	assert.equal(
		defined( uniqueNotice( 0x3058, Uint8Array.of( 1, 9, 9 ), refs ) ).key,
		"UIIT_MSG_JSERR_ROBBER_CANT_ENTER_TOWN_WITH_CART"
	);
	// 751155 via the 75118C table: kinds 8, 9 and 10 are the specialty
	// sell announcements, banner notices like the unique ones.
	for (
		const [kind, key] of [
			[ 8, "UIIT_MSG_SPECIALTY_ANNOUNCE_SELL_READY" ],
			[ 9, "UIIT_MSG_SPECIALTY_ANNOUNCE_SELL_START" ],
			[ 10, "UIIT_MSG_SPECIALTY_ANNOUNCE_SELL_END" ]
		]
	) {
		const notice = defined( uniqueNotice( 0x3058, Uint8Array.of( kind ), refs ) );
		assert.equal( notice.key, key );
		assert.equal( notice.banner, true );
	}
});
/*
================
badgePacket

A kind-7 frame: badge byte, then the length-prefixed guild name.
================
*/
function badgePacket( badge, name = "Haneul" ) {
	const bytes = new TextEncoder().encode( name );
	return Uint8Array.from( [ 7, badge, bytes.length, 0, ...bytes ] );
}
test("kind 7 prints the badge event line for bytes 1..4 and nothing outside them", async () => {
	const copy = JSON.parse(
		await readFile( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" )
	).entries;
	const keys = [
		"UIIT_MSG_EVENT_051021_BADGEGAIN_BLUEDRAGON",
		"UIIT_MSG_EVENT_051021_BADGEGAIN_PHOENIX",
		"UIIT_MSG_EVENT_051021_BADGEGAIN_WHITETIGER",
		"UIIT_MSG_EVENT_051021_BADGEGAIN_TURTLE"
	];
	for ( let badge = 1; badge <= 4; badge++ ) {
		const n = defined( uniqueNotice( 0x3058, badgePacket( badge ), refs ) );
		assert.equal( n.key, keys[badge - 1] );
		assert.equal( n.banner, true );
		assert.deepEqual( n.arguments, [ "Haneul" ] );
		assert.ok( copy[n.key], n.key );
		const rendered = noticeText( key => copy[key], n );
		assert.ok( rendered.startsWith( "Haneul guild obtained" ), rendered );
		assert.ok( !rendered.includes( "%s" ), rendered );
	}
	// 750FE7: a badge byte outside 1..4 formats nothing.
	for ( const badge of [ 0, 5, 255 ] ) {
		assert.equal( uniqueNotice( 0x3058, badgePacket( badge ), refs ), null );
	}
	// The name read is as strict as the killer read: no truncation, no tail.
	assert.throws( () => uniqueNotice( 0x3058, Uint8Array.of( 7 ), refs ) );
	assert.throws( () => uniqueNotice( 0x3058, Uint8Array.of( 7, 1, 9 ), refs ) );
	assert.throws( () => uniqueNotice( 0x3058, Uint8Array.from( [ ...badgePacket( 1 ), 0 ] ), refs ) );
});
