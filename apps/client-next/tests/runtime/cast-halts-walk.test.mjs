/*
===========================================================================

cast-halts-walk.test.mjs - tests for gameplay.ts and skill-catalog.ts:
an ordinary cast stops the walk

A self or ground cast of activity 2 (haltsWalk) stops the caster where it
stands: InitiateSkillCast (59B5F6) on the server, CICharactor_Action_CastSkill
(8E67E0) in the original client. An instant row (an imbue) keeps walking.

Where the local walk ends depends on who leads it (movement.ts WalkLead). A
click run is one delivery ahead of the server, so it ends at the press. A
walk the server drives is one delivery behind, so it walks on until the
server's stop arrives, which lands under it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { skillCatalog } = await import( "../../src/engine/foundation/gameplay/skill-catalog.ts" );

const LOCAL_GID = 7;
const GUARD_SKILL = 1001;
const IMBUE_SKILL = 1002;
const REGION = 257;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 1907,
	kind: "local-player",
	name: "caster",
	regionId: REGION,
	x: 100,
	y: 0,
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

Runs the local player toward x = 260, presses skill at 1000 ms, and returns
the x at the press and 1000 ms later. A "client" run is the player's own
ground click; a "server" run is a walk the server drives (0xB738).
================
*/
function pressWhileRunning( skill, lead, answer ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ GUARD_SKILL, IMBUE_SKILL ] },
		refSkillSnapshot: [ skillRef( GUARD_SKILL, true ), skillRef( IMBUE_SKILL, false ) ]
	} );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	if ( lead === "client" ) {
		game.command( { kind: "move", destination: { ...local, x: 260, angle: 0 } }, 0, undefined, local );
	} else {
		const walk = Buffer.alloc( 14 );
		walk.writeUInt32LE( LOCAL_GID );
		walk[4] = 1;
		walk.writeUInt16LE( REGION, 5 );
		walk.writeInt16LE( 260, 7 );
		walk.writeInt16LE( 0, 9 );
		walk.writeInt16LE( 100, 11 );
		assert.equal( game.receive( { opcode: 0xb738, payload: walk }, 0 ), true );
	}
	for ( let now = 16; now <= 1000; now += 16 ) game.step( now, local );
	game.step( 1000, local );
	const pressed = game.take()?.pose?.x;
	game.command( { kind: "skill", skillId: skill }, 1000, undefined, local );
	assert.equal( sent.at( -1 )?.opcode, 0x72cd );
	if ( answer ) game.receive( answer, 1010 );
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

test("a self buff pressed on a click run stops the walk where it was pressed", () => {
	const { pressed, later } = pressWhileRunning( GUARD_SKILL, "client" );
	assert.ok( pressed !== undefined && pressed > 100 && pressed < 260, "running at the press: " + pressed );
	assert.equal( later, pressed, "the caster walked on while casting" );
});

test("a self buff pressed during a server walk keeps walking until the server's stop arrives", () => {
	const { pressed, later } = pressWhileRunning( GUARD_SKILL, "server" );
	assert.ok( pressed !== undefined && later !== undefined && later > pressed + 10, `walk ${pressed} -> ${later}` );
});

test("an instant skill pressed on a click run keeps walking", () => {
	const { pressed, later } = pressWhileRunning( IMBUE_SKILL, "client" );
	assert.ok( pressed !== undefined && later !== undefined && later > pressed + 10, `walk ${pressed} -> ${later}` );
});

test("a press the server queues behind its open command never holds the click run", () => {
	// B2CD arm, count 2: the server waits for its open command and walks on.
	const { pressed, later } = pressWhileRunning( GUARD_SKILL, "client", {
		opcode: 0xb2cd,
		payload: Uint8Array.of( 1, 2 )
	} );
	assert.ok( pressed !== undefined && later !== undefined && later > pressed + 40, `walk ${pressed} -> ${later}` );
});
