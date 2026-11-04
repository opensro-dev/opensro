/*
===========================================================================

mounted-ledge-correction.test.mjs - a mounted correction keeps its wire height

#2574: riding the rock ledge at the 23456/23712 border, the player
stood on the placed object's deck (1514.43) while the ground below
sat at 1403.44. The server's stop named the mount and carried the
deck height, but the mount's entity row had never walked the ledge
through the entity-motion lane, so its surface arbitration re-seated
the correction at the terrain height and core passed that stale row
- not the wire position - to the local mover. Native 775B50/775CB0
pass wire XYZ to that mover's controller.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as bytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const REGION = 0x5ca0;
const LEDGE_REGION = 23456;
const PLAYER_GID = 7;
const MOUNT_GID = 123;
const DECK_Y = 1514.4346618652344;
const TERRAIN_Y = 1403.4390502929687;
const product = await createNavigationResources().resolve(
	bytes( "/assets/world/outdoor/regions/region-5ca0.json" ),
	REGION,
	async p => bytes( p )
);

/*
================
flush
================
*/
function flush( core, now ) {
	core.step( now, false );
	const batch = core.take();
	if ( batch ) core.ack( batch.sequence );
	return batch?.events ?? [];
}

/*
================
mountSpawn

The 0x30D7 COS spawn of the mount, standing on the valley floor
under the ledge: the row the correction later has to move.
================
*/
function mountSpawn() {
	const p = Buffer.alloc( 57 );
	p.writeUInt32LE( 2183, 0 );
	p.writeUInt32LE( MOUNT_GID, 4 );
	p.writeUInt16LE( REGION, 8 );
	p.writeFloatLE( 390, 10 );
	p.writeFloatLE( TERRAIN_Y, 14 );
	p.writeFloatLE( 28, 18 );
	p[25] = 1;
	p[31] = 4;
	p.writeFloatLE( 20, 32 );
	p.writeFloatLE( 50, 36 );
	p.writeFloatLE( 1, 40 );
	p[45] = 1;
	return p;
}

test("a correction naming the mount hands the local mover the wire deck height", () => {
	const core = createWorldCore( () => {} );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 2183, kind: "cos", tidWord: 0x11c6 } ],
		refItemSnapshot: [],
		localPlayerEntry: {
			modelRef: 1933,
			startProfile: { regionId: LEDGE_REGION, x: 417, y: DECK_Y, z: 1876, angle: 0 }
		}
	} );
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ PLAYER_GID, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	core.command( { kind: "navigation", regionId: REGION, bundle: product }, 0 );
	flush( core, 0 );
	core.receive( { opcode: 0x30d7, payload: mountSpawn() }, 0 );
	const ride = Buffer.alloc( 10 );
	ride[0] = 1;
	ride.writeUInt32LE( PLAYER_GID, 1 );
	ride[5] = 1;
	ride.writeUInt32LE( MOUNT_GID, 6 );
	core.receive( { opcode: 0xb4b5, payload: ride }, 0 );
	flush( core, 0 );
	// The server's arrival stop for the ledge walk: it names the mount
	// (775B50 resolves the rider's active mover) and carries the deck
	// height its own walk stands on.
	const payload = Buffer.alloc( 20 );
	payload.writeUInt32LE( MOUNT_GID, 0 );
	payload.writeUInt16LE( REGION, 4 );
	payload.writeFloatLE( 390, 6 );
	payload.writeFloatLE( DECK_Y, 10 );
	payload.writeFloatLE( 28, 14 );
	core.receive( { opcode: 0xb2f5, payload }, 16 );
	const events = flush( core, 16 );
	const gameplay = events.find( e => e.kind === "gameplay" );
	assert.ok( gameplay?.kind === "gameplay" && gameplay.state.pose );
	assert.equal( gameplay.state.pose.regionId, REGION );
	assert.ok( Math.abs( gameplay.state.pose.x - 390 ) < .01, `goal x: ${gameplay.state.pose.x}` );
	assert.ok( Math.abs( gameplay.state.pose.z - 28 ) < .01, `goal z: ${gameplay.state.pose.z}` );
	assert.ok(
		Math.abs( gameplay.state.pose.y - DECK_Y ) < .01,
		`mounted correction must keep the wire deck height ${DECK_Y}, got ${gameplay.state.pose.y}`
	);
	core.dispose();
});
