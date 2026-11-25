/*
===========================================================================

party-summon.test.mjs - the party summon wire (0x34C6 / 0xB4C6, #3918)

The leader reads the roster (rows keep the native status-cell bytes:
2 Possible, 3 Impossible), a refused request names its line through
the item-use error router's code map, a member answers the invitation
box, and the leader-side lines ride their UIIT keys. Encoders refuse
the commands the state cannot answer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { socialPacket, socialRequest, emptySocial, withoutSummonPrompt } = await import(
	"../../src/engine/foundation/gameplay/social.ts"
);
const { partySummonRefusalKey } = await import( "../../src/engine/foundation/gameplay/party-summon.ts" );
const { partySummonWindowLayout, partySummonQuestion } = await import(
	"../../src/engine/foundation/ui/party-summon.ts"
);

const SELF_ID = 11;
const LEADER_ID = 22;

const u16 = n => [ n & 255, n >>> 8 & 255 ];
const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ ...u16( Buffer.byteLength( s ) ), ...Buffer.from( s ) ];
const frame = payload => ({ opcode: 0xb4c6, payload: Uint8Array.from( payload ) });

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
leading

The local player leading a two-member party.
================
*/
function leading() {
	return {
		...emptySocial( "Me" ),
		self: SELF_ID,
		leader: SELF_ID,
		members: [ member( SELF_ID, "Me" ), member( LEADER_ID, "Other" ) ]
	};
}

/*
================
led

The local player as the second member of another leader's party.
================
*/
function led() {
	return {
		...emptySocial( "Me" ),
		self: SELF_ID,
		leader: LEADER_ID,
		members: [ member( SELF_ID, "Me" ), member( LEADER_ID, "Other" ) ]
	};
}

test("a roster answer publishes its header and native-shaped rows", () => {
	const after = socialPacket(
		leading(),
		frame( [
			1,
			...u16( 8 ),
			1,
			...u32( 600 ),
			2,
			...u32( LEADER_ID ),
			2,
			0,
			...u16( 7 ),
			...u32( 33 ),
			3,
			1,
			...u16( 0 )
		] )
	);
	assert.deepEqual( after?.summon, {
		allowed: 8,
		summoned: 1,
		limitSeconds: 600,
		rows: [
			{ gid: LEADER_ID, eligibility: 2, entered: false, remaining: 7 },
			{ gid: 33, eligibility: 3, entered: true, remaining: 0 }
		]
	} );
});

test("a refused answer names its router code, and _5/_9 codes stay silent", () => {
	const after = socialPacket( leading(), frame( [ 0, ...u16( 0x1c25 ) ] ) );
	assert.deepEqual( after?.summonNotice, { code: 0x1c25 } );
	assert.equal( partySummonRefusalKey( 0x1c25 ), "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_2" );
	assert.equal( partySummonRefusalKey( 0x1c37 ), "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_1" );
	assert.equal( partySummonRefusalKey( 0x1c3a ), "UIIT_STT_PARTY_SUMMON_WAIT_ERR" );
	// The busy code routes nowhere a summon line exists.
	assert.equal( partySummonRefusalKey( 0x1c0b ), null );
	assert.equal( partySummonRefusalKey( 0 ), null );
});

test("a leader-side notice carries its UIIT key verbatim", () => {
	const after = socialPacket( leading(), frame( [ 3, ...str( "UIIT_STT_PARTY_SUMMON_BUTTON" ) ] ) );
	assert.deepEqual( after?.summonNotice, { key: "UIIT_STT_PARTY_SUMMON_BUTTON" } );
});

test("a prompt names its leader and the answer closes over it", () => {
	const after = socialPacket( led(), frame( [ 2, ...u32( LEADER_ID ), ...str( "Other" ) ] ) );
	assert.deepEqual( after?.summonPrompt, { gid: LEADER_ID, name: "Other" } );
	assert.deepEqual( partySummonQuestion( "Other" ), {
		key: "UIIT_STT_PARTY_SUMMON_CONFIRM_BOX",
		arg: "Other"
	} );
	const sent = socialRequest( after, { kind: "party-summon-answer", accept: true } );
	assert.equal( sent.opcode, 0x34c6 );
	assert.deepEqual( [ ...sent.payload ], [ 2, 1 ] );
	const declined = socialRequest( after, { kind: "party-summon-answer", accept: false } );
	assert.deepEqual( [ ...declined.payload ], [ 2, 0 ] );
	const closed = withoutSummonPrompt( after );
	assert.equal( "summonPrompt" in closed, false );
});

test("the leader's commands encode the staged bytes; non-leaders cannot send them", () => {
	const roster = socialRequest( leading(), { kind: "party-summon-roster" } );
	assert.equal( roster.opcode, 0x34c6 );
	assert.deepEqual( [ ...roster.payload ], [ 0, 0 ] );
	const summon = socialRequest( leading(), { kind: "party-summon" } );
	assert.deepEqual( [ ...summon.payload ], [ 1, 0 ] );
	assert.throws( () => socialRequest( led(), { kind: "party-summon" } ), /party leader/ );
	assert.throws( () => socialRequest( leading(), { kind: "party-summon-answer", accept: true } ), /no longer/ );
});

test("a corrupt summon frame is refused without publishing", () => {
	// Eligibility byte outside the native 2/3.
	assert.throws( () =>
		socialPacket(
			leading(),
			frame( [
				1,
				...u16( 8 ),
				1,
				...u32( 0 ),
				1,
				...u32( LEADER_ID ),
				9,
				0,
				...u16( 7 )
			] )
		), /eligibility/ );
	// Truncated roster row.
	assert.throws( () =>
		socialPacket(
			leading(),
			frame( [
				1,
				...u16( 8 ),
				1,
				...u32( 0 ),
				1,
				...u32( LEADER_ID )
			] )
		), /Truncated/ );
	// Unknown kind.
	assert.throws( () => socialPacket( leading(), frame( [ 7 ] ) ), /party summon answer/ );
});

test("the window layout frames its rows and buttons", () => {
	const layout = partySummonWindowLayout( 1024, 768, 3, null );
	assert.equal( layout.rows.length, 3 );
	assert.equal( layout.headers.length, 4 );
	// Centred, and the buttons sit inside the frame.
	assert.equal( layout.frame[0], Math.round( (1024 - layout.frame[2]) / 2 ) );
	assert.ok( layout.summon[1] + layout.summon[3] <= layout.frame[1] + layout.frame[3] );
	assert.ok( layout.close[0] + layout.close[2] <= layout.frame[0] + layout.frame[2] );
});
