/*
===========================================================================

party-membership.test.mjs - the lines a 0x3E58 membership change prints

761870 pairs every roster mutation with a type-4 guide line (67a660):
a joined member prints UIIT_MSG_PARTY_JOIN, a departed one prints
UIIT_MSG_PARTY_LOGOUT / _SECEDE / _BOOTED by the reason byte (reason 3,
SERVER_MIGRATION, is a debug-only print), a broken party prints
UIIT_MSG_PARTY_BROKEN and a new leader prints
UIIT_MSG_PARTY_HOST_ROLE_DELEGATED. The roster effects themselves are
pinned by party-travel.test.mjs; this suite pins the prints.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { socialPacket, emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const SELF_ID = 11;
const PEER_ID = 22;

const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ Buffer.byteLength( s ), 0, ...Buffer.from( s ) ];
const frame = payload => ({ opcode: 0x3e58, payload: Uint8Array.from( payload ) });

/*
================
member

One roster member as the roster holds it.
================
*/
function member( id, name ) {
	return { id, name, model: 0, level: 20, status: 0, region: 0, x: 0, y: 0, z: 0, war: 0 };
}

/*
================
partied

A two-member roster: local player 11 leading, peer 22 beside them.
================
*/
function partied() {
	return {
		...emptySocial( "Me" ),
		self: SELF_ID,
		leader: SELF_ID,
		members: [ member( SELF_ID, "Me" ), member( PEER_ID, "Other" ) ]
	};
}

/*
================
join

A 0x3E58 type-2 JOIN frame carrying one full member row (mask 0x37).
================
*/
function join( id, name, level ) {
	return frame( [
		2,
		0x37,
		...u32( id ),
		...str( name ),
		...u32( 1907 ),
		level,
		0x9a,
		1,
		1,
		10,
		0,
		0,
		0,
		20,
		0,
		...u32( 0 )
	] );
}

/*
================
leave

A 0x3E58 type-3 LEAVE frame: the member id and the reason byte.
================
*/
function leave( id, reason ) {
	return frame( [ 3, ...u32( id ), reason ] );
}

test("a joined member prints the join line with the row's name", () => {
	const after = socialPacket( emptySocial( "Me" ), join( PEER_ID, "Other", 20 ) );
	assert.deepEqual( after?.notice, { key: "UIIT_MSG_PARTY_JOIN", value: 0, nativeType: 4, arguments: [ "Other" ] } );
	assert.deepEqual( after?.members.map( m => m.id ), [ PEER_ID ] );
});

test("a replayed join still prints the join line", () => {
	// A JOIN for a member the roster already holds reaches this client
	// only as a replay on kept state after a resume; the native client
	// appends and prints unconditionally, so the line follows the row.
	const after = socialPacket( partied(), join( PEER_ID, "Other", 26 ) );
	assert.deepEqual( after?.notice, { key: "UIIT_MSG_PARTY_JOIN", value: 0, nativeType: 4, arguments: [ "Other" ] } );
	assert.deepEqual( after?.members.map( m => m.id ), [ SELF_ID, PEER_ID ] );
});

test("departures print by reason, and reasons 3 and unlisted print nothing", () => {
	const lines = [
		[ 1, "UIIT_MSG_PARTY_LOGOUT" ],
		[ 2, "UIIT_MSG_PARTY_SECEDE" ],
		[ 4, "UIIT_MSG_PARTY_BOOTED" ]
	];
	for ( const [reason, key] of lines ) {
		const after = socialPacket( partied(), leave( PEER_ID, reason ) );
		assert.deepEqual( after?.notice, { key, value: 0, nativeType: 4, arguments: [ "Other" ] } );
		assert.deepEqual( after?.members.map( m => m.id ), [ SELF_ID ] );
	}
	for ( const reason of [ 3, 9 ] ) {
		const after = socialPacket( partied(), leave( PEER_ID, reason ) );
		assert.equal( after?.notice, undefined );
		assert.deepEqual( after?.members.map( m => m.id ), [ SELF_ID ] );
	}
});

test("the local player's own departure prints with the local name and clears the roster", () => {
	const after = socialPacket( partied(), leave( SELF_ID, 2 ) );
	assert.deepEqual( after?.notice, { key: "UIIT_MSG_PARTY_SECEDE", value: 0, nativeType: 4, arguments: [ "Me" ] } );
	assert.deepEqual( after?.members, [] );
	assert.equal( after?.self, 0 );
});

test("a broken party prints the plain line and clears the roster", () => {
	const after = socialPacket( partied(), frame( [ 1, 0 ] ) );
	assert.deepEqual( after?.notice, { key: "UIIT_MSG_PARTY_BROKEN", value: 0, nativeType: 4 } );
	assert.deepEqual( after?.members, [] );
	assert.equal( after?.self, 0 );
});

test("a delegated leader prints the delegation line with the promoted member's name", () => {
	const after = socialPacket( partied(), frame( [ 9, ...u32( PEER_ID ) ] ) );
	assert.deepEqual( after?.notice, {
		key: "UIIT_MSG_PARTY_HOST_ROLE_DELEGATED",
		value: 0,
		nativeType: 4,
		arguments: [ "Other" ]
	} );
	assert.equal( after?.leader, PEER_ID );
});

test("a member update still prints nothing", () => {
	// 0x3E58 type 6 with the level, status and position bits (0x26).
	const after = socialPacket(
		partied(),
		frame( [ 6, ...u32( PEER_ID ), 0x26, 30, 0x9a, 1, 1, 11, 0, 0, 0, 20, 0, ...u32( 0 ) ] )
	);
	assert.equal( after?.notice, undefined );
	assert.equal( after?.members.find( m => m.id === PEER_ID )?.level, 30 );
});
