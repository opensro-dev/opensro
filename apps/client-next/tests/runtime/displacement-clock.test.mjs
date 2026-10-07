/*
===========================================================================

displacement-clock.test.mjs - fixed-speed skills switch on their own clock

The motion owner knows when walking became a dash. Delayed publication must
not extend the old walking velocity through the first displayed dash frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const START = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const WALK_SPEED = 50, DASH_SPEED = 500, DASH_START_MS = 108;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const ENTITY = {
	...START,
	gid: 7,
	refObjId: 1907,
	kind: "player",
	name: "clock",
	heading: 0,
	movementMode: 3,
	walkSpeed: 20,
	runSpeed: WALK_SPEED,
	spawnDestination: { ...START, x: 900 }
};

/*
================
publish
================
*/
function publish( presentation, update, frameMs ) {
	presentation.samples(
		new Map( [ [ ENTITY.gid, {
			atMs: update.poseAtMs,
			revision: update.movementRevision ?? 0,
			moving: update.moving,
			...update.movementPath
		} ] ] )
	);
	return presentation.pose( ENTITY.gid, { ...update, angle: update.heading }, frameMs / 1000 );
}

for ( const publicationDelayMs of [ 0, 16, 32 ] ) {
	test(`a walk-to-dash switch retains its start across ${publicationDelayMs} ms of coalescing`, () => {
		const motion = createEntityMotion(), p = createPosePresentation();
		motion.spawn( ENTITY, 0 );
		p.origin( 0 );
		publish( p, motion.step( 0 )[0], 0 );
		const walking = motion.step( 96 )[0];
		publish( p, walking, 100 );
		let previous = publish( p, walking, 104 ), previousAt = 104;
		const accepted = motion.displace( ENTITY, {
			destination: { ...START, x: 300 },
			kind: 8,
			token: 1,
			gid: ENTITY.gid
		}, DASH_START_MS );
		assert.equal( accepted.movementPath?.startedAtMs, DASH_START_MS );
		const fromX = START.x + DASH_START_MS * WALK_SPEED / 1000;
		let sampledAt = DASH_START_MS + publicationDelayMs;
		let update = motion.step( sampledAt )[0];
		for ( let frameMs = sampledAt + 4; frameMs <= 460; frameMs += 4 ) {
			while ( sampledAt + 16 <= frameMs ) update = motion.step( sampledAt += 16 )[0];
			assert.equal( update.movementPath?.startedAtMs, DASH_START_MS );
			const shown = publish( p, update, frameMs );
			const expectedX = fromX + (frameMs - DASH_START_MS) * DASH_SPEED / 1000;
			assert.ok( Math.abs( shown.x - expectedX ) < 1e-8, `dash clock at ${frameMs}: ${shown.x} != ${expectedX}` );
			const budget = (frameMs - previousAt) * DASH_SPEED / 1000;
			assert.ok( shown.x - previous.x <= budget + 1e-8, "no artificial speed debt after the switch" );
			assert.deepEqual( publish( p, update, frameMs ), shown, "body and camera share the result" );
			previous = shown;
			previousAt = frameMs;
		}
	});
}
