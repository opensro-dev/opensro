/*
===========================================================================

speed-journal.test.mjs - the bug report's record of speed frames, effects,
mount and buffs (#3508)

Port-only diagnostics (foundation/gameplay/speed-journal.ts): every 0x376F
about the local player or its mount keeps where it was routed, the local
player's effects keep their add and teardown, the mount keeps each change,
and the buff board is projected at capture, numbers only.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const {
	createSpeedJournal,
	buffJournal,
	speedFrameRoute,
	SPEED_JOURNAL_ROWS,
	SPEED_JOURNAL_FIELDS,
	MOUNT_JOURNAL_FIELDS,
	BUFF_JOURNAL_NO_TIMER
} = await import( "../../src/engine/foundation/gameplay/speed-journal.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ];

/*
================
rows

A flattened journal as rows of fields numbers.
================
*/
function rows( journal, fields ) {
	const out = [];
	for ( let i = 0; i < journal.length; i += fields ) out.push( journal.slice( i, i + fields ) );
	return out;
}

test("speed rows keep the newest frames and their routing", () => {
	const journal = createSpeedJournal();
	for ( let i = 0; i < SPEED_JOURNAL_ROWS + 3; i++ ) {
		journal.speed( { atMs: i, gid: 7, appliedGid: 9, moverGid: 9, walkSpeed: 40, runSpeed: 100 + i } );
	}
	const kept = rows( journal.state( [] ).speedJournal, SPEED_JOURNAL_FIELDS );
	assert.equal( kept.length, SPEED_JOURNAL_ROWS );
	assert.deepEqual( kept[0], [ 3, 7, 9, 9, 40, 103 ] );
	assert.deepEqual( kept.at( -1 ), [ SPEED_JOURNAL_ROWS + 2, 7, 9, 9, 40, SPEED_JOURNAL_ROWS + 102 ] );
});

test("the mount journal records the first state and each change only", () => {
	const journal = createSpeedJournal();
	assert.equal( journal.mount( 1, 0 ), true );
	assert.equal( journal.mount( 2, 0 ), false );
	assert.equal( journal.mount( 3, 55 ), true );
	assert.equal( journal.mount( 4, 55 ), false );
	assert.equal( journal.mount( 5, 0 ), true );
	assert.deepEqual( rows( journal.state( [] ).mountJournal, MOUNT_JOURNAL_FIELDS ), [ [ 1, 0 ], [ 3, 55 ], [
		5,
		0
	] ] );
});

test("the buff projection carries numbers only, never a subject's name", () => {
	/** @type {import("../../src/engine/foundation/gameplay/buff-slots.ts").BuffSlot[]} */
	const slots = [
		{
			state: "active",
			serial: 1,
			secondary: false,
			effect: { gid: 7, skill: 3, token: 100, phase: 2, remainingMs: 900, receivedAtMs: 50 }
		},
		{
			state: "departing",
			serial: 2,
			endedAtMs: 60,
			effect: { gid: 7, skill: 4, token: 101, phase: 2, subject: { gid: 8, name: "Somebody" } }
		}
	];
	const projected = buffJournal( slots );
	assert.deepEqual( projected, [
		7,
		3,
		100,
		1,
		900,
		50,
		7,
		4,
		101,
		0,
		BUFF_JOURNAL_NO_TIMER,
		BUFF_JOURNAL_NO_TIMER
	] );
	assert.ok( projected.every( value => typeof value === "number" ) );
});

test("a speed frame is the local player's when it is about it, its mount, or lands on its mount", () => {
	const mounts = new Map( [ [ 7, 55 ], [ 8, 66 ] ] ), mountOf = gid => mounts.get( gid );
	assert.deepEqual( speedFrameRoute( 7, 7, mountOf ), { appliedGid: 55, local: true } );
	assert.deepEqual( speedFrameRoute( 55, 7, mountOf ), { appliedGid: 55, local: true } );
	assert.deepEqual( speedFrameRoute( 8, 7, mountOf ), { appliedGid: 66, local: false } );
	assert.deepEqual( speedFrameRoute( 66, 7, mountOf ), { appliedGid: 66, local: false } );
	assert.deepEqual( speedFrameRoute( 7, 7, () => undefined ), { appliedGid: 7, local: true } );
	assert.equal( speedFrameRoute( 7, 0, mountOf ).local, false );
});

test("combat journals the local player's effect adds and teardowns, not a peer's", () => {
	const noted = [], c = createCombat( () => undefined, () => {}, () => 0, row => noted.push( row ) );
	c.references( [ 1, 2 ].map( id => ({
		id,
		status: true,
		effectRider: false,
		effectDurationMs: 1000,
		zeroEffectDuration: false
	}) ) );
	// Only the fields combat reads; the rest of SkillMetadata is the catalog's.
	const catalog = /** @type {import("../../src/engine/foundation/gameplay/skill-catalog.ts").SkillMetadata[]} */ (
		/** @type {unknown} */ ([ 1, 2 ].map( id => ({ id, icon: "skill/a.ddj", name: "Buff" }) ))
	);
	c.cooldownReferences( 10, catalog );
	const apply = ( gid, skill, token, at ) =>
		c.receive( 0xb419, Uint8Array.from( [ ...u32( gid ), ...u32( skill ), ...u32( token ), 2 ] ), at );
	apply( 10, 1, 100, 5 );
	apply( 11, 2, 101, 6 );
	c.receive( 0xb6a0, Uint8Array.from( [ 2, ...u32( 100 ), ...u32( 101 ) ] ), 9 );
	assert.deepEqual( noted, [
		{ atMs: 5, opcode: 0xb419, gid: 10, skill: 1, token: 100 },
		{ atMs: 9, opcode: 0xb6a0, gid: 10, skill: 1, token: 100 }
	] );
});
