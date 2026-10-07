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
		const motion = createEntityMotion( undefined, ( _from, to ) => to ), p = createPosePresentation();
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
		const fromX = Math.fround(
			Math.fround( START.x + Math.fround( WALK_SPEED * Math.fround( .096 ) ) ) +
				Math.fround( WALK_SPEED * Math.fround( (DASH_START_MS - 96) / 1000 ) )
		);
		assert.equal( accepted.movementPath.from.x, fromX );
		let sampledAt = DASH_START_MS + publicationDelayMs;
		let update = motion.step( sampledAt )[0];
		for ( let frameMs = sampledAt + 4; frameMs <= 460; frameMs += 4 ) {
			while ( sampledAt + 16 <= frameMs ) update = motion.step( sampledAt += 16 )[0];
			assert.equal( update.movementPath?.startedAtMs, DASH_START_MS );
			const shown = publish( p, update, frameMs );
			// Native walking stores float32; the presentation anchor can retain less than one local-coordinate ULP.
			const expectedX = fromX + (frameMs - DASH_START_MS) * DASH_SPEED / 1000;
			assert.ok(
				Math.abs( shown.x - expectedX ) < 2 ** -16,
				`dash clock at ${frameMs}: ${shown.x} != ${expectedX}`
			);
			const budget = (frameMs - previousAt) * DASH_SPEED / 1000;
			assert.ok( shown.x - previous.x <= budget + 2 ** -16, "no artificial speed debt after the switch" );
			assert.deepEqual( publish( p, update, frameMs ), shown, "body and camera share the result" );
			previous = shown;
			previousAt = frameMs;
		}
	});
}

test("the first timed dash publication preserves the preceding untimed display frame", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to ), p = createPosePresentation();
	p.origin( 0 );
	for ( const atMs of [ 0, 96, 100, 104 ] ) p.pose( ENTITY.gid, START, atMs / 1000 );
	const accepted = motion.displace( ENTITY, {
		destination: { ...START, x: 300 },
		kind: 8,
		token: 1,
		gid: ENTITY.gid
	}, 100 );
	const first = publish( p, accepted, 108 );
	assert.ok(
		Math.abs( first.x - START.x - 4 * DASH_SPEED / 1000 ) < 1e-8,
		"the first timed sample gets the real 4 ms display interval, not 8 ms of hidden worker time"
	);
	let previous = first;
	for ( let atMs = 112; atMs <= 600; atMs += 4 ) {
		const update = motion.step( atMs )[0] ?? { ...accepted, x: 300, moving: false, poseAtMs: atMs };
		const shown = publish( p, update, atMs );
		assert.ok( shown.x >= previous.x - 1e-8 );
		assert.ok( shown.x - previous.x <= 4 * DASH_SPEED / 1000 + .5, "no later repayment burst" );
		previous = shown;
	}
	assert.ok( Math.abs( previous.x - 300 ) < 1e-8, "the handoff still reaches the authoritative endpoint" );
});

test("an ineligible relocation does not inherit the untimed dash bridge", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	p.pose( ENTITY.gid, START, .104 );
	const from = { ...START, x: 140 }, to = { ...START, x: 340 };
	p.samples(
		new Map( [ [ ENTITY.gid, {
			atMs: 100,
			revision: 1,
			moving: true,
			from,
			to,
			durationMs: 400,
			startedAtMs: 100,
			displacement: true,
			transition: { relocation: 1, reason: "correction", eligible: false }
		} ] ] )
	);
	assert.equal( p.pose( ENTITY.gid, from, .108 ).x, 144 );
});

test("returning to an untimed idle replaces the earlier dash bridge", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	for ( const fromX of [ 100, 150 ] ) {
		const now = fromX === 100 ? 100 : 200;
		const entity = { ...ENTITY, x: fromX };
		const from = { ...START, x: fromX };
		p.samples( new Map() );
		p.pose( ENTITY.gid, from, now / 1000 );
		p.pose( ENTITY.gid, from, (now + 4) / 1000 );
		const motion = createEntityMotion( undefined, ( _from, to ) => to );
		const accepted = motion.displace( entity, {
			destination: { ...from, x: fromX + 200 },
			kind: 8,
			token: 1,
			gid: ENTITY.gid
		}, now );
		const shown = publish( p, accepted, now + 8 );
		assert.ok( Math.abs( shown.x - fromX - 2 ) < 1e-8, "bridge starts from the latest idle display" );
	}
});

test("a replacement displacement keeps its own start across a coalesced publication", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to ), p = createPosePresentation();
	p.origin( 0 );
	const first = motion.displace( ENTITY, {
		destination: { ...START, x: 300 },
		kind: 8,
		token: 1,
		gid: ENTITY.gid
	}, 0 );
	publish( p, first, 0 );
	const walking = motion.step( 96 )[0];
	publish( p, walking, 100 );
	publish( p, walking, 104 );
	motion.displace( ENTITY, {
		destination: { ...START, x: 154, z: 300 },
		kind: 8,
		token: 2,
		gid: ENTITY.gid
	}, 108 );
	const second = motion.step( 124 )[0];
	const shown = publish( p, second, 128 );
	assert.equal( second.movementPath?.startedAtMs, 108 );
	assert.ok( Math.abs( shown.x - 154 ) < 1e-8 );
	assert.ok( Math.abs( shown.z - 110 ) < 1e-8, "the old dash cannot advance through the replacement's start" );
});

test("an idle dash carries publication lag without repaying it above skill speed", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to ), p = createPosePresentation();
	p.origin( 0 );
	p.pose( ENTITY.gid, START, .104 );
	const accepted = motion.displace( ENTITY, {
		destination: { ...START, x: 200 },
		kind: 8,
		token: 1,
		gid: ENTITY.gid
	}, 80 );
	let previous = publish( p, accepted, 108 ), previousAt = 108;
	const intervals = [ 4.3, 8.4, 25.1, 4.2 ];
	for ( let frame = 0; previousAt < 350; frame++ ) {
		const at = previousAt + intervals[frame % intervals.length];
		const update = motion.step( at )[0] ?? { ...accepted, x: 200, moving: false, poseAtMs: at };
		const shown = publish( p, update, at );
		assert.ok( shown.x >= previous.x - 1e-8 );
		assert.ok(
			shown.x - previous.x <= (at - previousAt) * DASH_SPEED / 1000 + 1e-8,
			`frame ${frame} spends only its native speed budget`
		);
		previous = shown;
		previousAt = at;
	}
	assert.ok( Math.abs( previous.x - 200 ) < 1e-8, "arrival consumes lag at skill speed, without a spring tail" );
});
