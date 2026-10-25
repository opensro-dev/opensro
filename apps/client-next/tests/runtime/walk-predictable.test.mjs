/*
===========================================================================

walk-predictable.test.mjs - the worker's walkPredictable flag (GH #1440)

The main-thread mover predicts a ground click only while the worker's
latest publication says a click would walk at once. The flag must agree
with what the command path does, state by state, and must republish when
a gate opens or closes with no command (a step).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
const { createGameplay } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ) ).href
);

const POSE = { regionId: 0x6b4f, x: 60, y: 10, z: 100, angle: 0 };
// movement.ts OP_PREDICTED_MOVE: the internal frame a walk request sends.
const PREDICTED_MOVE = 9;
const STANDING = { ...POSE, gid: 7, heading: 0, appearanceState: [ 1, 0, 0 ] };
// The riding mount's COS reference and gid (a band 1 vehicle).
const MOUNT = { gid: 9, refObjId: 90, tidWord: 0x09c6, band: 1 };
// A ridden transport (band 2), whose record carries a dead flag.
const TRANSPORT = { gid: 11, refObjId: 91, tidWord: 0x11c6, band: 2 };
const STATES = {
	standing: { local: STANDING },
	seated: { local: { ...STANDING, movementMode: 4 } },
	dead: { local: { ...STANDING, appearanceState: [ 2, 0, 0 ] } },
	// Riding a mount the worker holds no live COS record for: no authority.
	mounted: { local: { ...STANDING, mountedOn: MOUNT.gid } },
	// Riding the live active mount: the click walks the mount (#1566).
	riding: { local: { ...STANDING, mountedOn: MOUNT.gid }, cos: MOUNT },
	// Riding a transport whose record says it is dead: no authority.
	"dead transport": { local: { ...STANDING, mountedOn: TRANSPORT.gid }, cos: { ...TRANSPORT, dead: true } }
};

/*
================
started

A fresh gameplay owner in the given local state: the flag it publishes,
and whether a click then sent a walk request.
================
*/
/**
 * @param {{ local: any, cos?: { gid: number, refObjId: number, band: number, dead?: boolean } }} state
 */
function started( { local, cos } ) {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		refObjSnapshot: [ MOUNT, TRANSPORT ].map( cos => ({
			kind: "cos",
			refObjId: cos.refObjId,
			tidWord: cos.tidWord
		}) )
	} );
	game.seed( local );
	if ( cos ) {
		// 0x3158 (830EC0): gid, ref, hp, mp, status; a transport (band 2)
		// then carries its dead flag (cos-record.ts).
		const record = new Uint8Array( cos.band === 2 ? 21 : 17 ), view = new DataView( record.buffer );
		view.setUint32( 0, cos.gid, true );
		view.setUint32( 4, cos.refObjId, true );
		view.setUint32( 8, 100, true );
		if ( cos.band === 2 ) view.setUint32( 17, cos.dead ? 1 : 0, true );
		game.receive( { opcode: 0x3158, payload: record }, 0 );
	}
	game.step( 50, local );
	const flag = game.take()?.walkPredictable;
	try {
		game.command( { kind: "move", destination: { ...POSE, x: 300 } }, 100, undefined, local );
	} catch {
		// A mounted click without its COS authority throws; it does not walk.
	}
	game.dispose();
	return { flag, walked: sent.some( f => f.opcode === PREDICTED_MOVE ) };
}

test("walkPredictable is true exactly in the states where a click walks at once", () => {
	for ( const [name, state] of Object.entries( STATES ) ) {
		const { flag, walked } = started( state );
		assert.equal( flag, name === "standing" || name === "riding", name + ": published flag" );
		// Every state agrees with the command path, riding included (#1566).
		assert.equal( walked, flag, name + ": the command path agrees" );
	}
});

/*
================
pickupFlag

A fresh gameplay owner in the given local state: the pickupPredictable
flag it publishes (#1567). The mover predicts a pickup leg only under
this flag, whose gate is the worker's run-up gate (pickupPredictableNow),
not the ground click's: any mount closes it even where a rider's ground
walk is predictable.
================
*/
function pickupFlag( local ) {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	game.step( 50, local );
	const flag = game.take()?.pickupPredictable;
	game.dispose();
	return flag;
}

test("pickupPredictable is the run-up's own gate, state by state", () => {
	// Only the local entity feeds this gate (pickupPredictableNow reads no
	// COS record): riding publishes false although the ground gate walks a
	// rider (#1566) - the run-up never starts for a mounted player.
	for ( const [name, state] of Object.entries( STATES ) ) {
		assert.equal( pickupFlag( state.local ), name === "standing", name + ": published flag" );
	}
});

test("a gate that closes with no command republishes the flag on the next step", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( STANDING );
	game.step( 50, STANDING );
	assert.equal( game.take()?.walkPredictable, true );
	assert.equal( game.take(), null, "nothing changed" );
	game.step( 66, STATES.seated.local );
	assert.equal( game.take()?.walkPredictable, false );
	game.step( 83, STANDING );
	assert.equal( game.take()?.walkPredictable, true );
	game.dispose();
});
