/*
===========================================================================

guild-position.test.mjs - the box a 0xB65F position answer raises

The 0xB65F response (7689A0) raises a Confirm-titled box (6888C0,
UIIT_STT_CONFIRM_BOX) carrying the completion when a fortress
position is granted (UIIT_MSG_FORT_POSITION_GRANT_COMPLETE, naming
the member and the position title) or removed
(UIIT_MSG_FORT_POSITION_REMOVE_COMPLETE, naming the member): the
handler's call census has no system-line or banner print at all.
A refusal already prints through the category-0x10 arm and is
covered by native-notice-dispatch.test.mjs. The roster merge
itself predates this suite; what is pinned here is the box on the
success arm, in the dialog shape.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { socialPacket, emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const socialPacketOf = ( state, frame ) => defined( socialPacket( state, frame ), "social frame" );

const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ Buffer.byteLength( s ), 0, ...Buffer.from( s ) ];

/*
================
member

One guild member as the roster holds it.
================
*/
function member( id, name, role ) {
	return { id, name, grade: 1, level: 20, donated: 0, permissions: 0, grant: "", model: 1907, role, offline: 0 };
}

/*
================
guilded

A guild state holding members 1 (master) and 2 (peer, no position).
================
*/
function guilded() {
	return {
		...emptySocial( "Echo" ),
		guild: {
			id: 7,
			name: "Alpha",
			level: 2,
			gp: 0,
			subject: "",
			contents: "",
			crest: 0,
			members: [ member( 1, "Echo", 1 ), member( 2, "Peer", 0 ) ]
		}
	};
}

/*
================
positionAck

A 0xB65F success body: result 1, the target jid twice, the role byte.
================
*/
function positionAck( jid, role ) {
	return { opcode: 0xb65f, payload: Uint8Array.from( [ 1, ...u32( jid ), ...u32( jid ), role ] ) };
}

test("granting a position raises the completion box with the member and the title", () => {
	const after = socialPacketOf( guilded(), positionAck( 2, 2 ) );
	assert.deepEqual( after.notice, {
		key: "",
		value: 0,
		bannerOnly: true,
		dialog: { title: "UIIT_STT_CONFIRM_BOX", lines: [ "UIIT_MSG_FORT_POSITION_GRANT_COMPLETE" ] },
		arguments: [ "Peer", "" ],
		localizedArguments: [ null, "UIIT_STT_FORT_GUILD_SUBCOMMANDER" ]
	} );
	assert.equal( after.guild?.members.find( m => m.id === 2 )?.role, 2 );
});

test("removing a position raises the removal box with the member", () => {
	const state = guilded();
	state.guild.members[1] = member( 2, "Peer", 4 );
	const after = socialPacketOf( state, positionAck( 2, 0 ) );
	assert.deepEqual( after.notice, {
		key: "",
		value: 0,
		bannerOnly: true,
		dialog: { title: "UIIT_STT_CONFIRM_BOX", lines: [ "UIIT_MSG_FORT_POSITION_REMOVE_COMPLETE" ] },
		arguments: [ "Peer" ]
	} );
	assert.equal( after.guild?.members.find( m => m.id === 2 )?.role, 0 );
});

test("a position answer for a member the roster does not hold raises nothing", () => {
	const after = socialPacketOf( guilded(), positionAck( 99, 2 ) );
	assert.equal( after.notice, undefined );
	assert.deepEqual( after.guild?.members.map( m => m.role ), [ 1, 0 ] );
});

test("a name grant still merges the grant string and raises nothing", () => {
	const frame = { opcode: 0xb2bc, payload: Uint8Array.from( [ 1, ...u32( 2 ), ...u32( 2 ), ...str( "Officer" ) ] ) };
	const after = socialPacketOf( guilded(), frame );
	assert.equal( after.notice, undefined );
	assert.equal( after.guild?.members.find( m => m.id === 2 )?.grant, "Officer" );
});
