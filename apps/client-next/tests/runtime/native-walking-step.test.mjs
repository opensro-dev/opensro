/*
===========================================================================

native-walking-step.test.mjs - native elapsed movement arithmetic and stops

Runtime scenarios verify that elapsed client steps start at the accepted
position, keep their stored heading, and never replay consumed time.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { clientWalkingStep, clientWalkingDirection } = await import(
	"../../src/engine/foundation/gameplay/native-movement.ts"
);
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
/*
================
walker
================
*/
function walker( blocked = false ) {
	const movement = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	if ( blocked ) {
		const tiles = Buffer.alloc( 9216 );
		for ( let z = 0; z < 96; z++ ) tiles[z * 96 + 4] = 1;
		navigation.navmesh.regions[0].blockedTiles = tiles.toString( "base64" );
	}
	const from = { regionId: 257, x: 30, y: 0, z: 110, angle: 0 };
	movement.seed( from );
	movement.navigation( 257, navigation );
	movement.request( { ...from, x: 1500 }, 0 );
	return movement;
}

test("a client stall consumes its uncapped native distance exactly once", () => {
	const movement = walker();
	movement.step( 4000 );
	assert.equal( defined( movement.state().pose ).x, 230 );
	movement.step( 4016 );
	assert.equal( defined( movement.state().pose ).x, Math.fround( 230 + Math.fround( 50 * Math.fround( .016 ) ) ) );
	movement.clear();
});

test("an obstacle stops an actual elapsed step, while the published corridor never crosses it", () => {
	const movement = walker( true );
	for ( let now = 16; now <= 1500; now += 16 ) {
		movement.step( now );
		const state = movement.state();
		assert.ok( defined( state.pose ).x < 80 );
		if ( state.movementPath ) assert.ok( state.movementPath.to.x < 80 );
	}
	assert.ok( defined( movement.state().pose ).x > 79 );
	assert.equal( movement.state().moving, false );
	movement.clear();
});

test("rounding accepted positions does not re-aim the stored walking direction", () => {
	const movement = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	const from = { regionId: 257, x: 30, y: 0, z: 110, angle: 0 };
	movement.seed( from );
	movement.navigation( 257, navigation );
	movement.request( { ...from, x: 130, z: 143 }, 0 );
	const { step } = clientWalkingStep( 50, .016, clientWalkingDirection( [ 100, 33 ] ) );
	let x = from.x, z = from.z;
	for ( let now = 16; now <= 1600; now += 16 ) {
		x = Math.fround( x + step[0] );
		z = Math.fround( z + step[1] );
		movement.step( now );
		assert.equal( defined( movement.state().pose ).x, x );
		assert.equal( defined( movement.state().pose ).z, z );
	}
	movement.clear();
});

test("an elevated destination clamps the planar step and acquires terrain height", () => {
	const movement = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	const from = { regionId: 257, x: 30, y: 0, z: 110, angle: 0 };
	movement.seed( from );
	movement.navigation( 257, navigation );
	movement.request( { ...from, x: 31, y: 200 }, 0 );
	movement.step( 100 );
	assert.equal( defined( movement.state().pose ).x, 31 );
	assert.equal( defined( movement.state().pose ).y, 0 );
	assert.equal( movement.state().moving, false );
	movement.clear();
});
