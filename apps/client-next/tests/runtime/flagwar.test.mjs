/*
===========================================================================

flagwar.test.mjs - tests for flagwar.ts, targeting.ts and npc-talk.ts

The Capture the Flag wire (0x30A9 in, 0x7753 out): the strict decoder,
the notice table, the grant byte the talk row reads, and the row itself.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, "src/engine", file + ".ts" ) ).href );
}
const { decodeFlagWarEvent, flagWarNotice, FLAG_WAR_LEVEL_MIN, FLAG_WAR_MATCH_MS } = await load(
	"foundation/gameplay/flagwar"
);
const { createTargeting } = await load(
	"runtime/simulation/worker/session/world/gameplay/targeting/targeting"
);
const { npcTalkLayout } = await load( "foundation/ui/npc-talk" );

const named = ( tag, sub, name ) => {
	const payload = [ tag, sub, name.length & 0xff, (name.length >> 8) & 0xff ];
	for ( const ch of name ) payload.push( ch.charCodeAt( 0 ) );
	return new Uint8Array( payload );
};

test("plain event types decode strictly and the timer is the authored 20 minutes", () => {
	assert.equal( FLAG_WAR_LEVEL_MIN, 20 );
	assert.equal( FLAG_WAR_MATCH_MS, 0x124f80 );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x02 ] ) ), { type: "standby10" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x0d ] ) ), { type: "standby5" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x0e ] ) ), { type: "standby1" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x04 ] ) ), { type: "gameStart" } );
	// The mission start is three bytes: the type and a u16 team value.
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x08, 1, 0 ] ) ), { type: "missionStart", team: 1 } );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0x08 ] ) ) );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0x08, 1 ] ) ) );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x05 ] ) ), { type: "timerStop" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0x09 ] ) ), { type: "timerStop" } );
	const carrier = new Uint8Array( 5 );
	carrier[0] = 0x0a;
	new DataView( carrier.buffer ).setUint32( 1, 4242, true );
	assert.deepEqual( decodeFlagWarEvent( carrier ), { type: "carrier", gid: 4242 } );
	// Unknown plain types and subtypes read as null, never as an event.
	assert.equal( decodeFlagWarEvent( new Uint8Array( [ 0x03 ] ) ), null );
	assert.equal( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x16 ] ) ), null );
	assert.equal( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x17 ] ) ), null );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( 0 ) ) );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0x02, 0x00 ] ) ) );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0x0a, 1, 2 ] ) ) );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0xff ] ) ) );
});

test("subtype frames decode their payloads and refuse trailing bytes", () => {
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x00 ] ) ), { type: "joinResult" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x05 ] ) ), { type: "levelError" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x06 ] ) ), { type: "gameCancel" } );
	assert.deepEqual( decodeFlagWarEvent( named( 0xff, 0x09, "Ragnar" ) ), { type: "pickupOurs", name: "Ragnar" } );
	assert.deepEqual( decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x0a ] ) ), { type: "pickupTheirs" } );
	const key = new Uint8Array( [ 0xff, 0x0b, 7, 0, 0, 0, 1, 3, 0, 65, 66, 67 ] );
	assert.deepEqual( decodeFlagWarEvent( key ), { type: "masterKey", gid: 7, team: 1, name: "ABC" } );
	const elapsed = new Uint8Array( 6 );
	elapsed[0] = 0xff;
	elapsed[1] = 0x0e;
	new DataView( elapsed.buffer ).setUint32( 2, 60000, true );
	assert.deepEqual( decodeFlagWarEvent( elapsed ), { type: "elapsed", ms: 60000 } );
	const scored = new Uint8Array( [ 0xff, 0x10, 1, 2, 0, 65, 66 ] );
	assert.deepEqual( decodeFlagWarEvent( scored ), { type: "missionScored", team: 1, name: "AB" } );
	assert.throws( () => decodeFlagWarEvent( new Uint8Array( [ 0xff, 0x00, 0x00 ] ) ) );
	assert.throws( () => decodeFlagWarEvent( named( 0xff, 0x09, "AB" ).subarray( 0, 5 ) ) );
});

test("notices name the native texts; timer and carrier events stay silent", () => {
	assert.equal( flagWarNotice( { type: "standby10" }, false, null )?.key, "UIIT_MSG_FLAGWAR_STANDBY_10" );
	assert.equal(
		flagWarNotice( { type: "joinResult" }, true, null )?.key,
		"UIIT_MSG_FLAGWAR_GAME_JOIN_SUCCES"
	);
	assert.equal(
		flagWarNotice( { type: "joinResult" }, false, null )?.key,
		"UIIT_MSG_FLAGWAR_GAME_JOIN_CANCEL"
	);
	assert.equal(
		flagWarNotice( { type: "levelError" }, false, null )?.key,
		"UIIT_MSG_FLAGWAR_GAME_JOIN_LEVEL_ERORR"
	);
	assert.deepEqual( flagWarNotice( { type: "pickupOurs", name: "Ragnar" }, false, null )?.arguments, [ "Ragnar" ] );
	assert.equal(
		flagWarNotice( { type: "missionScored", team: 1, name: "AB" }, false, 1 )?.key,
		"UIIT_MSG_FLAGWAR_FLAG_MISSION_CLEAR"
	);
	assert.equal(
		flagWarNotice( { type: "missionScored", team: 1, name: "AB" }, false, 0 )?.key,
		"UIIT_MSG_FLAGWAR_FLAG_MISSION_FAIL"
	);
	assert.equal( flagWarNotice( { type: "missionScored", team: 1, name: "AB" }, false, null ), null );
	// Team-addressed notices speak only on the named team.
	assert.equal(
		flagWarNotice( { type: "missionStart", team: 1 }, false, 1 )?.key,
		"UIIT_MSG_FLAGWAR_GAME_START_MISSION"
	);
	assert.equal( flagWarNotice( { type: "missionStart", team: 1 }, false, 0 ), null );
	assert.equal(
		flagWarNotice( { type: "masterKey", gid: 7, team: 1, name: "AB" }, false, 0 ),
		null
	);
	assert.equal( flagWarNotice( { type: "elapsed", ms: 1 }, false, null ), null );
	assert.equal( flagWarNotice( { type: "carrier", gid: 1 }, false, null ), null );
	assert.equal( flagWarNotice( { type: "timerStop" }, false, null ), null );
});

test("the NPC grant's trailing byte surfaces as the flag-war mode and dies with the grant", () => {
	const sent = [];
	const targeting = createTargeting( frame => sent.push( frame ) );
	targeting.select( 777, 0, "npc" );
	assert.equal( sent[0]?.opcode, 0x745a );
	const grant = new Uint8Array( 11 );
	grant[0] = 1;
	new DataView( grant.buffer, grant.byteOffset, grant.byteLength ).setUint32( 1, 777, true );
	grant[10] = 2;
	assert.equal( targeting.receive( 0xb45a, grant ), true );
	assert.equal( targeting.state().targetFlagWarMode, 2 );
	targeting.release( 0 );
	assert.equal( targeting.state().targetFlagWarMode, undefined );
	assert.equal( targeting.receive( 0xb4b3, new Uint8Array( [ 1 ] ) ), true );
});

test("the talk menu offers sign-up or cancel only for the registration NPC's mode", () => {
	const layout = /** @type {any} */ ({
		GDR_NT_TALKBOX: { rect: [ 0, 0, 300, 400 ] },
		GDR_NPCTALK_SCROLL: { rect: [ 300, 0, 16, 400 ] }
	});
	const state = /** @type {any} */ ({ phase: "menu", gid: 7 });
	const rows = flagWarMode =>
		npcTalkLayout( {
			state,
			layout,
			origin: [ 0, 0 ],
			copy: key => key,
			measure: () => 8,
			draw: () => [],
			size: () => undefined,
			hover: null,
			pressed: null,
			top: 0,
			flagWarMode
		} ).controls.map( control => control.id );
	assert.ok( !rows( 0 ).includes( "npc-flagwar" ) );
	const joined = rows( 1 );
	assert.ok( joined.includes( "npc-flagwar" ) );
	assert.ok( rows( 2 ).includes( "npc-flagwar" ) );
	// The row rides after the service menu, ahead of TalkEnd.
	assert.ok( joined.indexOf( "npc-flagwar" ) < joined.indexOf( "npc-talkend" ) );
});

test("a 9-byte grant is a teleport gate even when the click guessed otherwise", () => {
	const targeting = createTargeting( () => {} );
	targeting.select( 778, 0, "npc" );
	const grant = new Uint8Array( 9 );
	grant[0] = 1;
	const view = new DataView( grant.buffer, grant.byteOffset, grant.byteLength );
	view.setUint32( 1, 778, true );
	view.setUint32( 5, 4, true );
	assert.equal( targeting.receive( 0xb45a, grant ), true );
	assert.equal( targeting.state().target, 778 );
	assert.equal( targeting.state().targetCapabilities, 4 );
});

test("a grant that matches no shape still refuses", () => {
	const targeting = createTargeting( () => {} );
	targeting.select( 779, 0, "npc" );
	const grant = new Uint8Array( 7 );
	grant[0] = 1;
	assert.throws( () => targeting.receive( 0xb45a, grant ), /Unsupported target grant/ );
});
