/*
===========================================================================

academy-travel.test.mjs - tests for core.ts and gameplay.ts bootstrap

A 0x3369 world transfer or a resumed transport keeps the academy roster:
the entry snapshot carries only the academyMember flag and the server
seeds the roster once (its OnWorldBound), so it resends nothing on a
continued entry. The member updates that follow a teleport must find
the roster ("Unknown academy member status", #2876). A new character,
or a cleared session, starts with no roster.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );

const SELF_ID = 7;
// The incident's member (0x3AC5 05 cc890100 02 55, 2026-11-03).
const MEMBER_ID = 0x189cc;

const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ s.length, 0, ...Buffer.from( s ) ];
const frame = ( opcode, p ) => ({ opcode, payload: Uint8Array.from( p ) });

/*
================
entry

One enter-world bootstrap for an academy member.
================
*/
function entry( name ) {
	return {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x694f, x: 10, y: 20, z: 30, angle: 0 } },
		character: { name, skills: [], quickSlots: [] },
		academyMember: true
	};
}

/*
================
memberRow

One 0x3AC5 camp-record member row (82AA10).
================
*/
function memberRow( id, name, kind, level ) {
	return [
		...u32( 0 ),
		...u32( id ),
		...u32( 1907 ),
		...str( name ),
		kind,
		0,
		...Array( 16 ).fill( 0 ),
		level,
		level,
		...u32( 0 ),
		0,
		...u32( 0 ),
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		...str( "" )
	];
}

/*
================
seed

The status-10 roster seed the server sends once at world entry.
================
*/
function seed() {
	return frame( 0x3ac5, [
		10,
		1,
		...u32( SELF_ID ),
		...Array( 16 ).fill( 0 ),
		0,
		...str( "" ),
		...str( "" ),
		2,
		...memberRow( SELF_ID, "Me", 0, 60 ),
		...memberRow( MEMBER_ID, "Student", 2, 84 )
	] );
}

/*
================
levelUpdate

The incident frame: a status-5 level update for one camp member.
================
*/
function levelUpdate( level ) {
	return frame( 0x3ac5, [ 5, ...u32( MEMBER_ID ), 2, level ] );
}

/*
================
camped

A world core whose character "Me" leads a seeded two-member camp.
================
*/
function camped() {
	const core = createWorldCore( () => {} );
	core.bootstrap( entry( "Me" ) );
	core.receive( seed(), 0 );
	return core;
}

/*
================
academy

The academy state the presentation holds after everything queued so far.
================
*/
function academy( core, presentation ) {
	core.step( 0, false );
	for ( let batch = core.take(); batch; batch = core.take() ) {
		core.ack( batch.sequence );
		presentation.apply( batch );
	}
	return defined( defined( presentation.gameplay(), "gameplay" ).academy, "academy" );
}

test("a teleport keeps the academy roster, so the next member update applies (#2876)", () => {
	for ( const reset of [ 0x3369, 0x366a ] ) {
		const core = camped(), presentation = createPresentation();
		assert.equal( academy( core, presentation ).members?.length, 2 );
		core.receive( frame( reset, [ 0x4f, 0x69 ] ), 1 );
		core.bootstrap( entry( "Me" ) );
		// The exact incident payload: member 0x189CC reaches level 85.
		core.receive( levelUpdate( 85 ), 2 );
		const after = academy( core, presentation );
		assert.equal( after.member, true );
		assert.deepEqual( after.members?.map( m => m.id ), [ SELF_ID, MEMBER_ID ] );
		assert.equal( after.members?.[1]?.level, 85 );
		core.dispose();
		presentation.dispose();
	}
});

test("a resumed transport keeps the roster the server kept; a plain entry starts over", () => {
	for ( const resumed of [ true, false ] ) {
		const core = camped(), presentation = createPresentation();
		core.bootstrap( entry( "Me" ), resumed );
		if ( resumed ) {
			core.receive( levelUpdate( 85 ), 1 );
			assert.equal( academy( core, presentation ).members?.[1]?.level, 85 );
		} else {
			assert.equal( academy( core, presentation ).members, undefined, "a non-resumed entry starts over" );
			assert.throws( () => core.receive( levelUpdate( 85 ), 1 ), /Unknown academy member status/ );
		}
		core.dispose();
		presentation.dispose();
	}
});

test("a continued entry whose snapshot ends the membership drops the roster", () => {
	const core = camped(), presentation = createPresentation();
	core.receive( frame( 0x3369, [ 0x4f, 0x69 ] ), 1 );
	core.bootstrap( { ...entry( "Me" ), academyMember: false } );
	const after = academy( core, presentation );
	assert.equal( after.member, false );
	assert.equal( after.members, undefined );
	core.dispose();
	presentation.dispose();
});
