/*
===========================================================================

recovery-continuity.test.mjs - full-window evidence outlives the frame tail

Feeds synthetic frame observations through the same installer serialized into
the browser. Tests measure motion, not source strings or implementation names.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { installMovementContinuity } from "../../tools/perf/core/movement-continuity.mjs";

/*
================
fixture
================
*/
function fixture() {
	const entity = { gid: 1, movementMode: 3, runSpeed: 50, walkSpeed: 20 };
	const game = { localGid: 1, moving: true, pendingMoves: 0 };
	const target = /** @type {any} */ ({
		__benchRuntime: { gameplay: () => game, entities: () => [ entity ] },
		__worldProbeFrameProfiler: {
			movement: sample => {
				sample.body = { pose: sample.displayed };
			}
		}
	});
	installMovementContinuity( { target } );
	target.__recoveryContinuity.reset();
	return {
		entity,
		game,
		target,
		/*
		================
		frame
		================
		*/
		frame( atMs, x, y = 0, regionId = 25000 ) {
			const pose = { regionId, x, y, z: 0 };
			target.__worldProbeFrameProfiler.movement( { atMs, logical: pose, displayed: pose, revision: 1 } );
		},
		read: () => target.__recoveryContinuity.snapshot()
	};
}

test("ordinary motion and a continuous region crossing have zero excess", () => {
	const f = fixture();
	f.frame( 0, 1918 );
	f.frame( 20, 1919 );
	f.frame( 60, 1, 0, 25001 );
	for ( const channel of Object.values( f.read().channels ) ) {
		assert.equal( channel.pairs, 2 );
		assert.equal( channel.maxExcessXZ, 0 );
		assert.equal( channel.maxExcessXYZ, 0 );
	}
});

test("an early jump remains in totals after more than the recorder tail length", () => {
	const f = fixture();
	f.frame( 0, 0 );
	f.frame( 10, 10 );
	for ( let i = 2; i < 5000; i++ ) f.frame( i * 10, 10 );
	const result = f.read();
	assert.equal( result.frames, 5000 );
	assert.equal( result.channels.displayed.maxExcessXZ, 9.5 );
	assert.equal( result.channels.body.excessFramesXZ, 1 );
	assert.equal( result.events.length, 3 );
	assert.equal( result.events[0].atMs, 10 );
});

test("ground height and long-frame travel stay distinct from horizontal snaps", () => {
	const f = fixture();
	f.frame( 0, 0 );
	f.frame( 10, 0.5, 2 );
	f.frame( 1010, 50.5, 2 );
	const result = f.read();
	assert.equal( result.channels.body.excessFramesXZ, 0 );
	assert.equal( result.channels.body.excessFramesXYZ, 1 );
	assert.equal( result.longFrames, 1 );
	assert.equal( result.maxDtMs, 1000 );
});

test("event storage is bounded and reset and stop delimit separate windows", () => {
	const f = fixture();
	for ( let i = 0; i < 100; i++ ) f.frame( i * 10, i * 10 );
	assert.equal( f.read().events.length, 32 );
	assert.equal( f.read().channels.body.excessEvents.length, 8 );
	assert.equal( f.read().channels.body.excessFramesXZ, 99 );
	f.target.__recoveryContinuity.reset();
	f.frame( 2000, 0 );
	const result = f.target.__recoveryContinuity.stop();
	f.frame( 2010, 100 );
	assert.equal( result.frames, 1 );
	assert.deepEqual( f.read(), result );
});

test("missing speed or a non-increasing timestamp cannot silently pass", () => {
	const f = fixture();
	f.frame( 0, 0 );
	f.frame( 0, 1 );
	f.entity.runSpeed = NaN;
	f.frame( 10, 1 );
	assert.equal( f.read().invalidTime, 1 );
	assert.equal( f.read().invalidSpeed, 1 );
});

test("standing corrections receive no walking budget", () => {
	const f = fixture();
	f.game.moving = false;
	f.frame( 0, 0 );
	f.frame( 100, 1 );
	const channel = f.read().channels.body;
	assert.equal( channel.maxExcessXZ, 1 );
	assert.equal( channel.stationaryExcessFramesXZ, 1 );
	assert.equal( channel.maxStationaryStepXZ, 1 );
});

test("a long frame cannot hide displayed divergence inside its speed budget", () => {
	const f = fixture();
	f.frame( 0, 0 );
	f.target.__worldProbeFrameProfiler.movement( {
		atMs: 1000,
		revision: 1,
		logical: { regionId: 25000, x: 0, y: 0, z: 0 },
		displayed: { regionId: 25000, x: 10, y: 0, z: 0 }
	} );
	const result = f.read();
	assert.equal( result.channels.displayed.maxExcessXZ, 0 );
	assert.equal( result.channels.displayed.maxDeltaDifferenceXZ, 10 );
	assert.equal( result.channels.body.deltaDifferenceFramesXZ, 1 );
	assert.equal( result.events[0].deltaDifferenceXZ, 10 );
});

test("closing the measurement window excludes artifact serialization stalls", () => {
	const f = fixture();
	f.target.__benchLoop = true;
	f.frame( 0, 0 );
	f.frame( 10, 0.5 );
	f.target.__benchLoop = false;
	f.frame( 3000, 50 );
	assert.equal( f.read().frames, 2 );
	assert.equal( f.read().longFrames, 0 );
	assert.equal( f.read().maxDtMs, 10 );
});

test("logical publication jumps cannot evict a visible excess witness", () => {
	const f = fixture();
	f.frame( 0, 0 );
	f.frame( 10, 2 );
	for ( let i = 2; i < 100; i++ ) {
		f.target.__worldProbeFrameProfiler.movement( {
			atMs: i * 10,
			revision: 1,
			logical: { regionId: 25000, x: i * 100, y: 0, z: 0 },
			displayed: { regionId: 25000, x: 2, y: 0, z: 0 }
		} );
	}
	const events = f.read().channels.body.excessEvents;
	assert.equal( events.length, 1 );
	assert.equal( events[0].atMs, 10 );
	assert.equal( events[0].excessXZ, 1.5 );
});
