/*
===========================================================================

remote-ground-step.test.mjs - peers collide on finite elapsed steps

An acknowledged destination is intent, including for remote players. These
tests inject the geometry boundary, not a substitute movement algorithm.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import test from "node:test";
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const START = { regionId: 0x0101, x: 100, y: 0, z: 100, angle: 0 };
const ENTITY = {
	...START,
	gid: 1,
	refObjId: 1,
	kind: "player",
	name: "Peer",
	heading: 0,
	movementMode: 3,
	walkSpeed: 20,
	runSpeed: 50,
	spawnDestination: { ...START, x: 1000 }
};

test("a peer collides from its last accepted pose and cannot extrapolate through a future wall", () => {
	const visits = [];
	const motion = createEntityMotion( pose => pose, ( from, to, query ) => {
		query.status = to.x > 115 ? 1 : 0;
		visits.push( [ from.x, to.x ] );
		return to.x > 115 ? { ...from, x: 114.99 } : to;
	} );
	motion.spawn( ENTITY, 0 );
	const first = motion.step( 200 )[0];
	assert.equal( first.x, 110 );
	assert.equal( first.moving, true );
	const stopped = motion.step( 400 )[0];
	assert.equal( stopped.x, 114.99 );
	assert.equal( stopped.moving, false );
	assert.equal( defined( stopped.movementPath ).from.x, defined( stopped.movementPath ).to.x );
	assert.ok( visits.some( ( [from, to] ) => from === 110 && to === 120 ) );
	assert.deepEqual( motion.step( 1000 ), [] );
});

test("a stalled peer preserves native client elapsed stepping without the server-only cap", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	motion.spawn( { ...ENTITY, spawnDestination: { ...START, x: 1700 } }, 0 );
	assert.equal( motion.step( 20000 )[0].x, 1100 );
	assert.equal( motion.step( 20020 )[0].x, 1101 );
	assert.equal( motion.step( 20040 )[0].x, 1102 );
});

test("missing peer navigation holds position and resumes without accumulating a catch-up burst", () => {
	let ready = false;
	const motion = createEntityMotion( pose => pose, ( _from, to ) => ready ? to : null );
	motion.spawn( ENTITY, 0 );
	const held = motion.step( 1000 )[0];
	assert.equal( held.x, 100 );
	assert.equal( defined( held.movementPath ).from.x, defined( held.movementPath ).to.x );
	ready = true;
	assert.equal( motion.step( 1020 )[0].x, 101 );
});

test("a peer death settles at the collision result rather than sampling the intended destination", () => {
	const motion = createEntityMotion( pose => pose, ( from, to, query ) => {
		query.status = to.x > 115 ? 1 : 0;
		return to.x > 115 ? { ...from, x: 114.99 } : to;
	} );
	motion.spawn( ENTITY, 0 );
	const death = motion.stopForDeath( ENTITY, 500 );
	assert.equal( death.x, 114.99 );
	assert.equal( death.moving, false );
	assert.deepEqual( motion.step( 600 ), [] );
});

test("authored peer displacement keeps its separate timing and is not capped as ground walking", () => {
	let clips = 0;
	const motion = createEntityMotion( pose => pose, () => {
		clips++;
		return null;
	} );
	const accepted = motion.displace( ENTITY, { kind: 8, gid: 1, token: 9, destination: { ...START, x: 500 } }, 0 );
	assert.ok( accepted.movementPath.displacement );
	const arrived = motion.step( 100000 )[0];
	assert.equal( arrived.x, 500 );
	assert.equal( clips, 0 );
});

test("remote ground arrival ignores authored goal height before geometry settles the surface", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => ({ ...to, y: 3 }) );
	motion.spawn( { ...ENTITY, spawnDestination: { ...START, x: 112, y: 99 } }, 0 );
	assert.equal( motion.step( 100 )[0].x, 105 );
	const arrived = motion.step( 300 )[0];
	assert.equal( arrived.x, 112 );
	assert.equal( arrived.y, 3 );
	assert.equal( arrived.moving, false );
	assert.deepEqual( motion.step( 400 ), [] );
});

test("remote ground motion fails closed when no navigation dependency is installed", () => {
	const motion = createEntityMotion();
	motion.spawn( ENTITY, 0 );
	assert.equal( motion.step( 1000 )[0].x, START.x );
});

test("native contact status stops a peer even when the accepted candidate is unchanged", () => {
	const motion = createEntityMotion( undefined, ( _from, to, query ) => {
		query.status = 1;
		return to;
	} );
	motion.spawn( ENTITY, 0 );
	const stopped = motion.step( 20 )[0];
	assert.equal( stopped.x, 101 );
	assert.equal( stopped.moving, false );
	assert.deepEqual( motion.step( 40 ), [] );
});

test("native rejected navigation clears peer travel without accepting the candidate", () => {
	const motion = createEntityMotion( undefined, ( _from, to, query ) => {
		query.status = 0x10000000;
		return to;
	} );
	motion.spawn( ENTITY, 0 );
	const stopped = motion.step( 20 )[0];
	assert.equal( stopped.x, START.x );
	assert.equal( stopped.moving, false );
	assert.deepEqual( motion.step( 40 ), [] );
});
