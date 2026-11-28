/*
===========================================================================

perf-scene-evidence.test.mjs - the bench's verdict on what it drew

judgeFrames is the line between "the bench ran" and "the bench ran the
scene it claims": each promised property must fail the verdict when its
frames do not show it.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { judgeFrames, SNOW_MODE } from "../../tools/perf/core/scene-evidence.mjs";

const ALL = Object.freeze( { camera: true, actors: true, dress: true, imbue: true, snow: true } );

/*
================
frame

One record: the camera at yaw, peer 7 at x wearing chest (0 = bare),
imbued or not, and the weather mode.
================
*/
function frame( { yaw = 0, x = 0, chest = 3001, imbued = false, mode = SNOW_MODE } = {} ) {
	return {
		camera: { yaw, pitch: 0.4, distance: 30 },
		weather: { mode, amount: 255 },
		heapMB: 40,
		picture: { lumaMean: 120, lumaSpread: 45 },
		peers: [ { gid: 7, x, z: 0, chest, imbued } ]
	};
}

test("a busy snowy drag passes only when every promise shows in the frames", () => {
	const good = [
		frame(),
		frame( { yaw: 0.2, x: 2, chest: 0 } ),
		frame( { yaw: 0.4, x: 4, chest: 3001, imbued: true } )
	];
	const verdict = judgeFrames( good, ALL );
	assert.equal( verdict.pass, true, verdict.failed.join( "; " ) );
	assert.deepEqual( [ verdict.cameraTurns, verdict.peerMoves, verdict.dressChanged, verdict.imbuedPeers ], [
		2,
		2,
		1,
		1
	] );
	const broken = {
		"camera never moved": good.map( f => ({ ...f, camera: good[0].camera }) ),
		"no actor moved": good.map( f => ({ ...f, peers: f.peers.map( p => ({ ...p, x: 0 }) ) }) ),
		"no actor changed clothes": good.map( f => ({ ...f, peers: f.peers.map( p => ({ ...p, chest: 3001 }) ) }) ),
		"no actor wore the imbue": good.map( f => ({ ...f, peers: f.peers.map( p => ({ ...p, imbued: false }) ) }) ),
		"not snowing in every frame": [ ...good.slice( 0, 2 ), { ...good[2], weather: { mode: 1, amount: 0 } } ],
		// The client held a living scene, but the page presented a blank white frame.
		"a frame shows no drawn scene": [ ...good.slice( 0, 2 ), {
			...good[2],
			picture: { lumaMean: 254, lumaSpread: 1.2 }
		} ]
	};
	for ( const [reason, frames] of Object.entries( broken ) ) {
		const failed = judgeFrames( frames, ALL );
		assert.equal( failed.pass, false, reason );
		assert.deepEqual( failed.failed, [ reason ] );
	}
	// A frame without a picture record was never looked at: not drawn either.
	assert.equal( judgeFrames( good.map( ( { picture, ...rest } ) => rest ), ALL ).pass, false );
	// A still scene promises nothing it does not show.
	assert.equal( judgeFrames( broken["camera never moved"], { ...ALL, camera: false } ).pass, true );
});
