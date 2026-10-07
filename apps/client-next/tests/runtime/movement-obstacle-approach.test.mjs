/*
===========================================================================

movement-obstacle-approach.test.mjs - keep a blocked click's reachable walk

The requested endpoint may be blocked. Prediction and the server receipt
must agree on walking to the first contact and remaining there.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);

test("a blocked destination walks to contact before and after its accepted receipt", () => {
	const frames = [], movement = createMovement( frame => frames.push( frame ) );
	const navigation = product(), blocked = Buffer.alloc( 9216 );
	navigation.objects = [];
	for ( let z = 4; z <= 7; z++ ) {
		for ( let x = 4; x <= 7; x++ ) blocked[z * 96 + x] = 1;
	}
	navigation.navmesh.regions[0].blockedTiles = blocked.toString( "base64" );
	const from = { regionId: 257, x: 30, y: 0, z: 110, angle: 0 };
	const requested = { ...from, x: 110 }, contact = { ...from, x: 79.99 };
	movement.seed( from );
	movement.navigation( 257, navigation );
	movement.request( requested, 0 );
	assert.equal( new DataView( frames[0].payload.buffer ).getInt16( 8, true ), requested.x );
	movement.step( 200 );
	assert.equal( movement.state().pose?.x, 40 );
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: true,
			serverTimeMs: 100,
			world: {
				spawn: contact,
				moveSegment: { from, startedAtMs: 100, arrivesAtMs: 1099.8 }
			}
		} ) ),
		200,
		7
	);
	movement.step( 400 );
	assert.equal( movement.state().pose?.x, 50 );
	movement.step( 2000 );
	const arrived = movement.state().pose;
	assert.ok( arrived );
	assert.ok( Math.abs( arrived.x - contact.x ) < 1 / 8192 );
	assert.equal( movement.state().pendingMoves, 0 );
	// A second click into the same obstacle cannot pass through the wall.
	movement.request( requested, 2000 );
	movement.step( 4000 );
	const repeated = movement.state().pose;
	assert.ok( repeated );
	assert.ok( Math.abs( repeated.x - contact.x ) < 1 / 8192 );
	movement.clear();
});

test("a ground receipt retains accepted authority instead of sampling the unvalidated goal", () => {
	const movement = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	const from = { regionId: 257, x: 30, y: 0, z: 110, angle: 0 };
	const goal = { ...from, x: 110 }, accepted = { ...from, x: 35 };
	movement.seed( from );
	movement.navigation( 257, navigation );
	movement.request( goal, 0 );
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: true,
			serverTimeMs: 1000,
			world: {
				spawn: goal,
				ground: { pose: accepted, at: 1000, speed: 50 },
				moveSegment: { from, startedAtMs: 0, arrivesAtMs: 1600 }
			}
		} ) ),
		1000,
		7
	);
	assert.equal( defined( movement.state().authoritativePose ).x, accepted.x );
	assert.equal( movement.state().pendingMoves, 0 );
	movement.clear();
});
