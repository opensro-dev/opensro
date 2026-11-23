/*
===========================================================================

ghost-walk-endpoint.test.mjs - the combo behind the #4154 player report

Ghost Walk then Ghost Spear sometimes faced the wrong way, attacked from
too far from the monster and jumped on the next ground click. The server
publishes the dash destination and seats the actor there at once; the
worker core used to re-clip that destination on the client navigation, so
where the two navigations disagreed the presented dash stopped or slid at
an edge while the server (and every range check) used the published
point. This test drives the exact core chain across the fixture cell
boundary: the presented dash must end at the published point, facing the
travel direction, and the next ground move must start there.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const START = { regionId: 257, x: 50, y: 10, z: 50, angle: 0 };
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const LOCAL = { ...START, gid: 1, refObjId: 1907, kind: "local-player", name: "dash", heading: 0 };
const REFERENCE = {
	id: 114,
	group: 261,
	level: 1,
	status: false,
	effectRider: false,
	ui: {
		name: "Ghost Walk",
		trainable: true,
		spCost: 1,
		targetRequired: true,
		groundTarget: true,
		haltsWalk: true,
		cooldownMs: 5000,
		cooldownGroup: 59,
		masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
		prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
	}
};

/*
================
dashPacket

A bit-8 (guided) cast success ending at ( 330, 50 ) for the given gid.
================
*/
function dashPacket( gid ) {
	const packet = Buffer.alloc( 27 );
	packet[0] = 1;
	packet.writeUInt32LE( 114, 2 );
	packet.writeUInt32LE( gid, 6 );
	packet.writeUInt32LE( 9, 10 );
	packet[18] = 8;
	packet.writeUInt16LE( 257, 19 );
	packet.writeInt16LE( 330, 21 );
	packet.writeInt16LE( 50, 25 );
	return packet;
}

test("the presented Ghost Walk ends where the server put the character, and the next move starts there", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1, character: { skills: [ 114 ] }, refSkillSnapshot: [ REFERENCE ] } );
	game.seed( LOCAL );
	// The fixture keeps its one 0..100 cell: the dash line leaves the cell,
	// which is where the old resolver substituted its own endpoint.
	game.command( { kind: "navigation", regionId: 257, bundle: product() }, 0, undefined );
	game.receive( { opcode: 0xb245, payload: dashPacket( 1 ) }, 400 );
	const travel = game.takeDisplacements()[0];
	assert.ok( travel, "the cast publishes a displacement" );
	assert.equal( travel.destination.x, 330, "the wire destination is the published point" );
	// The worker core displaces with the displacement as published (core.ts).
	const arrival = game.displace( travel, 400 );
	assert.ok( arrival !== undefined && arrival > 400 );
	game.step( 5000 );
	const end = game.take();
	assert.equal( end?.pose?.x, 330, "the presented dash ends at the published point, not the nav edge" );
	assert.equal( end?.pose?.z, 50, "no sideways slide at the edge" );
	assert.equal( end?.pose?.angle, 0, "at rest the character faces the travel direction" );
	// The follow-up the report is about: a ground move issued now must walk
	// from the published point, never snap back to the substituted one.
	game.command( { kind: "move", destination: { ...START, x: 430, z: 50 } }, 5000, undefined );
	game.step( 5100 );
	const walking = game.take();
	assert.ok(
		walking?.pose && walking.pose.x >= 330 && walking.pose.x < 340,
		`the ground move starts at the published point (pose ${walking?.pose?.x})`
	);
	game.dispose();
});

test("a peer's dash also lands on the published point", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	const peer = { ...START, gid: 2, kind: "player", heading: 0 };
	motion.displace( peer, { gid: 2, token: 9, kind: 8, destination: { ...START, x: 330 } }, 400 );
	let pose;
	for ( const now of [ 1000, 2000 ] ) pose = motion.step( now ).find( step => step.gid === 2 ) ?? pose;
	assert.equal( pose?.x, 330 );
	assert.equal( pose?.moving, false );
});
