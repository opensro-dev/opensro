/*
===========================================================================

cast-halts-walk.test.mjs - tests for gameplay.ts and skill-catalog.ts:
an ordinary cast stops the walk at the press

A self or ground cast of activity 2 (haltsWalk) stops the caster where it
stands: InitiateSkillCast (59B5F6) on the server, CICharactor_Action_CastSkill
(8E67E0) in the original client. The local walk ends at the press, as a
targeted command's does; an instant row (an imbue) keeps walking.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { skillCatalog } = await import( "../../src/engine/foundation/gameplay/skill-catalog.ts" );

const LOCAL_GID = 7;
const GUARD_SKILL = 1001;
const IMBUE_SKILL = 1002;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 1907,
	kind: "local-player",
	name: "caster",
	regionId: 0x61a8,
	x: 100,
	y: 10,
	z: 100,
	heading: 0
};

/*
================
skillRef

An untargeted skill reference row; halts marks an ordinary cast.
================
*/
function skillRef( id, halts ) {
	return {
		id,
		group: id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: false,
			...(halts ? { haltsWalk: true } : {}),
			cooldownMs: 0,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

/*
================
pressWhileRunning

Runs the local player toward x = 260 on the server's walk, presses skill at
1000 ms, and returns the drawn x at the press and 1000 ms later.
================
*/
function pressWhileRunning( skill ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ GUARD_SKILL, IMBUE_SKILL ] },
		refSkillSnapshot: [ skillRef( GUARD_SKILL, true ), skillRef( IMBUE_SKILL, false ) ]
	} );
	game.seed( local );
	const walk = Buffer.alloc( 14 );
	walk.writeUInt32LE( LOCAL_GID );
	walk[4] = 1;
	walk.writeUInt16LE( local.regionId, 5 );
	walk.writeInt16LE( 260, 7 );
	walk.writeInt16LE( 10, 9 );
	walk.writeInt16LE( 100, 11 );
	assert.equal( game.receive( { opcode: 0xb738, payload: walk }, 0 ), true );
	for ( let now = 16; now <= 1000; now += 16 ) game.step( now, local );
	game.step( 1000, local );
	const pressed = game.take()?.pose?.x;
	game.command( { kind: "skill", skillId: skill }, 1000, undefined, local );
	assert.equal( sent.at( -1 )?.opcode, 0x72cd );
	let later = pressed;
	for ( let now = 1016; now <= 2000; now += 16 ) {
		game.step( now, local );
		later = game.take()?.pose?.x ?? later;
	}
	game.dispose();
	return { pressed, later };
}

test("the catalog reads haltsWalk and defaults it to false", () => {
	const rows = skillCatalog( {
		refSkillSnapshot: [ skillRef( GUARD_SKILL, true ), skillRef( IMBUE_SKILL, false ) ]
	} );
	assert.equal( rows.find( r => r.id === GUARD_SKILL )?.haltsWalk, true );
	assert.equal( rows.find( r => r.id === IMBUE_SKILL )?.haltsWalk, false );
	const bad = skillRef( GUARD_SKILL, true );
	assert.throws( () => skillCatalog( { refSkillSnapshot: [ { ...bad, ui: { ...bad.ui, haltsWalk: 1 } } ] } ) );
});

test("a self buff pressed on the run stops the walk where it was pressed", () => {
	const { pressed, later } = pressWhileRunning( GUARD_SKILL );
	assert.ok( pressed !== undefined && pressed > 100 && pressed < 260, "running at the press: " + pressed );
	assert.equal( later, pressed, "the caster walked on while casting" );
});

test("an instant skill pressed on the run keeps walking", () => {
	const { pressed, later } = pressWhileRunning( IMBUE_SKILL );
	assert.ok( pressed !== undefined && later !== undefined && later > pressed + 10, `walk ${pressed} -> ${later}` );
});
