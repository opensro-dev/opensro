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
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const { createPresentationSamples } = await import( "../../src/engine/runtime/characters/presentation-samples.ts" );
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

for ( const speed of [ 150, 240 ] ) {
	test(`certified remote ${speed}-speed movement recovers a 1000 ms stall without a legacy distance reset`, () => {
		const motion = createEntityMotion( undefined, ( _from, to ) => to );
		const presentation = createPosePresentation();
		motion.spawn( { ...ENTITY, runSpeed: speed }, 0 );
		presentation.origin( 0 );
		/*
		================
		draw
		================
		*/
		function draw( state, at ) {
			const entity = { ...ENTITY, ...state, kind: "player" };
			const { samples, logicalPose } = createPresentationSamples( [ entity ], null );
			assert.equal(
				samples.get( ENTITY.gid )?.transition,
				undefined,
				"native peers have no local transition envelope"
			);
			presentation.samples( samples );
			return presentation.pose( ENTITY.gid, logicalPose( entity ), at / 1000 );
		}
		let state = defined( motion.step( 0 )[0] );
		draw( state, 0 );
		for ( let now = 16; now <= 320; now += 16 ) {
			state = defined( motion.step( now )[0] );
			draw( state, now );
		}
		draw( state, 1320 );
		const before = draw( state, 1332.5 );
		state = defined( motion.step( 1320 )[0] );
		assert.ok( defined( defined( state.movementPath ).walkingPath ).length > 75 );
		const resumed = draw( state, 1332.5 );
		assert.ok( Math.hypot( resumed.x - before.x, resumed.y - before.y, resumed.z - before.z ) < .01 );
		let previous = resumed;
		for ( let now = 1344; now <= 2000; now += 16 ) {
			state = defined( motion.step( now )[0] );
			const shown = draw( state, now );
			assert.ok( shown.x >= previous.x && shown.x <= defined( state.movementPath ).to.x );
			previous = shown;
		}
		assert.ok( Math.abs( previous.x - defined( state.x ) ) < speed * .02 );
	});
}

test("an uncertified remote displacement retains the legacy distance reset", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	presentation.samples( new Map( [ [ ENTITY.gid, { atMs: 0, revision: 1, moving: false } ] ] ) );
	presentation.pose( ENTITY.gid, START, 0 );
	const target = { ...START, x: START.x + 150 };
	presentation.samples( new Map( [ [ ENTITY.gid, { atMs: 1000, revision: 2, moving: false } ] ] ) );
	assert.deepEqual( presentation.pose( ENTITY.gid, target, 1 ), target );
});

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

test("remote recovery publishes the accepted hill and resolved lookahead height", () => {
	const hill = x => Math.max( 0, 5 - Math.abs( x - 105 ) );
	const motion = createEntityMotion( undefined, ( _from, to ) => ({ ...to, y: hill( to.x ) }) );
	motion.spawn( ENTITY, 0 );
	const advanced = motion.step( 200 )[0];
	const path = defined( advanced.movementPath );
	const history = defined( path.walkingPath );
	assert.equal( advanced.x, 110 );
	assert.equal( advanced.y, 0 );
	assert.ok( history.some( point => point.x > 100 && point.x < 110 && point.y > 3 ) );
	assert.ok( history.every( point => point.y === hill( point.x ) ) );
	const uphill = createEntityMotion( undefined, ( _from, to ) => ({ ...to, y: to.x - 100 }) );
	uphill.spawn( ENTITY, 0 );
	const rising = defined( uphill.step( 20 )[0].movementPath );
	assert.equal( rising.to.y, rising.to.x - 100 );
	assert.ok( rising.to.y > rising.from.y );
});

test("remote history is bounded, preserved on retiming and cleared on source reseed", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	motion.spawn( { ...ENTITY, spawnDestination: { ...START, x: 1800 } }, 0 );
	let latest = motion.step( 20 )[0];
	for ( let at = 40; at <= 8000; at += 20 ) latest = motion.step( at )[0];
	const history = defined( defined( latest.movementPath ).walkingPath );
	assert.ok( history.length <= 256 );
	assert.ok( history[0].x > START.x );
	const retimed = defined( motion.speeds( ENTITY, { ...ENTITY, runSpeed: 80 }, 8000 ) );
	assert.ok( defined( defined( retimed.movementPath ).walkingPath ).some( point => point.x < latest.x - 100 ) );
	const reseeded = motion.source( ENTITY, { ...START, x: 900 }, 8000 );
	assert.ok( defined( defined( reseeded.movementPath ).walkingPath ).every( point => point.x >= 900 ) );
	assert.equal( motion.correct( ENTITY, START ).movementPath, undefined );
});

test("remote angular turn retains its admitted corner through a mount gait change", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	const packet = Buffer.alloc( 19 );
	packet.writeUInt32LE( ENTITY.gid );
	packet[5] = 1;
	packet.writeUInt16LE( 0, 6 );
	packet[8] = 1;
	packet.writeUInt16LE( START.regionId, 9 );
	packet.writeInt16LE( START.x * 10, 11 );
	packet.writeFloatLE( START.y, 13 );
	packet.writeInt16LE( START.z * 10, 17 );
	motion.receive( packet, ENTITY, 0 );
	const first = motion.step( 200 )[0];
	const turn = defined( motion.steer( ENTITY, 16384, 200 ) );
	const second = motion.step( 400 )[0];
	const path = defined( defined( second.movementPath ).walkingPath );
	assert.ok( path.some( point => Math.abs( point.x - first.x ) < .001 && Math.abs( point.z - first.z ) < .001 ) );
	assert.ok( path.some( point => point.x < first.x - 2 ) );
	assert.ok( path.some( point => point.z > first.z + 2 ) );
	const walking = defined( motion.mode( { ...ENTITY, mountedOn: 2, movementMode: 2 }, 400 ) );
	assert.ok( defined( defined( walking.movementPath ).walkingPath ).some( point => point.x < turn.x - 2 ) );
});
