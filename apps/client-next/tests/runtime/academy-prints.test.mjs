/*
===========================================================================

academy-prints.test.mjs - the training-camp lines #2286 restores

Four prints on flows that already exist: a camp member's log-on and
log-off (0x3AC5 status 5 flag 1, 5C54F0 state kind 1 via 67A600
type 0; the prints follow the announced latch, not the stored
byte), the join answers (0xB592 detail 0 and 2, 769CA0 via 67A600
type 4), the matching window's in-process label (62E540 mode 1;
mode 2's duplicate text is held), and
the guild master-transfer name lookup (5D3EB0 via 67A600 type 0,
guild lane). Channels and arms are ASM-verified against the client
binary (sha256 375e8682...); the texts are the retail rows.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { academyPacket, academyJoinLabel } = await import( "../../src/engine/foundation/gameplay/academy.ts" );
const packetOf = ( state, frame ) => defined( academyPacket( state, frame ), "academy packet" );
const { academyStatusNotice } = await import( "../../src/engine/foundation/gameplay/academy-notices.ts" );
const { guildTransferTargetMissing } = await import( "../../src/engine/foundation/gameplay/guild-manager.ts" );

const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};

/*
================
member

One camp member as the roster holds it.
================
*/
function member( id, name, offline, announced ) {
	return { id, name, kind: 2, level: 30, entryLevel: 20, model: 1907, offline, announced, honor: 0, location: "" };
}

/*
================
camp

An academy state holding the given roster.
================
*/
function camp( members ) {
	return { member: true, page: 0, pages: 0, rows: [], request: null, result: null, members, localMemberId: 1 };
}

/*
================
status

A 0x3AC5 status-5 frame: member id, flag, one value byte.
================
*/
function status( id, flag, value ) {
	return { opcode: 0x3ac5, payload: Uint8Array.from( [ 5, ...u32( id ), flag, value ] ) };
}

test("a connect byte prints while the announced latch is clear, then latches", () => {
	const state = camp( [ member( 2, "Peer", true, false ) ] );
	const frame = status( 2, 1, 0 );
	const next = packetOf( state, frame );
	assert.deepEqual( academyStatusNotice( state, next, frame ), {
		key: "UIIT_MSG_TC_CONNECT",
		value: 0,
		nativeType: 0,
		arguments: [ "Peer" ]
	} );
	assert.equal( next?.members?.[0]?.offline, false );
	assert.equal( next?.members?.[0]?.announced, true );
	// The latch is set now: the same byte again prints nothing.
	const after = packetOf( next, frame );
	assert.equal( academyStatusNotice( next, after, frame ), null );
	// A member the seed already showed online still announces the
	// first connect byte: the latch, not the stored byte, gates.
	const seeded = camp( [ member( 2, "Peer", false, false ) ] );
	assert.deepEqual( academyStatusNotice( seeded, packetOf( seeded, frame ), frame ), {
		key: "UIIT_MSG_TC_CONNECT",
		value: 0,
		nativeType: 0,
		arguments: [ "Peer" ]
	} );
});

test("a disconnect byte prints every time and clears the latch; other flags stay silent", () => {
	const state = camp( [ member( 2, "Peer", false, true ) ] );
	const frame = status( 2, 1, 1 );
	const next = packetOf( state, frame );
	assert.deepEqual( academyStatusNotice( state, next, frame ), {
		key: "UIIT_MSG_TC_DISCONNECT",
		value: 0,
		nativeType: 0,
		arguments: [ "Peer" ]
	} );
	assert.equal( next?.members?.[0]?.announced, false );
	// No latch check on this leg: the same byte prints again.
	const after = packetOf( next, frame );
	assert.deepEqual( academyStatusNotice( next, after, frame ), {
		key: "UIIT_MSG_TC_DISCONNECT",
		value: 0,
		nativeType: 0,
		arguments: [ "Peer" ]
	} );
	// And the cleared latch lets the next connect byte print again.
	const back = status( 2, 1, 0 );
	assert.deepEqual( academyStatusNotice( after, packetOf( after, back ), back ), {
		key: "UIIT_MSG_TC_CONNECT",
		value: 0,
		nativeType: 0,
		arguments: [ "Peer" ]
	} );
	const level = status( 2, 2, 31 );
	assert.equal( academyStatusNotice( state, packetOf( state, level ), level ), null );
});

test("join answers print cancel and no-reply at type 4, and stay silent otherwise", () => {
	const state = camp( [] );
	const cases = [
		{ payload: [ 1, 0 ], expected: { key: "UIIT_MSG_TC_JOIN_CANCEL", value: 0, nativeType: 4 } },
		{ payload: [ 1, 2 ], expected: { key: "UIIT_MSG_TC_JOIN_NOREPLY", value: 0, nativeType: 4 } },
		{ payload: [ 1, 1 ], expected: null },
		{ payload: [ 2, 0 ], expected: null }
	];
	for ( const entry of cases ) {
		const frame = { opcode: 0xb592, payload: Uint8Array.from( entry.payload ) };
		assert.deepEqual( academyStatusNotice( state, packetOf( state, frame ), frame ), entry.expected );
	}
});

test("the matching window labels a join in process with the progress text and nothing else", () => {
	const rows = [
		{ id: 7, kind: 0, detail: "", level: 80, model: 1907, name: "MasterM", students: 0, grade: 0, graduates: 0 }
	];
	assert.deepEqual(
		academyJoinLabel( { ...camp( [] ), rows, request: { kind: "join", id: 7 } } ),
		{ key: "UIIT_STT_PARTYMATCH_JOIN_PROGRESS", campName: "MasterM" }
	);
	assert.equal( academyJoinLabel( camp( [] ) ), null );
	assert.equal( academyJoinLabel( { ...camp( [] ), request: { kind: "page", id: 0 } } ), null );
});

test("a transfer target that left the roster is missing, a held one is not", () => {
	const guild = {
		members: [ 1, 2 ].map( id => ({
			id,
			name: "Member" + id,
			grade: 0,
			level: 1,
			donated: 0,
			permissions: 0,
			grant: "",
			model: 0,
			role: 0,
			offline: 0
		}) )
	};
	assert.equal( guildTransferTargetMissing( guild, 2 ), false );
	assert.equal( guildTransferTargetMissing( guild, 99 ), true );
	assert.equal( guildTransferTargetMissing( null, 2 ), true );
});
