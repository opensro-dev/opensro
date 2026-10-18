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
/*
================
load
================
*/
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
	// ChatInput_HandleGmCommand matches with CRT_wcsicmp (0x50A77A).
	assert.deepEqual( [ ...gmRequest( "/makeitem ITEM_ETC_SPEED_UP_BASIC 20", rows ).payload ], [
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
	const frame = line => [ ...gmRequest( line, new Map(), 0, { monster } )?.payload ?? [] ];
	// 50A53E..50A57E: subcmd 6, u32 refObjID LE, u8 count, u8 type.
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 GIANT" ), [ 6, 0x34, 0x12, 0, 0, 20, 4 ] );
	// ChatInput_HandleGmCommand matches with CRT_wcsicmp (0x50A3D5).
	assert.deepEqual( frame( "/loadmonster MOB_CH_MANGNYANG 20 GIANT" ), [ 6, 0x34, 0x12, 0, 0, 20, 4 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 champ" ), [ 6, 0x34, 0x12, 0, 0, 20, 1 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 NORMAL" ), [ 6, 0x34, 0x12, 0, 0, 20, 0 ] );
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 20 OTHER" ), [ 6, 0x34, 0x12, 0, 0, 20, 0 ] );
	// No type token: the record's own type byte (50A4A6).
	assert.deepEqual( frame( "/LOADMONSTER MOB_CH_MANGNYANG 3" ), [ 6, 0x34, 0x12, 0, 0, 3, 1 ] );
	// The parsed integer's low byte, then 0 and 1 become 1 (50A51D..50A532).
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 0" )[5], 1 );
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 257" )[5], 1 );
	assert.equal( frame( "/LOADMONSTER MOB_CH_MANGNYANG 300" )[5], 44 );
	assert.equal( gmRequest( "/LOADMONSTER MOB_UNKNOWN 5", new Map(), 0, { monster } ), null );
	assert.equal( gmRequest( "/LOADMONSTER MOB_CH_MANGNYANG", new Map(), 0, { monster } ), null );
});

test("the quest tools compose the native subcmd 27..30 frames", () => {
	const frame = line => [ ...gmRequest( line )?.payload ?? [] ];
	const ascii = text => [ ...new TextEncoder().encode( text ) ];
	// 50CA69..50CBE6: discriminator, then 4FD5B0's u16 length and the name.
	assert.deepEqual( frame( "/INITQ QNO_CH_CHEF_1" ), [ 27, 13, 0, ...ascii( "QNO_CH_CHEF_1" ) ] );
	assert.deepEqual( frame( "/COMPQ QNO_CH_CHEF_1" ), [ 29, 13, 0, ...ascii( "QNO_CH_CHEF_1" ) ] );
	assert.deepEqual( frame( "/REMOVEQ QNO_CH_CHEF_1" ), [ 30, 13, 0, ...ascii( "QNO_CH_CHEF_1" ) ] );
	assert.deepEqual( frame( "/RESETQ" ), [ 28 ] );
	// The client matches the words without case (__wcsicmp), like the other new arms.
	assert.deepEqual( frame( "/initq QNO_CH_CHEF_1" ), [ 27, 13, 0, ...ascii( "QNO_CH_CHEF_1" ) ] );
	assert.deepEqual( frame( "/resetq" ), [ 28 ] );
	// The token counts are the parser's: a name for three, none for /RESETQ.
	assert.equal( gmRequest( "/INITQ" ), null );
	assert.equal( gmRequest( "/COMPQ A B" ), null );
	assert.equal( gmRequest( "/RESETQ QNO_CH_CHEF_1" ), null );
	assert.throws( () => gmRequest( "/REMOVEQ " + "Q".repeat( 128 ) ), /Invalid GM name/ );
	// The refactored name arms keep their frames.
	assert.deepEqual( frame( "/FINDUSER Remo" ), [ 1, 4, 0, ...ascii( "Remo" ) ] );
});

test("ZOE and MOBKILL compose the native subcmd 12 and 20 frames", () => {
	const refs = new Map( [ [ "MOB_CH_MANGNYANG", { refObjId: 0x1234, monsterType: 0 } ] ] );
	const monster = codename => refs.get( codename );
	const payload = (
		line,
		targetGid = 0
	) => [ ...gmRequest( line, new Map(), 0, { monster, targetGid } )?.payload ?? [] ];
	// 50A5A8..50A6DF: subcmd 12, u32 refObjID LE, u8 count (0/1 -> 1, 255 stays).
	assert.deepEqual( payload( "/ZOE MOB_CH_MANGNYANG 20" ), [ 12, 0x34, 0x12, 0, 0, 20 ] );
	assert.equal( payload( "/ZOE MOB_CH_MANGNYANG 0" )[5], 1 );
	assert.equal( payload( "/ZOE MOB_CH_MANGNYANG 255" )[5], 255 );
	assert.equal( payload( "/ZOE MOB_CH_MANGNYANG 300" )[5], 44 );
	assert.equal( gmRequest( "/ZOE MOB_UNKNOWN 5", new Map(), 0, { monster } ), null );
	assert.equal( gmRequest( "/ZOE MOB_CH_MANGNYANG", new Map(), 0, { monster } ), null );
	// 50B645..50B77E: subcmd 20, u32 selected target, u16 mode; no target, no frame.
	assert.deepEqual( payload( "/MOBKILL 0", 0x12345678 ), [ 20, 0x78, 0x56, 0x34, 0x12, 0, 0 ] );
	assert.deepEqual( payload( "/MOBKILL 1", 0x12345678 ), [ 20, 0x78, 0x56, 0x34, 0x12, 1, 0 ] );
	assert.equal( gmRequest( "/MOBKILL 0", new Map(), 0, { monster } ), null );
	assert.equal( gmRequest( "/MOBKILL 2", new Map(), 0, { monster, targetGid: 1 } ), null );
	assert.equal( gmRequest( "/MOBKILL", new Map(), 0, { monster, targetGid: 1 } ), null );
	assert.deepEqual( payload( "/zoe MOB_CH_MANGNYANG 2" ), [ 12, 0x34, 0x12, 0, 0, 2 ] );
	assert.deepEqual( payload( "/mobkill 1", 0x12345678 ), [ 20, 0x78, 0x56, 0x34, 0x12, 1, 0 ] );
	for ( const targetGid of [ 0, -1, 1.5, NaN, Infinity, 0x100000000 ] ) {
		assert.equal( gmRequest( "/MOBKILL 0", new Map(), 0, { targetGid } ), null );
	}
});

test("INSTANCE, flagworld, movetonpc, and SIEGE compose the native subcmd 19, 31, 32, 33, and 48 frames", () => {
	const frame = line => [ ...gmRequest( line )?.payload ?? [] ];
	const ascii = text => [ ...new TextEncoder().encode( text ) ];
	// 50B56C..50B627: /INSTANCE worldId layerId -> [19, u16(worldId), u16(layerId)].
	assert.deepEqual( frame( "/INSTANCE 2 0" ), [ 19, 2, 0, 0, 0 ] );
	assert.deepEqual( frame( "/instance 258 513" ), [ 19, 2, 1, 1, 2 ] );
	assert.equal( gmRequest( "/INSTANCE 2" ), null );
	assert.equal( gmReply( Uint8Array.of( 1, 19 ) ), null );
	assert.equal( gmReply( Uint8Array.of( 2, 19 ) ), null );
	// 50C88F..50C971: /flagworld n -> [0x30, u8(n)].
	assert.deepEqual( frame( "/flagworld 3" ), [ 0x30, 3 ] );
	assert.deepEqual( frame( "/FLAGWORLD 3" ), [ 0x30, 3 ] );
	assert.equal( gmRequest( "/flagworld" ), null );
	assert.equal( gmReply( Uint8Array.of( 1, 0x30 ) ), null );
	// 50C982..50CA57: /movetonpc codename -> [31, u16(len), ASCII codename].
	assert.deepEqual( frame( "/movetonpc NPC_CH_SMITH" ), [ 31, 12, 0, ...ascii( "NPC_CH_SMITH" ) ] );
	assert.deepEqual( frame( "/MOVETONPC NPC_CH_SMITH" ), [ 31, 12, 0, ...ascii( "NPC_CH_SMITH" ) ] );
	assert.equal( gmRequest( "/movetonpc" ), null );
	assert.equal( gmReply( Uint8Array.of( 1, 31 ) ), null );
	assert.equal( gmReply( Uint8Array.of( 2, 31 ) ), null );
	// 50C1B6..50C66B: /SIEGE period (0x21) and structure (0x20) commands.
	assert.deepEqual( frame( "/SIEGE STARTWAR" ), [ 0x21, 1 ] );
	assert.deepEqual( frame( "/siege startwar" ), [ 0x21, 1 ] );
	assert.deepEqual( frame( "/SIEGE ENDWAR" ), [ 0x21, 2 ] );
	assert.deepEqual( frame( "/SIEGE STARTTAX" ), [ 0x21, 3 ] );
	assert.deepEqual( frame( "/SIEGE ENDTAX" ), [ 0x21, 4 ] );
	assert.deepEqual( frame( "/SIEGE STARTREQ" ), [ 0x21, 5 ] );
	assert.deepEqual( frame( "/SIEGE ENDREQ" ), [ 0x21, 6 ] );
	assert.deepEqual( frame( "/SIEGE STRUCT_BUILD 105" ), [ 0x20, 0x0a, 1, 0, 0, 0, 105, 0, 0, 0 ] );
	assert.deepEqual( frame( "/siege struct_build 105" ), [ 0x20, 0x0a, 1, 0, 0, 0, 105, 0, 0, 0 ] );
	assert.deepEqual( frame( "/SIEGE STRUCT_REPAIR 88" ), [ 0x20, 0x0c, 1, 0, 0, 0, 88, 0, 0, 0 ] );
	assert.deepEqual( frame( "/SIEGE STRUCT_UPGRADE 88 2" ), [ 0x20, 0x0b, 1, 0, 0, 0, 88, 0, 0, 0, 2 ] );
	assert.deepEqual( frame( "/siege struct_upgrade 88 2" ), [ 0x20, 0x0b, 1, 0, 0, 0, 88, 0, 0, 0, 2 ] );
	assert.deepEqual( frame( "/SIEGE GATE 88 0" ), [ 0x20, 0x15, 1, 0, 0, 0, 88, 0, 0, 0, 0 ] );
	assert.deepEqual( frame( "/siege gate 88 1" ), [ 0x20, 0x15, 1, 0, 0, 0, 88, 0, 0, 0, 2 ] );
	assert.deepEqual( frame( "/SIEGE GATE 88 9" ), [ 0x20, 0x15, 1, 0, 0, 0, 88, 0, 0, 0, 2 ] );
	assert.equal( gmRequest( "/SIEGE UNKNOWN" ), null );
	assert.equal( gmRequest( "/SIEGE STRUCT_BUILD" ), null );
	assert.equal( gmRequest( "/SIEGE GATE 88" ), null );
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0x20 ) ), { console: false, text: "SiegeManager MSG Result - Ok." } );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0x20 ) ), {
		console: false,
		text: "SiegeManager MSG Result - Fail."
	} );
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0x21 ) ), { console: false, text: "SiegeManager MSG Result - Ok." } );
});
