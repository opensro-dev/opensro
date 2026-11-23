/*
===========================================================================

position-skill.test.mjs - tests for position-skill.ts, combat.ts,
gameplay.ts, movement.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { positionSkillGoal, positionSkillRequest } = await import(
	"../../src/engine/foundation/gameplay/position-skill.ts"
);
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const pose = { regionId: 0x61a8, x: 100, y: 20, z: 100, angle: 0 };
const query = { originRegion: pose.regionId, ray: { start: [ 0, 100, 0 ], delta: [ 3, 0, 4 ] }, terrainDepth: null };
const ref = {
	id: 19636,
	group: 768,
	level: 1,
	status: false,
	effectRider: false,
	ui: {
		name: "Ghost Walk - Shadow",
		trainable: true,
		spCost: 1,
		targetRequired: true,
		groundTarget: true,
		cooldownMs: 5000,
		cooldownGroup: 59,
		masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
		prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
	}
};
test("ground action encodes flag 2 and signed coordinates; sky aim uses horizontal direction", () => {
	const goal = positionSkillGoal( pose, query, null );
	assert.deepEqual( goal, { ...pose, x: 400, z: 500 } );
	const p = positionSkillRequest( 19636, { ...pose, regionId: 0x8001, x: -12.9, z: 200.9 } ).payload,
		v = new DataView( p.buffer );
	assert.equal( p.length, 15 );
	assert.deepEqual( [ ...p.slice( 0, 2 ) ], [ 1, 4 ] );
	assert.equal( p[6], 2 );
	assert.equal( v.getUint32( 2, true ), 19636 );
	assert.equal( v.getInt16( 9, true ), -12 );
	assert.equal( v.getInt16( 13, true ), 200 );
	assert.throws( () => positionSkillRequest( 19636, { ...pose, x: Infinity } ) );
	assert.equal(
		positionSkillGoal( pose, { ...query, ray: { start: [ 0, 0, 0 ], delta: [ 0, 1, 0 ] } }, null ),
		null
	);
});
test("activation uses pointer query without an enemy or a second click", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( { character: { skills: [ 19636 ] }, refSkillSnapshot: [ ref ] } );
	g.seed( { ...pose, gid: 1, heading: 0 } );
	assert.throws( () => g.command( { kind: "skill", skillId: 19636 }, 100 ), /Point into the world/ );
	g.command( { kind: "skill", skillId: 19636, gid: 999, query }, 100 );
	assert.equal( sent.length, 0, "missing navigation must not submit" );
	g.command( {
		kind: "navigation",
		regionId: pose.regionId,
		bundle: {
			regionId: pose.regionId,
			complete: true,
			objects: [],
			navmesh: {
				regionSize: 1920,
				tileSize: 20,
				tilesPerAxis: 96,
				regions: [ {
					dx: 0,
					dz: 0,
					blockedTiles: Buffer.alloc( 9216 ).toString( "base64" ),
					tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
					cells: { count: 1 },
					objects: []
				} ]
			}
		}
	}, 100 );
	g.command( { kind: "skill", skillId: 19636, gid: 999, query }, 100 );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].opcode, 0x72cd );
	assert.equal( sent[0].payload[6], 2 );
	assert.equal( defined( defined( g.take() ).pose ).x, 100, "request must not predict successful relocation" );
	g.dispose();
});
function open() {
	const p = Buffer.alloc( 27 );
	p[0] = 1;
	p.writeUInt32LE( 19636, 2 );
	p.writeUInt32LE( 1, 6 );
	p.writeUInt32LE( 9, 10 );
	p[18] = 8;
	p.writeUInt16LE( pose.regionId, 19 );
	p.writeInt16LE( 315, 21 );
	p.writeInt16LE( 20, 23 );
	p.writeInt16LE( 100, 25 );
	return p;
}
test("release preserves dash; guided arrival retires it once and reset removes pending arrival", () => {
	const c = createCombat();
	c.cooldownReferences( 1, [ { id: 19636, groundTarget: true, cooldownMs: 5000, cooldownGroup: 59 } ] );
	c.receive( 0xb245, open(), 100 );
	assert.equal( c.takeDisplacements()[0].kind, 8 );
	c.guidedArrival( 9, 530 );
	const release = Buffer.alloc( 10 );
	release[0] = 1;
	release.writeUInt32LE( 9, 1 );
	release.writeUInt32LE( 1, 5 );
	c.receive( 0xb505, release, 100 );
	c.step( 529 );
	assert.equal( c.state().casts[0].cancelledAtMs, undefined );
	assert.deepEqual( c.takeCancellations(), [] );
	c.step( 530 );
	assert.equal( c.state().casts[0].cancelledAtMs, 530 );
	assert.deepEqual( c.takeCancellations(), [ 9 ] );
	c.step( 730 );
	assert.equal( c.state().casts.length, 0 );
	c.receive( 0xb245, open(), 1000 );
	c.guidedArrival( 9, 1430 );
	c.clear();
	c.step( 2000 );
	assert.deepEqual( c.takeCancellations(), [] );
});
test("dash blocks replacement input and death cancels its arrival through the combat owner", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( { character: { skills: [ 19636 ] }, refSkillSnapshot: [ ref ] } );
	g.seed( { ...pose, gid: 1, heading: 0 } );
	g.receive( { opcode: 0xb245, payload: open() }, 100 );
	const travel = g.takeDisplacements()[0], arrival = g.displace( travel, 100 );
	assert.equal( arrival, 530 );
	g.guidedArrival( travel.token, arrival );
	g.command( { kind: "skill", skillId: 19636, query }, 200 );
	g.command( { kind: "move", destination: { ...pose, x: 900 } }, 200 );
	assert.equal( sent.length, 0 );
	g.die( 1, 300 );
	assert.deepEqual( g.takeCancellations(), [ 9 ] );
	g.step( 1000 );
	assert.deepEqual( g.takeCancellations(), [] );
	assert.equal( defined( g.take() ).moving, false );
	g.dispose();
});
test("pointer goal stops at resident collision before sending the ground request", () => {
	const m = createMovement( () => {} ), blocked = Buffer.alloc( 9216 );
	blocked[5 * 96 + 8] = 1;
	m.seed( pose );
	m.navigation( pose.regionId, {
		navmesh: {
			regionSize: 1920,
			tileSize: 20,
			tilesPerAxis: 96,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: blocked.toString( "base64" ),
				tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
				cells: { count: 1 },
				objects: []
			} ]
		}
	} );
	const to = m.groundSkillGoal( { ...query, ray: { start: [ 0, 100, 0 ], delta: [ 1, 0, 0 ] } }, 100 );
	assert.ok( defined( to ).x < 160 && defined( to ).x > 159 );
	assert.equal( defined( to ).z, 100 );
	assert.equal( defined( m.state().pose ).x, 100 );
});

test("a received dash ends at the server-published destination, never a client-clipped substitute", () => {
	// #4154: the worker core used to re-clip the published destination on the
	// client navigation before stepping it. From a stale entity row (or any
	// spot where the two navigations disagree) the clip stopped or slid the
	// dash at an edge while the server had already seated the actor at the
	// published point, and nothing reconciled the presented rest pose with
	// the server's position afterwards. The destination is authoritative:
	// the wall band below used to cut a peer's dash short from the stale
	// row; it has no say over a received displacement any more.
	const g = createGameplay( () => {} );
	g.bootstrap( { character: { skills: [ 19636 ] }, refSkillSnapshot: [ ref ] } );
	g.seed( { ...pose, gid: 1, heading: 0 } );
	const blocked = Buffer.alloc( 9216 );
	for ( let zTile = 10; zTile <= 20; zTile++ ) blocked[zTile * 96 + 15] = 1; // x 300..320, z 200..420
	g.command( {
		kind: "navigation",
		regionId: pose.regionId,
		bundle: {
			regionId: pose.regionId,
			complete: true,
			objects: [],
			navmesh: {
				regionSize: 1920,
				tileSize: 20,
				tilesPerAxis: 96,
				regions: [ {
					dx: 0,
					dz: 0,
					blockedTiles: blocked.toString( "base64" ),
					tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
					cells: { count: 1 },
					objects: []
				} ]
			}
		}
	}, 100 );
	const arrival = g.displace(
		{ gid: 1, token: 9, kind: 8, destination: { regionId: pose.regionId, x: 310, y: 20, z: 100 } },
		100
	);
	assert.ok( arrival !== undefined && arrival > 100 );
	g.step( arrival + 1 );
	const end = defined( g.take() );
	assert.equal( end.pose?.x, 310, "the dash ends at the published point" );
	assert.equal( end.pose?.z, 100 );
	g.dispose();
});
