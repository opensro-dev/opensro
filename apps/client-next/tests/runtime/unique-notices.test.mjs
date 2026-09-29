/*
===========================================================================

unique-notices.test.mjs - tests for unique-notices.ts, gameplay.ts,
messages.ts, unique-banner.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
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
	const names =
		JSON.parse( await readFile( "../../.generated/client-public/assets/text/textdataname.en.json", "utf8" ) )
			.entries;
	const copy =
		JSON.parse( await readFile( "../../.generated/client-public/assets/text/textuisystem.en.json", "utf8" ) )
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
	assert.equal( uniqueNotice( 0x3058, Uint8Array.of( 10 ), refs ), null );
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
