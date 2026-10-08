/*
===========================================================================

arena-correction.test.mjs - local corrections use the local navigation owner

The entity table does not advance the local player's predicted walk. Its
old terrain position must not replace a server position on Lord's arena.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as bytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const REGION = 0x697e;
const GID = 7;
const FLOOR_Y = 430.57059461810354;
const product = await createNavigationResources().resolve(
	bytes( "/assets/world/outdoor/regions/region-697e.json" ),
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

for ( const opcode of [ 0xb2f5, 0x30e3 ] ) {
	test(`arena correction ${opcode.toString( 16 )} cannot inherit the local entity's stale terrain`, () => {
		const core = createWorldCore( () => {} );
		core.bootstrap( {
			protocolVersion: 2,
			nativeResult: 1,
			refObjSnapshot: [],
			refItemSnapshot: [],
			localPlayerEntry: {
				modelRef: 1933,
				startProfile: { regionId: REGION, x: 1908, y: 268.8196356, z: 507, angle: 0 }
			}
		} );
		core.receive( { opcode: 0x32a6, payload: Buffer.from( [ GID, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
		core.command( { kind: "navigation", regionId: REGION, bundle: product }, 0 );
		flush( core, 0 );
		const payload = Buffer.alloc( 20 ), offset = opcode === 0x30e3 ? 0 : 4;
		payload.writeUInt32LE( GID, opcode === 0x30e3 ? 16 : 0 );
		payload.writeUInt16LE( REGION, offset );
		payload.writeFloatLE( 1908, offset + 2 );
		payload.writeFloatLE( FLOOR_Y, offset + 6 );
		payload.writeFloatLE( 507, offset + 10 );
		core.receive( { opcode, payload }, 16 );
		const event = flush( core, 16 ).find( e => e.kind === "gameplay" );
		assert.ok( event?.kind === "gameplay" && event.state.pose );
		assert.ok( Math.abs( event.state.pose.y - FLOOR_Y ) < .001, `arena height: ${event.state.pose.y}` );
		core.dispose();
	});
}
