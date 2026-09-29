/*
===========================================================================

direction-movement.test.mjs - tests for direction-movement.ts, the direction
walk of movement.ts and motion.ts, and the 0xB738 decoder

BUG-033: a click that misses the ground walks the camera ray's direction
until something blocks it. These tests drive the shipped modules through
the native source loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const direction = await import( "../../src/engine/foundation/gameplay/direction-movement.ts" );
const { decodeNativeMovement } = await import( "../../src/engine/foundation/gameplay/native-movement.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);

const GID = 7;
const EAST = 0;
const SOUTH = 0x4000;
/** @type {import('../../src/engine/contracts/gameplay').Pose} */
const pose = { regionId: 0x0101, x: 100, y: 10, z: 100, angle: 0 };

/*
================
ack

A 0xB738 for GID: mode 0 along heading, or mode 1 to x; optional source.
================
*/
/**
 * @param {{ heading?: number; x?: number; source?: import('../../src/engine/contracts/gameplay').Pose }} shape
 */
function ack( { heading, x = 0, source } ) {
	const angular = heading !== undefined, offset = angular ? 8 : 13;
	const p = Buffer.alloc( offset + 1 + (source ? 10 : 0) );
	p.writeUInt32LE( GID );
	if ( angular ) {
		p[5] = direction.ANGULAR_FLAG_GO;
		p.writeUInt16LE( heading, 6 );
	} else {
		p[4] = 1;
		p.writeUInt16LE( pose.regionId, 5 );
		p.writeInt16LE( x, 7 );
		p.writeInt16LE( pose.y, 9 );
		p.writeInt16LE( pose.z, 11 );
	}
	if ( source ) {
		p[offset] = 1;
		p.writeUInt16LE( source.regionId, offset + 1 );
		p.writeInt16LE( source.x * 10, offset + 3 );
		p.writeFloatLE( source.y, offset + 5 );
		p.writeInt16LE( source.z * 10, offset + 9 );
	}
	return new Uint8Array( p );
}

/*
================
receipt

The predicted-movement receipt (opcode 10) for command id.
================
*/
/**
 * @param {number} id
 * @param {import('../../src/engine/contracts/gameplay').Pose} spawn
 * @param {{ from: import('../../src/engine/contracts/gameplay').Pose; startedAtMs: number; arrivesAtMs: number }} segment
 */
function receipt( id, spawn, segment ) {
	return new TextEncoder().encode( JSON.stringify( {
		v: 1,
		id,
		gid: GID,
		accepted: true,
		serverTimeMs: 0,
		world: { spawn, moveSegment: segment }
	} ) );
}

/*
================
remote

A running remote player at pose.
================
*/
function remote() {
	/** @type {import('../../src/engine/contracts/world').EntityState} */
	const entity = {
		...pose,
		gid: GID,
		refObjId: 1907,
		name: "peer",
		kind: "player",
		heading: 0,
		movementMode: 3,
		walkSpeed: 20,
		runSpeed: 50
	};
	return entity;
}

/*
================
groundQuery
================
*/
/** @param {number[]} delta */
function groundQuery( delta ) {
	return { originRegion: pose.regionId, ray: { start: [ 0, 50, 0 ], delta }, terrainDepth: null };
}

test("0xB738 mode 0 walks a direction only with a source block", () => {
	const walk = decodeNativeMovement( ack( { heading: SOUTH, source: pose } ), pose );
	assert.equal( walk.kind, "direction" );
	assert.equal( walk.heading, SOUTH );
	const keep = decodeNativeMovement( ack( { heading: SOUTH } ), { ...pose, x: 5 } );
	assert.equal( keep.kind, "keep" );
	assert.equal( keep.to.x, 5, "a keep changes nothing" );
	assert.equal( decodeNativeMovement( ack( { x: 300 } ), pose ).kind, "destination" );
});

test("the pick-miss heading drops the ray's height; a vertical ray keeps the heading", () => {
	assert.equal( direction.pickMissHeading( groundQuery( [ 700, 700, 0 ] ), pose ), EAST );
	assert.equal( direction.pickMissHeading( groundQuery( [ 0, -10, 900 ] ), pose ), 16383 );
	assert.equal( direction.pickMissHeading( groundQuery( [ 0, -1000, 0 ] ), { ...pose, angle: 1234 } ), 1234 );
});

test("a miss within 5 degrees of the walked direction is dropped, only while walking", () => {
	const fiveDegrees = Math.floor( 65535 * 5 / 360 );
	assert.equal( direction.directionTurnSkipped( true, 20000, 20000 + fiveDegrees ), true );
	assert.equal( direction.directionTurnSkipped( true, 20000, 20000 + fiveDegrees + 20 ), false );
	assert.equal( direction.directionTurnSkipped( false, 20000, 20000 ), false );
	// No wrap in model yaw: 0xC000 is model yaw 0 (bearing 270 + 90).
	assert.equal( direction.directionTurnSkipped( true, 0xc000 - 10, 0xc000 + 10 ), false );
});

test("a leg is folded into the region it ends in and its body is the 0x7738 angular form", () => {
	const end = direction.directionLegEnd( { ...pose, x: 1500 }, EAST );
	assert.equal( end.regionId, 0x0102 );
	assert.ok( Math.abs( end.x - 580 ) < 1e-9 );
	assert.deepEqual( [ ...direction.directionMoveBody( 0x1234 ) ], [ 0, 1, 0x34, 0x12 ] );
	assert.throws( () => direction.directionMoveBody( 70000 ) );
});

test("drift reconciliation keeps the heading inside 5 units and chases a reference ahead or behind", () => {
	assert.deepEqual( direction.directionDrift( pose, { ...pose, x: 103 }, EAST, 50 ), { heading: EAST, factor: 1 } );
	const ahead = direction.directionDrift( pose, { ...pose, x: 120 }, EAST, 50 );
	assert.equal( ahead.factor, 1.10000002 );
	assert.equal( ahead.heading, EAST );
	assert.equal( direction.directionDrift( pose, { ...pose, x: 80 }, EAST, 50 ).factor, 0.899999976 );
	const beside = direction.directionDrift( pose, { ...pose, z: 120 }, EAST, 50 );
	assert.equal( beside.factor, 1, "a reference abeam is neither ahead nor behind" );
	assert.ok( beside.heading > 0 && beside.heading < SOUTH, "the walker bends toward the server line" );
});

test("a ground click walks a hit point, walks the direction of a miss and skips a 5 degree turn", () => {
	const query = groundQuery( [ 1000, 0, 0 ] );
	assert.deepEqual( direction.worldPointAction( pose, null, query, false ), {
		kind: "walk-direction",
		heading: EAST
	} );
	assert.deepEqual( direction.worldPointAction( pose, null, query, true ), { kind: "none" } );
	const point = { regionId: pose.regionId, x: 400, y: 10, z: 100 };
	assert.deepEqual( direction.worldPointAction( pose, point, query, true ), {
		kind: "walk-to",
		destination: { ...point, angle: 0 }
	} );
	const cancel = direction.targetActionCancel();
	assert.deepEqual( [ cancel.opcode, ...cancel.payload ], [ 0x72cd, 2 ] );
});

test("the local walk sends the angular envelope and keeps walking past its first leg", () => {
	/** @type {import('../../src/engine/contracts/network').WireFrame[]} */
	const frames = [];
	const m = createMovement( frame => frames.push( frame ) );
	m.seed( pose );
	const frame = m.direct( EAST, 0 );
	assert.deepEqual( [ ...frame.payload ], [ 1, 1, 0, 0, 0, 0, 1, 0, 0 ] );
	assert.equal( frames.length, 1 );
	assert.equal( m.state().directionWalk, EAST );

	const legEnd = { ...pose, x: 1100 };
	m.receive( receipt( 1, legEnd, { from: pose, startedAtMs: 0, arrivesAtMs: 20000 } ), 0, GID );
	for ( let now = 100; now <= 30000; now += 100 ) m.step( now );
	const walked = defined( m.state().pose );
	assert.equal( walked.regionId, 0x0101 );
	assert.ok( Math.abs( walked.x - 1600 ) < 1e-6, "walked to " + walked.x );
	assert.equal( m.state().moving, true );

	m.correct( { ...walked, angle: EAST } );
	assert.equal( m.state().directionWalk, undefined );
	m.step( 31000 );
	assert.equal( defined( m.state().pose ).x, walked.x, "a correction ends the walk" );
});

test("a blocked server leg ends the local walk at the contact", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.direct( EAST, 0 );
	const wall = { ...pose, x: 400 };
	m.receive( receipt( 1, wall, { from: pose, startedAtMs: 0, arrivesAtMs: 6000 } ), 0, GID );
	for ( let now = 100; now <= 12000; now += 100 ) m.step( now );
	assert.equal( defined( m.state().pose ).x, 400 );
	assert.equal( m.state().directionWalk, undefined );
	assert.equal( m.state().moving, false );
});

test("a local mode-0 acknowledgement with a source starts the walk; without one it keeps the path", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.native( ack( { x: 300 } ), 0, GID );
	m.native( ack( { heading: SOUTH } ), 1000, GID );
	m.step( 2000 );
	assert.ok( Math.abs( defined( m.state().pose ).x - 200 ) < 1e-6, "the destination walk kept running" );
	m.native( ack( { heading: EAST, source: { ...pose, x: 200 } } ), 2000, GID );
	for ( let now = 2100; now <= 42000; now += 100 ) m.step( now );
	assert.ok( Math.abs( defined( m.state().pose ).x - 2200 + 1920 ) < 1e-6 );
	assert.equal( defined( m.state().pose ).regionId, 0x0102 );
});

test("a remote direction walk renews its legs and turns on 0xB2CF", () => {
	const motion = createEntityMotion();
	let entity = remote();
	/** @param {number} now */
	const step = now => {
		for ( const row of motion.step( now ) ) entity = { ...entity, ...row };
		return entity;
	};
	motion.receive( ack( { heading: EAST, source: pose } ), entity, 0 );
	step( 20000 );
	assert.ok( Math.abs( step( 30000 ).x - 1600 ) < 1e-6 );
	const turned = motion.steer( entity, SOUTH, 30000 );
	assert.equal( turned?.heading, SOUTH );
	const after = step( 31000 );
	assert.ok( Math.abs( after.x - 1600 ) < .1 && Math.abs( after.z - 150 ) < 1e-3, JSON.stringify( after ) );

	const idle = createEntityMotion(), still = { ...remote(), x: 5 };
	assert.equal( idle.receive( ack( { heading: SOUTH } ), still, 0 ).to.x, 5 );
	assert.deepEqual( idle.step( 1000 ), [], "a keep starts nothing" );
	assert.equal( idle.steer( still, SOUTH, 0 )?.heading, SOUTH, "an idle mover turns in place" );
});
