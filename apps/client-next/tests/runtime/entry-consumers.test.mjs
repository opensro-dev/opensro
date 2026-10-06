/*
===========================================================================

entry-consumers.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { entryEnvironment, environmentPacket } = await load( "foundation/gameplay/event-environment.ts" );
const { gmRequest, gmReply } = await load( "foundation/gameplay/gm-command.ts" );
test("coordinate warp uses native waypoint wire without coordinate-axis swaps", () => {
	const frame = gmRequest( "/warp 25416 703 42.5 1575", new Map(), 12345 ), v = new DataView( frame.payload.buffer );
	assert.equal( frame.opcode, 0x75b6 );
	assert.equal( v.byteLength, 17 );
	assert.equal( v.getUint8( 0 ), 16 );
	assert.equal( v.getUint16( 1, true ), 25416 );
	assert.deepEqual( [
		v.getFloat32( 3, true ),
		v.getFloat32( 7, true ),
		v.getFloat32( 11, true ),
		v.getUint16( 15, true )
	], [ 703, 42.5, 1575, 12345 ] );
	for (
		const line of [
			"/warp 0 1 2 3",
			"/warp 65536 1 2 3",
			"/warp 1 NaN 2 3",
			"/warp 1 1e40 2 3",
			"/warp 1 1 2",
			"/warp 1 1 2 3 4"
		]
	) assert.equal( gmRequest( line ), null );
});
test("MAKEITEM resolves authored references and uses native low-byte clamping", () => {
	const rows = new Map( [ [ "ITEM_ETC_SPEED_UP_BASIC", {
		refObjId: 24198,
		codename: "ITEM_ETC_SPEED_UP_BASIC",
		typeFlags: 0x6c,
		maxStack: 20
	} ], [ "ITEM_SWORD", { refObjId: 99, codename: "ITEM_SWORD", typeFlags: 0x2c, maxStack: 1 } ] ] );
	assert.deepEqual( [ ...gmRequest( "/MAKEITEM ITEM_ETC_SPEED_UP_BASIC 20", rows ).payload ], [
		7,
		134,
		94,
		0,
		0,
		20
	] );
	assert.equal( gmRequest( "/MAKEITEM UNKNOWN 1", rows ), null );
	assert.equal( gmRequest( "/MAKEITEM ITEM_ETC_SPEED_UP_BASIC", rows ), null );
	assert.equal( gmRequest( "/MAKEITEM ITEM_ETC_SPEED_UP_BASIC 256", rows ).payload[5], 1 );
	assert.equal( gmRequest( "/MAKEITEM ITEM_SWORD 255", rows ).payload[5], 12 );
});
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const event = ( id, active ) => {
	const p = new Uint8Array( 5 );
	new DataView( p.buffer ).setUint32( 0, id, true );
	p[4] = active;
	return p;
};
test("entry event groups are keyed, copied, bounded and preserve duplicate announcements", () => {
	const ids = [ 5, 1, 1 ], entry = entryEnvironment( { character: { enterEventGroupIds: ids } } );
	ids[0] = 2;
	assert.deepEqual( entry.state.groups, { 1: 1, 5: 1 } );
	assert.equal( entry.notices.length, 2 );
	assert.deepEqual( entry.state.effective, { mode: 3, amount: 20 } );
	assert.deepEqual( entryEnvironment( { character: { enterEventGroupIds: [ 5 ] } } ).state.effective, {
		mode: 1,
		amount: 0
	} );
	for ( const ids of [ [ -1 ], [ 1.5 ], [ 2 ** 32 ], Array( 256 ).fill( 1 ), "1" ] ) {
		assert.throws( () => entryEnvironment( { character: { enterEventGroupIds: ids } } ) );
	}
});
test("event weather restores the latest server value and handles nonbinary states", () => {
	let s = entryEnvironment( { character: { enterEventGroupIds: [ 1 ] } } ).state;
	s = environmentPacket( s, 0x3bde, Uint8Array.of( 2, 91 ) ).state;
	assert.deepEqual( s.effective, { mode: 3, amount: 20 } );
	s = environmentPacket( s, 0x3347, event( 5, 0 ) ).state;
	assert.deepEqual( s.effective, { mode: 3, amount: 20 } );
	let end = environmentPacket( s, 0x3347, event( 1, 0 ) );
	assert.equal( end.notice, "UIIT_MSG_EVENT_END" );
	assert.deepEqual( end.state.effective, { mode: 2, amount: 91 } );
	s = environmentPacket( end.state, 0x3347, event( 1, 3 ) ).state;
	assert.deepEqual( s.effective, { mode: 2, amount: 91 } );
	s = environmentPacket( s, 0x3bde, Uint8Array.of( 9, 255 ) ).state;
	assert.deepEqual( s.server, { mode: 1, amount: 255 } );
	assert.deepEqual( s.effective, { mode: 3, amount: 20 } );
	const before = structuredClone( s );
	assert.throws( () => environmentPacket( s, 0x3347, Uint8Array.of( 1 ) ) );
	assert.deepEqual( s, before );
});
test("production gameplay consumes entry/live events and resets across sessions", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( { character: { enterEventGroupIds: [ 1, 5 ] } } );
	let s = g.take();
	assert.deepEqual( s.weather, { mode: 3, amount: 20 } );
	assert.equal( s.notices[0].key, "UIIT_MSG_EVENT_START" );
	g.receive( { opcode: 0x3bde, payload: Uint8Array.of( 2, 80 ) }, 0 );
	g.receive( { opcode: 0x3347, payload: event( 1, 0 ) }, 1 );
	s = g.take();
	assert.deepEqual( s.weather, { mode: 2, amount: 80 } );
	assert.equal( s.eventGroups[5], 1 );
	assert.equal( s.notices.at( -1 ).key, "UIIT_MSG_EVENT_END" );
	g.reset();
	g.bootstrap( {} );
	assert.deepEqual( g.take().eventGroups, {} );
	g.dispose();
});
test("GM request matching, integer widths and silent branches", () => {
	assert.deepEqual( [ ...gmRequest( "/FINDUSER Remo" ).payload ], [ 1, 4, 0, 82, 101, 109, 111 ] );
	for ( const [name, code] of [ [ "/INVISIBLE", 14 ], [ "/INVINCIBLE", 15 ] ] ) {
		assert.deepEqual( [ ...gmRequest( name ).payload ], [ code ] );
		assert.equal( gmReply( Uint8Array.of( 1, code ) ), null );
		assert.equal( gmReply( Uint8Array.of( 2, code ) ), null );
	}
	assert.equal( gmRequest( "/finduser Remo" ), null );
	assert.equal( gmRequest( "/FINDUSER a b" ), null );
	assert.equal( gmRequest( "/STAT x" ), null );
	assert.equal( gmRequest( "/INVISIBLE x" ), null );
	assert.deepEqual( [ ...gmRequest( "/INSTANCE 65537 -1" ).payload ], [ 19, 1, 0, 255, 255 ] );
	assert.deepEqual( [ ...gmRequest( "/SETTIME 24" ).payload ], [ 10, 0 ] );
});
test("GM acknowledgments retain message channels, alias failure tails and silent errors", () => {
	const str = ( result, code, text, tail = [] ) =>
		Uint8Array.of( result, code, text.length, 0, ...new TextEncoder().encode( text ), ...tail );
	assert.deepEqual( gmReply( str( 1, 1, "Remo" ) ), { console: false, text: "Remo" } );
	for (
		const [result, code, prefix] of [ [ 1, 25, "-> " ], [ 1, 26, "-> *" ], [ 2, 25, "Failed. -> *" ], [
			2,
			26,
			"Failed. -> "
		] ]
	) {
		assert.deepEqual( gmReply( str( result, code, "R", result === 2 ? [ 0 ] : [] ) ), {
			console: true,
			text: prefix + "R"
		} );
	}
	assert.throws( () => gmReply( str( 2, 25, "R" ) ) );
	assert.throws( () => gmReply( Uint8Array.of( 1, 4 ) ) );
	assert.equal( gmReply( Uint8Array.of( 2, 1 ) ), null );
	assert.equal( gmReply( Uint8Array.of( 2, 6 ) ), null );
	assert.equal( gmReply( Uint8Array.of( 2, 3 ) ).key, "UIIT_STT_ERR_COMMON_INVALID_TARGET" );
});
test("GM authority is independent of PC-room eligibility and resets", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	const local = {
		gid: 1,
		kind: "local-player",
		refObjId: 1907,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Fixture"
	};
	for ( const gm of [ false, true ] ) {
		for ( const pcRoomEvent of [ false, true ] ) {
			g.bootstrap( { character: { gmPrivilege: gm, pcRoomEvent } } );
			g.seed( local );
			const before = sent.length;
			g.command( { kind: "gm-command", line: "/FINDUSER R" }, 0, undefined, local );
			assert.equal( sent.length - before, Number( gm ) );
		}
	}
	g.reset();
	assert.equal( g.command( { kind: "gm-command", line: "/FINDUSER R" }, 0 ), null );
	g.dispose();
});

test("LOADMONSTER sends the native subcmd 6 frame with clamped count and resolved type", () => {
	const refs = new Map( [ [ "MOB_CH_MANGNYANG", { refObjId: 0x1234, monsterType: 1 } ] ] );
	const monster = codename => refs.get( codename );
	const frame = line => [ ...gmRequest( line, new Map(), 0, monster )?.payload ?? [] ];
	// 50A53E..50A57E: subcmd 6, u32 refObjID LE, u8 count, u8 type.
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 GIANT" ), [ 6, 0x34, 0x12, 0, 0, 20, 4 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 champ" ), [ 6, 0x34, 0x12, 0, 0, 20, 1 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 NORMAL" ), [ 6, 0x34, 0x12, 0, 0, 20, 0 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 OTHER" ), [ 6, 0x34, 0x12, 0, 0, 20, 0 ] );
	// No type token: the record's own type byte (50A4A6).
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 3" ), [ 6, 0x34, 0x12, 0, 0, 3, 1 ] );
	// The parsed integer's low byte, then 0 and 1 become 1 (50A51D..50A532).
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 0" )[5], 1 );
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 257" )[5], 1 );
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 300" )[5], 44 );
	assert.equal( gmRequest( "/LOADMONSTER MOB_UNKNOWN 5", new Map(), 0, monster ), null );
	assert.equal( gmRequest( "/LOADMONSTER MOB_CH_MANGNYANG", new Map(), 0, monster ), null );
});
