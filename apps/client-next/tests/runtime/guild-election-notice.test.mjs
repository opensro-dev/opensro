/*
===========================================================================

guild-election-notice.test.mjs - the guild election reminder at world entry

The guild record must carry the Event Guide dialog through the gameplay
notice queue. The native predicate selects the master-release vote's ID;
it does not test the remaining time or a previous session's guild state.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { emptySocial, socialPacket } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const GUILD_RECORD = 0x32c4;
const GUILD_VOTE_UPDATE = 0x3a6c;
const VOTE_TIME_UPDATE = 5;
const REMINDER = {
	key: "",
	value: 0,
	bannerOnly: true,
	dialog: { title: "UIIT_STT_EVENTGUIDE", lines: [ "UIIT_CTL_MASTERRELEASE_NOTICE" ] }
};

/*
================
u32
================
*/
function u32( value ) {
	const bytes = Buffer.alloc( 4 );
	bytes.writeUInt32LE( value );
	return [ ...bytes ];
}

/*
================
string
================
*/
function string( value ) {
	const bytes = Buffer.from( value );
	return [ bytes.length, 0, ...bytes ];
}

/*
================
guildRecord

A complete guild record with an empty member roster and the supplied votes.
================
*/
function guildRecord( votes = [] ) {
	return {
		opcode: GUILD_RECORD,
		payload: Uint8Array.from( [
			...u32( 7 ),
			...string( "Guild" ),
			1,
			...u32( 100 ),
			...string( "Notice" ),
			...string( "Contents" ),
			...u32( 0 ),
			0,
			0,
			votes.length,
			...votes.flatMap( vote => [ ...u32( vote.id ), vote.kind, ...u32( vote.remainingMs ) ] )
		] )
	};
}

/*
================
The native predicate reads the kind-zero vote's ID, never its clock.
================
*/
test("a master-release vote raises the Event Guide dialog regardless of remaining time", () => {
	for ( const remainingMs of [ 0, 1, 600000, 0xffffffff ] ) {
		const votes = [ { id: 0xffffffff, kind: 0, remainingMs } ];
		const state = defined( socialPacket( emptySocial( "Member" ), guildRecord( votes ) ) );
		assert.deepEqual( state.notice, REMINDER );
		assert.deepEqual( state.guild?.votes, votes );
	}
});

/*
================
An absent, different-kind or zero-ID vote does not open the dialog.
================
*/
test("only a nonzero master-release vote ID raises the reminder", () => {
	for (
		const votes of [ [], [ { id: 7, kind: 1, remainingMs: 600000 } ], [ { id: 0, kind: 0, remainingMs: 600000 } ] ]
	) {
		const state = defined( socialPacket( emptySocial( "Member" ), guildRecord( votes ) ) );
		assert.equal( state.notice, undefined );
	}
	const mixed = [ { id: 7, kind: 1, remainingMs: 600000 }, { id: 8, kind: 0, remainingMs: 600000 } ];
	assert.deepEqual( defined( socialPacket( emptySocial(), guildRecord( mixed ) ) ).notice, REMINDER );
});

/*
================
A new record owns the decision even when the previous record had a vote.
================
*/
test("a replacement guild record clears the old vote and its reminder", () => {
	const active = defined( socialPacket( emptySocial(), guildRecord( [ { id: 7, kind: 0, remainingMs: 600000 } ] ) ) );
	const quiet = defined( socialPacket( active, guildRecord() ) );
	assert.equal( quiet.guild?.votes, undefined );
	assert.equal( quiet.notice, undefined );
	assert.deepEqual( active.notice, REMINDER );
});

/*
================
Only the complete entry record emits the reminder; deltas clear the outcome.
================
*/
test("a later vote clock update does not replay the entry reminder", () => {
	const active = defined( socialPacket( emptySocial(), guildRecord( [ { id: 7, kind: 0, remainingMs: 600000 } ] ) ) );
	const updated = defined( socialPacket( active, {
		opcode: GUILD_VOTE_UPDATE,
		payload: Uint8Array.of( VOTE_TIME_UPDATE, ...u32( 7 ), ...u32( 540000 ) )
	} ) );
	assert.equal( updated.notice, undefined );
	assert.equal( updated.guild?.votes?.[0].remainingMs, 540000 );
});

/*
================
Malformed records cannot publish a partial guild or a reminder.
================
*/
test("an incomplete or trailing-byte guild record publishes nothing", () => {
	const before = emptySocial( "Member" );
	const record = guildRecord( [ { id: 7, kind: 0, remainingMs: 600000 } ] );
	for ( let end = 0; end < record.payload.length; end++ ) {
		assert.throws( () => socialPacket( before, { ...record, payload: record.payload.subarray( 0, end ) } ) );
	}
	assert.throws( () =>
		socialPacket( before, {
			...record,
			payload: Uint8Array.of( ...record.payload, 0 )
		} ), /Trailing/ );
	assert.deepEqual( before, emptySocial( "Member" ) );
});

/*
================
The gameplay owner publishes the dialog once, without a chat line, and resets.
================
*/
test("the gameplay queue carries the reminder through entry and re-entry", () => {
	const game = createGameplay( () => {} );
	const record = guildRecord( [ { id: 7, kind: 0, remainingMs: 600000 } ] );
	game.receive( record, 100 );
	const entry = defined( game.take() );
	assert.deepEqual( entry.notices, [ { ...REMINDER, sequence: 1 } ] );
	assert.equal( entry.social?.notice, undefined );
	assert.equal( entry.chat?.lines.length ?? 0, 0 );
	game.receive( {
		opcode: GUILD_VOTE_UPDATE,
		payload: Uint8Array.of( VOTE_TIME_UPDATE, ...u32( 7 ), ...u32( 500000 ) )
	}, 200 );
	assert.deepEqual( defined( game.take() ).notices, entry.notices );
	game.reset();
	game.receive( guildRecord(), 300 );
	assert.equal( defined( game.take() ).notices?.length ?? 0, 0 );
	game.reset();
	game.receive( record, 400 );
	assert.deepEqual( defined( game.take() ).notices, [ { ...REMINDER, sequence: 2 } ] );
});
