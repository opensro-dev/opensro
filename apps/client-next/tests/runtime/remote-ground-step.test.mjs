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
		for ( let now = 1344; now <= 3504; now += 16 ) {
			state = defined( motion.step( now )[0] );
			const shown = draw( state, now );
			assert.ok( shown.x >= previous.x && shown.x <= defined( state.movementPath ).to.x );
			assert.ok( shown.x - previous.x <= speed * 1.5 * .016 + .01, "peer recovery obeys its gait budget" );
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

test("remote history is bounded and retained through certified retiming and source reseed", () => {
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
	assert.ok( defined( defined( reseeded.movementPath ).walkingPath ).some( point => point.x < 900 ) );
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

for ( const method of [ "source", "correct" ] ) {
	for ( const lateral of [ 0, .015 ] ) {
		test(`peer ${method} preserves delayed terminal history with ${lateral} lateral correction`, () => {
			const hill = x => Math.max( 0, 4 - Math.abs( x - 104 ) );
			const motion = createEntityMotion( undefined, ( _from, to ) => ({ ...to, y: hill( to.x ) }) );
			const presentation = createPosePresentation();
			presentation.origin( 0 );
			motion.spawn( { ...ENTITY, spawnDestination: { ...START, x: 110 } }, 0 );
			let entity = { ...ENTITY, ...defined( motion.step( 0 )[0] ), movementRevision: 1 };
			/*
			================
			draw
			================
			*/
			function draw( at ) {
				const source = createPresentationSamples( [ entity ], null );
				presentation.samples( source.samples );
				return presentation.pose( entity.gid, source.logicalPose( entity ), at );
			}
			draw( 0 );
			draw( 1 );
			entity = { ...entity, ...defined( motion.step( 1000 )[0] ) };
			const before = draw( 1 );
			assert.ok( before.x < 105, "display still trails the accepted arrival" );
			const target = { ...START, x: 110, z: START.z + lateral };
			const update = method === "source" ?
				motion.source( entity, target, 1000 ) :
				motion.correct( entity, target );
			entity = { ...entity, ...update, movementRevision: 2 };
			const history = defined( defined( entity.movementPath ).walkingPath );
			assert.ok( history.some( point => point.y >= 4 ) );
			assert.deepEqual( draw( 1 ), before, "terminal receipt cannot retire unfinished display recovery" );
			let shown = before, crossedHill = false;
			for ( let frame = 1; frame <= 90; frame++ ) {
				shown = draw( 1 + frame / 120 );
				assert.ok( shown.x >= before.x && shown.x <= target.x );
				assert.ok( Math.abs( shown.y - hill( shown.x ) ) < .01 );
				if ( shown.y > 3 ) crossedHill = true;
			}
			assert.ok( crossedHill );
			assert.ok( Math.hypot( shown.x - target.x, shown.z - target.z ) < .01 );
		});
	}
}

test("peer corrections explicitly discard history when their connector is not admitted", () => {
	let allow = true;
	const motion = createEntityMotion( undefined, ( _from, to ) => allow ? to : null );
	motion.spawn( ENTITY, 0 );
	const entity = { ...ENTITY, ...defined( motion.step( 200 )[0] ) };
	allow = false;
	const corrected = motion.correct( entity, { ...START, x: 110, z: 101 } );
	assert.equal( ({ ...entity, ...corrected }).movementPath, undefined );
	assert.equal( corrected.moving, false );
});

test("peer fixed displacement history is never reused by a ground correction", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	const displacement = motion.displace( ENTITY, { kind: 8, gid: 1, token: 9, destination: { ...START, x: 500 } }, 0 );
	const entity = { ...ENTITY, ...displacement };
	assert.equal( motion.correct( entity, START ).movementPath, undefined );
});

for ( const method of [ "receive", "steer", "mode", "speed", "sit", "stop-speed" ] ) {
	test(`peer ${method} retains already displayed flat lookahead beyond its native command timestamp`, () => {
		const motion = createEntityMotion( undefined, ( _from, to ) => to );
		const presentation = createPosePresentation();
		presentation.origin( 0 );
		/** @type {import("../../src/engine/contracts/world").EntityState} */
		let entity = { ...ENTITY, kind: "player", runSpeed: 150, movementRevision: 1 };
		if ( method === "steer" ) {
			const packet = Buffer.alloc( 19 );
			packet.writeUInt32LE( ENTITY.gid );
			packet[5] = 1;
			packet[8] = 1;
			packet.writeUInt16LE( START.regionId, 9 );
			packet.writeInt16LE( START.x * 10, 11 );
			packet.writeInt16LE( START.z * 10, 17 );
			motion.receive( packet, entity, 0 );
		} else motion.spawn( entity, 0 );
		/*
		================
		draw
		================
		*/
		function draw( at ) {
			const source = createPresentationSamples( [ entity ], null );
			presentation.samples( source.samples );
			return presentation.pose( entity.gid, source.logicalPose( entity ), at );
		}
		entity = { ...entity, ...defined( motion.step( 0 )[0] ) };
		draw( 0 );
		entity = { ...entity, ...defined( motion.step( 16 )[0] ) };
		draw( .016 );
		const shown = draw( .064 );
		assert.ok( shown.x > 109, "display occupies checked lookahead before an older worker command arrives" );
		if ( method === "receive" ) {
			const packet = Buffer.alloc( 14 );
			packet.writeUInt32LE( ENTITY.gid );
			packet[4] = 1;
			packet.writeUInt16LE( START.regionId, 5 );
			packet.writeInt16LE( 90, 7 );
			packet.writeInt16LE( 100, 11 );
			entity = { ...entity, movementPath: motion.receive( packet, entity, 32 ) };
			entity = { ...entity, ...defined( motion.step( 32 )[0] ) };
		} else if ( method === "steer" ) entity = { ...entity, ...defined( motion.steer( entity, 16384, 32 ) ) };
		else if ( method === "mode" || method === "sit" ) {
			entity = {
				...entity,
				...defined( motion.mode( { ...entity, movementMode: method === "sit" ? 0 : 2 }, 32 ) )
			};
		} else {entity = {
				...entity,
				...defined( motion.speeds( entity, { ...entity, runSpeed: method === "stop-speed" ? 0 : 30 }, 32 ) )
			};}
		entity = { ...entity, movementRevision: 2 };
		assert.ok( defined( entity.movementPath ).walkingPath, "command retirement must retain certified geometry" );
		const received = draw( .064 );
		assert.ok(
			Math.hypot( received.x - shown.x, received.y - shown.y, received.z - shown.z ) < .0001,
			"native command replacement preserves the pose already displayed at this timestamp"
		);
		for ( let frame = 1; frame <= 32; frame++ ) {
			const now = 64 + frame * 16;
			const step = motion.step( now )[0];
			if ( step ) entity = { ...entity, ...step };
			const shown = draw( now / 1000 );
			const proof = defined( defined( entity.movementPath ).walkingPath );
			assert.ok(
				proof.some( ( to, index ) => {
					const from = proof[index - 1];
					if ( !from ) return false;
					const dx = to.x - from.x, dy = to.y - from.y, dz = to.z - from.z;
					const length2 = dx * dx + dy * dy + dz * dz;
					const fraction = length2 ?
						Math.max(
							0,
							Math.min(
								1,
								((shown.x - from.x) * dx + (shown.y - from.y) * dy + (shown.z - from.z) * dz) / length2
							)
						) :
						0;
					return Math.hypot(
						shown.x - from.x - fraction * dx,
						shown.y - from.y - fraction * dy,
						shown.z - from.z - fraction * dz
					) < .01;
				} ),
				"subsequent recovery remains on the admitted terrain chain"
			);
		}
	});
}
