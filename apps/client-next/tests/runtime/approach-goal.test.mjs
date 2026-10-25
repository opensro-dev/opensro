/*
===========================================================================

approach-goal.test.mjs - the destination an entity click walks to first

#1567: the main-thread mover predicts an entity-approach walk from the
click frame, so the destination it walks must be the worker's own: the
interaction approach point for a select (698740), the item itself for a
far pickup (the run-up predictPickupRunUp starts). One pure owner names
it for both sides. The worker half pins that gameplay's select and
pickup arms walk exactly where approachGoal says.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { product } from "../helpers/navigation-fixture.mjs";
const { approachGoal, PICKUP_EXECUTE_RANGE } = await import(
	"../../src/engine/foundation/gameplay/approach-goal.ts"
);
const { createGameplay } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ) ).href
);

const REGION = 257;
const POSE = { regionId: REGION, x: 60, y: 10, z: 100, angle: 0 };
const STANDING = { ...POSE, gid: 7, heading: 0, appearanceState: [ 1, 0, 0 ] };
// movement.ts OP_PREDICTED_MOVE: the internal frame a walk request sends.
const PREDICTED_MOVE = 9;

/*
================
standing

An entity row of kind standing at (x, y, z) in REGION.
================
*/
function standing( kind, x, z, extra = {} ) {
	return {
		gid: 100 + x,
		refObjId: 2000,
		kind,
		name: "n",
		regionId: REGION,
		x,
		y: 10,
		z,
		heading: 0,
		...extra
	};
}

/*
================
goal4

A published movement goal's place, without its heading.
================
*/
function goal4( goal ) {
	return goal && { regionId: goal.regionId, x: goal.x, y: goal.y, z: goal.z };
}

/*
================
started

A fresh gameplay owner standing at POSE, initial publication drained.
================
*/
function started() {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( STANDING );
	game.step( 50, STANDING );
	game.take();
	return { game, sent };
}

test("a far NPC, gate or stall is approached to its native reach point", () => {
	// NPC: selected inside 240, approached to 240 (698740).
	assert.deepEqual( approachGoal( POSE, standing( "npc", 360, 100 ) ), {
		regionId: REGION,
		x: 120,
		y: 10,
		z: 100,
		angle: 0
	} );
	assert.equal( approachGoal( POSE, standing( "npc", 300, 100 ) ), null, "exactly 240 is already in reach" );
	assert.equal( approachGoal( POSE, standing( "npc", 299, 100 ) ), null );
	// The reach is 3D: 144 across and 192 up is exactly 240.
	assert.equal( approachGoal( POSE, { ...standing( "npc", 204, 100 ), y: 202 } ), null );
	// Gate (teleport): selected inside 800, approached to 640.
	assert.deepEqual( approachGoal( POSE, standing( "teleport", 960, 100 ) ), {
		regionId: REGION,
		x: 320,
		y: 10,
		z: 100,
		angle: 0
	} );
	assert.equal( approachGoal( POSE, standing( "teleport", 860, 100 ) ), null, "exactly 800 selects at once" );
	// A stall keeper (title mode 4) is visited inside 100, approached to 80.
	const stall = standing( "player", 210, 100, { appearanceState: [ 0, 0, 0, 0, 0, 0, 4 ] } );
	assert.deepEqual( approachGoal( POSE, stall ), { regionId: REGION, x: 130, y: 10, z: 100, angle: 0 } );
	assert.equal( approachGoal( POSE, { ...stall, x: 160 } ), null, "exactly 100 visits at once" );
});

test("a click that starts no walk has no approach goal", () => {
	assert.equal( approachGoal( POSE, standing( "monster", 360, 100 ) ), null );
	assert.equal( approachGoal( POSE, standing( "player", 360, 100 ) ), null, "no stall, no approach" );
	assert.equal( approachGoal( POSE, standing( "cos", 360, 100 ) ), null );
	assert.equal( approachGoal( POSE, standing( "structure", 360, 100 ) ), null );
	assert.equal( approachGoal( POSE, standing( "local-player", 360, 100 ) ), null );
	// Another dungeon's entity is never walked to from here.
	assert.equal( approachGoal( POSE, { ...standing( "npc", 360, 100 ), regionId: REGION | 0x8000 } ), null );
	assert.equal( approachGoal( POSE, { ...standing( "ground-item", 360, 100 ), regionId: REGION | 0x8000 } ), null );
});

test("a far ground item is walked onto; one in execute range is not", () => {
	assert.equal( PICKUP_EXECUTE_RANGE, 10 );
	assert.deepEqual( approachGoal( POSE, { ...standing( "ground-item", 110, 100 ), y: 12 } ), {
		regionId: REGION,
		x: 110,
		y: 12,
		z: 100,
		angle: 0
	} );
	assert.equal( approachGoal( POSE, standing( "ground-item", 70, 100 ) ), null, "exactly 10 picks up in place" );
	assert.equal( approachGoal( POSE, standing( "ground-item", 69, 100 ) ), null );
});

test("the worker's select walks where approachGoal says, and only then", () => {
	const far = started();
	far.game.command( { kind: "navigation", regionId: REGION, bundle: product( REGION ) }, 0, undefined, STANDING );
	far.game.command( { kind: "select", gid: 460 }, 100, standing( "npc", 360, 100 ), STANDING );
	far.game.step( 116, STANDING );
	assert.equal( far.sent.some( f => f.opcode === PREDICTED_MOVE ), true, "the select started a walk" );
	assert.deepEqual(
		goal4( far.game.take()?.movementGoal ),
		goal4( approachGoal( POSE, standing( "npc", 360, 100 ) ) )
	);
	far.game.dispose();

	const near = started();
	near.game.command( { kind: "navigation", regionId: REGION, bundle: product( REGION ) }, 0, undefined, STANDING );
	near.game.command( { kind: "select", gid: 399 }, 100, standing( "npc", 299, 100 ), STANDING );
	near.game.step( 116, STANDING );
	assert.equal( near.sent.some( f => f.opcode === PREDICTED_MOVE ), false, "an in-reach select walks nowhere" );
	assert.equal( near.game.take()?.movementGoal, undefined );
	near.game.dispose();
});

test("the worker's pickup run-up walks where approachGoal says, and only then", () => {
	const far = started();
	far.game.command( { kind: "navigation", regionId: REGION, bundle: product( REGION ) }, 0, undefined, STANDING );
	far.game.command( { kind: "pickup", gid: 190 }, 100, { ...standing( "ground-item", 90, 90 ), y: 12 }, STANDING );
	far.game.step( 116, STANDING );
	assert.equal( far.sent.some( f => f.opcode === 0x72cd ), true, "the pickup went on the wire" );
	assert.deepEqual(
		goal4( far.game.take()?.movementGoal ),
		goal4( approachGoal( POSE, { ...standing( "ground-item", 90, 90 ), y: 12 } ) )
	);
	far.game.dispose();

	const near = started();
	near.game.command( { kind: "navigation", regionId: REGION, bundle: product( REGION ) }, 0, undefined, STANDING );
	near.game.command( { kind: "pickup", gid: 168 }, 100, standing( "ground-item", 68, 100 ), STANDING );
	near.game.step( 116, STANDING );
	assert.equal( near.sent.some( f => f.opcode === 0x72cd ), true );
	assert.equal( near.game.take()?.movementGoal, undefined, "an in-range pickup walks nowhere" );
	near.game.dispose();
});
