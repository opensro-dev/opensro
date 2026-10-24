/*
===========================================================================

stall-lanes.test.mjs - every lost-answer lane feeds the stall detector

The world session names a stalled link when a request has waited 7 s with no
frame of any kind arriving (#1418, world.ts stepStall). It asks the gameplay
owner when the oldest request still waiting left (outstandingSince). A
pending target select or release is such a request: its lost answer resumes
the session like a move's (#1472).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const local = {
	gid: 100007,
	refObjId: 1907,
	name: "walker",
	kind: /** @type {const} */ ("player"),
	regionId: 0x62a8,
	x: 50,
	y: 0,
	z: 50,
	heading: 0
};
const monster = { ...local, gid: 9, refObjId: 1950, name: "wolf", kind: /** @type {const} */ ("monster"), x: 60 };

test("a pending target select is the oldest outstanding request until it is answered", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	assert.equal( game.outstandingSince(), undefined );
	game.command( { kind: "select", gid: monster.gid }, 100, /** @type {any} */ (monster), local );
	// The select left at 100 and waits for its grant.
	assert.equal( game.outstandingSince(), 100 );
	// Its timeout clears the selection locally (a select has no lost-answer
	// throw), after which nothing is outstanding.
	game.step( 100 + 10_001, local );
	assert.equal( game.outstandingSince(), undefined );
});
