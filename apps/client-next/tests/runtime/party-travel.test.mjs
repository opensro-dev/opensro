/*
===========================================================================

party-travel.test.mjs - tests for core.ts and gameplay.ts bootstrap

A 0x3369 world transfer keeps the party: native keeps it in
g_CharacterDependentData, which only CPSMission_OnCreate clears, and the
server keeps the membership and resends nothing. The member deltas that
follow a teleport must find the roster ("Unknown party delta member").
A new character, or a cleared session, starts with no party.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );

const SELF_ID = 11;
const PEER_ID = 22;
// 0x3E58 type 6 with the level, status and position bits (the server's
// party.encodeMemberUpdate mask).
const MEMBER_UPDATE_MASK = 0x26;

const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ Buffer.byteLength( s ), 0, ...Buffer.from( s ) ];
const frame = ( opcode, p ) => ({ opcode, payload: Uint8Array.from( p ) });

/*
================
entry

One enter-world bootstrap for a character.
================
*/
function entry( name ) {
	return {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x694f, x: 10, y: 20, z: 30, angle: 0 } },
		character: { name, skills: [], quickSlots: [] }
	};
}

/*
================
partyRow

A full 0x35D6 roster row (mask 0x37: id, name, level, status, position).
================
*/
function partyRow( id, name ) {
	return [ 0x37, ...u32( id ), ...str( name ), ...u32( 1907 ), 20, 0x9a, 1, 1, 10, 0, 0, 0, 20, 0, ...u32( 0 ) ];
}

/*
================
memberUpdate

The ticker's once-a-second 0x3E58 type-6 row for one member.
================
*/
function memberUpdate( id, level ) {
	return frame( 0x3e58, [ 6, ...u32( id ), MEMBER_UPDATE_MASK, level, 0x9a, 1, 1, 11, 0, 0, 0, 20, 0, ...u32( 0 ) ] );
}

/*
================
partied

A world core whose character "Me" is in a two-member party.
================
*/
function partied() {
	const core = createWorldCore( () => {} );
	core.bootstrap( entry( "Me" ) );
	core.receive( frame( 0xb0d5, [ 1, ...u32( SELF_ID ) ] ), 0 );
	core.receive(
		frame( 0x35d6, [ 3, ...u32( SELF_ID ), 0, 2, ...partyRow( SELF_ID, "Me" ), ...partyRow( PEER_ID, "Other" ) ] ),
		0
	);
	return core;
}

/*
================
social

The social state the presentation holds after everything queued so far.
================
*/
function social( core, presentation ) {
	core.step( 0, false );
	for ( let batch = core.take(); batch; batch = core.take() ) {
		core.ack( batch.sequence );
		presentation.apply( batch );
	}
	return defined( defined( presentation.gameplay(), "gameplay" ).social, "social" );
}

test("a teleport keeps the party, so the next member delta applies", () => {
	for ( const reset of [ 0x3369, 0x366a ] ) {
		const core = partied(), presentation = createPresentation();
		assert.equal( social( core, presentation ).members.length, 2 );
		core.receive( frame( reset, [ 0x4f, 0x69 ] ), 1 );
		core.bootstrap( entry( "Me" ) );
		core.receive( memberUpdate( PEER_ID, 21 ), 2 );
		const after = social( core, presentation );
		assert.equal( after.self, SELF_ID );
		assert.equal( after.leader, SELF_ID );
		assert.deepEqual( after.members.map( m => m.id ), [ SELF_ID, PEER_ID ] );
		assert.equal( after.members[1].level, 21 );
		core.dispose();
		presentation.dispose();
	}
});

test("a cleared session or another character starts with no party", () => {
	const cleared = partied(), presentation = createPresentation();
	cleared.clear();
	cleared.bootstrap( entry( "Me" ) );
	assert.deepEqual( social( cleared, presentation ).members, [] );
	assert.throws( () => cleared.receive( memberUpdate( PEER_ID, 21 ), 1 ), /Unknown party delta member/ );
	cleared.dispose();

	const switched = partied(), other = createPresentation();
	switched.receive( frame( 0x3369, [ 0x4f, 0x69 ] ), 1 );
	switched.bootstrap( entry( "Someone" ) );
	assert.deepEqual( social( switched, other ).members, [] );
	switched.dispose();

	const fresh = partied(), third = createPresentation();
	fresh.bootstrap( entry( "Me" ) );
	assert.deepEqual( social( fresh, third ).members, [], "an entry no reset preceded starts over" );
	fresh.dispose();
});

test("a resumed transport keeps the party the server kept", () => {
	// Login order: the EnterWorld result, then the bootstrap's own 0x3369. A
	// resume repeats the EnterWorld result on the same session; the server
	// keeps the membership (WorldBound runs once per session).
	const core = createWorldCore( () => {} ), presentation = createPresentation();
	core.bootstrap( entry( "Me" ) );
	core.receive( frame( 0x3369, [ 0x4f, 0x69 ] ), 0 );
	core.receive( frame( 0xb0d5, [ 1, ...u32( SELF_ID ) ] ), 0 );
	core.receive(
		frame( 0x35d6, [ 3, ...u32( SELF_ID ), 0, 2, ...partyRow( SELF_ID, "Me" ), ...partyRow( PEER_ID, "Other" ) ] ),
		0
	);
	core.bootstrap( entry( "Me" ) );
	core.receive( memberUpdate( PEER_ID, 22 ), 1 );
	assert.equal( social( core, presentation ).members[1].level, 22 );
	core.dispose();
	presentation.dispose();
});
