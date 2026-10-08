/*
===========================================================================

stall-recovery.test.mjs - visible continuity after scheduling and receipt stalls

Exercise the shipped presentation owner on deterministic render clocks.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const { createFrameWork } = await import( "../../src/engine/runtime/frame-work.ts" );
const START = { regionId: 24744, x: 100, y: 10, z: 500, angle: 0 };
const FRAME = 1 / 60;

test("the worker publishes a navigable cast stop, rejects an unsafe corridor and retains relocation generations", () => {
	const movement = createMovement( () => {} ), nav = product();
	nav.objects = [];
	const start = { ...START, regionId: 257, y: 0 };
	movement.seed( start );
	movement.navigation( 257, nav );
	const generation = movement.state().movementTransition.relocation;
	movement.request( { ...start, x: 500 }, 0 );
	assert.equal( movement.state().movementTransition.pathEligible, true );
	movement.step( 100 );
	movement.holdForCast( 100 );
	const held = movement.state().pose;
	assert.ok( held );
	movement.correct( { ...held, x: held.x - 1.6 }, 132 );
	const corrected = movement.state();
	assert.equal( corrected.movementTransition.reason, "correction" );
	assert.equal( corrected.movementTransition.relocation, generation );
	assert.equal( corrected.movementTransition.eligible, true );
	assert.ok( corrected.movementTransition.corridor );
	assert.equal( corrected.pendingMoves, 1, "correction does not drop an ordered acknowledgement" );
	movement.correct( { ...start, regionId: 0x8001 }, 150 );
	assert.equal( movement.state().movementTransition.eligible, false );
	assert.equal( movement.state().movementTransition.pathEligible, false );
	assert.ok( movement.state().movementTransition.relocation > generation );
	movement.request( { ...start, regionId: 0x8001, x: 300 }, 160 );
	assert.ok(
		movement.state().movementTransition.relocation > generation,
		"input coalesced after an unsafe correction must retain the reset"
	);
	movement.clear();
	movement.seed( start );
	movement.request( { ...start, x: 300 }, 200 );
	assert.ok(
		movement.state().movementTransition.relocation > generation,
		"a following input cannot erase relocation"
	);
});

test("untimed poses also preserve their last displayed translation across a stall", () => {
	const p = createPosePresentation();
	p.pose( 1, START, 0 );
	const before = p.pose( 1, { ...START, x: 98.4 }, .016 );
	assert.equal( p.pose( 1, { ...START, x: 98.4 }, 1.016 ).x, before.x );
});

/*
================
sample
================
*/
/** @param {{ relocation: number, reason: string, eligible: boolean, corridor?: { from: typeof START, to: typeof START } }} [transition] */
function sample( p, revision, atMs, transition = {
	relocation: 1,
	reason: "correction",
	eligible: true,
	corridor: { from: { ...START, x: 95 }, to: { ...START, x: 105 } }
} ) {
	p.samples( new Map( [ [ 1, { atMs, revision, moving: false, transition } ] ] ) );
}

for ( const size of [ 1.1, 1.6 ] ) {
	for ( const gap of [ 50, 100, 150, 300, 1000 ] ) {
		test(`${size} unit correction after ${gap} ms preserves its first displayed pose and settles monotonically`, () => {
			const p = createPosePresentation();
			p.origin( 0 );
			sample( p, 1, 0 );
			p.pose( 1, START, 0 );
			const target = { ...START, x: START.x - size };
			sample( p, 2, gap );
			let now = gap / 1000, previous = p.pose( 1, target, now ).x;
			assert.equal( previous, START.x );
			for ( let i = 0; i < 21; i++ ) {
				now += FRAME;
				const drawn = p.pose( 1, target, now );
				assert.ok( drawn.x <= previous + 1e-9 && drawn.x >= target.x - 1e-9 );
				assert.deepEqual( p.pose( 1, target, now ), drawn, "body and camera queries do not advance twice" );
				previous = drawn.x;
			}
			assert.ok( Math.abs( previous - target.x ) < .01 );
		});
	}
}

test("opposite receipts preserve the old trajectory at receipt time, then approach their new endpoint", () => {
	const p = createPosePresentation(), control = createPosePresentation();
	for ( const owner of [ p, control ] ) {
		owner.origin( 0 );
		sample( owner, 1, 0 );
		owner.pose( 1, START, 0 );
	}
	let oldTarget = START, now = 0;
	for ( let revision = 2; revision < 15; revision++ ) {
		const target = { ...START, x: START.x + (revision % 2 ? 1.6 : -1.1) };
		now += FRAME;
		const priorTrajectory = control.pose( 1, oldTarget, now );
		sample( p, revision, now * 1000 );
		const retargeted = p.pose( 1, target, now );
		assert.deepEqual( retargeted, priorTrajectory, "new receipt cannot rewrite the old elapsed interval" );
		sample( control, revision, now * 1000 );
		assert.deepEqual( control.pose( 1, target, now ), retargeted );
		const start = retargeted.x;
		for ( let frame = 0; frame < 5; frame++ ) {
			now += FRAME;
			const drawn = p.pose( 1, target, now );
			assert.deepEqual( control.pose( 1, target, now ), drawn );
			assert.ok( drawn.x >= Math.min( start, target.x ) && drawn.x <= Math.max( start, target.x ) );
		}
		oldTarget = target;
	}
});

test("a new 3D corridor withholds inadmissible old carry instead of snapping to its endpoint", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 1, 0 );
	p.pose( 1, START, 0 );
	sample( p, 2, 16 );
	const oldTarget = { ...START, x: START.x + 1.6 };
	p.pose( 1, oldTarget, .016 );
	const before = p.pose( 1, oldTarget, .032 );
	const target = { ...before, x: before.x + 10, y: before.y + 5, z: before.z + 10 };
	sample( p, 3, 65, {
		relocation: 1,
		reason: "correction",
		eligible: true,
		corridor: { from: before, to: target }
	} );
	assert.deepEqual( p.pose( 1, target, .065 ), before );
	let previous = before;
	for ( let frame = 1; frame <= 60; frame++ ) {
		const shown = p.pose( 1, target, .065 + frame * FRAME );
		if ( frame === 1 ) assert.ok( shown.x > before.x && shown.x < (before.x + target.x) / 2 );
		assert.ok( shown.x >= previous.x && shown.x <= target.x );
		assert.ok( Math.abs( (shown.x - before.x) - (shown.z - before.z) ) < 1e-9 );
		assert.ok( Math.abs( (shown.x - before.x) / 2 - (shown.y - before.y) ) < 1e-9 );
		previous = shown;
	}
	assert.deepEqual( previous, target );
});

for ( const elapsed of [ 0, .00001 ] ) {
	test(`a corridor adopted after ${elapsed} seconds constrains velocity before the next recovery frame`, () => {
		const p = createPosePresentation();
		p.origin( 0 );
		sample( p, 1, 0 );
		p.pose( 1, START, 0 );
		sample( p, 2, 16 );
		const oldTarget = { ...START, x: START.x + 1.6 };
		p.pose( 1, oldTarget, .016 );
		const before = p.pose( 1, oldTarget, .032 );
		const target = { ...before, x: before.x + 10, y: before.y + 5, z: before.z + 10 };
		const now = .032 + elapsed;
		sample( p, 3, now * 1000, {
			relocation: 1,
			reason: "correction",
			eligible: true,
			corridor: { from: before, to: target }
		} );
		const received = p.pose( 1, target, now );
		if ( elapsed === 0 ) assert.deepEqual( received, before );
		else assert.ok( Math.abs( received.x - before.x ) < .01 );
		let previous = received;
		for ( let frame = 1; frame <= 60; frame++ ) {
			const shown = p.pose( 1, target, now + frame * FRAME );
			if ( frame === 1 ) assert.ok( shown.x > before.x && shown.x < (before.x + target.x) / 2 );
			assert.ok( shown.x >= previous.x && shown.x <= target.x );
			assert.ok( Math.abs( (shown.x - before.x) - (shown.z - before.z) ) < .01 );
			assert.ok( Math.abs( (shown.x - before.x) / 2 - (shown.y - before.y) ) < .01 );
			previous = shown;
		}
		assert.deepEqual( previous, target );
	});
}

test("a stall during recovery spends at most 33 ms and retains the unfinished glide", () => {
	const make = () => {
		const p = createPosePresentation();
		p.origin( 0 );
		sample( p, 1, 0 );
		p.pose( 1, START, 0 );
		sample( p, 2, 16 );
		p.pose( 1, { ...START, x: 98.4 }, .016 );
		return p;
	};
	const normal = make(), stalled = make(), target = { ...START, x: 98.4 };
	const resumed = stalled.pose( 1, target, 1.016 ).x;
	assert.ok( resumed >= normal.pose( 1, target, .049 ).x && resumed <= START.x );
	assert.ok( stalled.pose( 1, target, 1.032 ).x < resumed );
});

test("generation changes and unsafe corrections reset even at tiny distances", () => {
	for (
		const transition of [
			{ relocation: 2, reason: "spawn", eligible: false },
			{ relocation: 1, reason: "correction", eligible: false },
			{ relocation: 2, reason: "death", eligible: false },
			{ relocation: 2, reason: "displacement", eligible: false }
		]
	) {
		const p = createPosePresentation();
		p.origin( 0 );
		sample( p, 1, 0 );
		p.pose( 1, START, 0 );
		const target = { ...START, x: 99.9 };
		sample( p, 2, 300, transition );
		assert.equal( p.pose( 1, target, .3 ).x, target.x );
	}
});

test("a correction cannot leave its admitted slope or cross an unadmitted chord", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 1, 0 );
	p.pose( 1, START, 0 );
	const target = { ...START, x: 98.4, y: 9.2 };
	sample( p, 2, 300, { relocation: 1, reason: "correction", eligible: true, corridor: { from: START, to: target } } );
	for ( let now = .3; now < .7; now += FRAME ) {
		const drawn = p.pose( 1, target, now );
		assert.ok( Math.abs( (drawn.x - target.x) / 2 - (drawn.y - target.y) ) < 1e-8 );
	}
});

test("CPU detail hysteresis excludes hidden gaps, restores gradually and shares one optional budget", () => {
	const work = createFrameWork();
	let now = 0;
	const frames = ( count, cost, visible = true ) => {
		for ( let i = 0; i < count; i++ ) {
			now += 20;
			work.begin( now, visible );
			work.recordCpu( cost );
		}
	};
	frames( 50, 20 );
	assert.equal( work.level(), 0 );
	frames( 1, 20 );
	assert.equal( work.level(), 1 );
	frames( 50, 20 );
	assert.equal( work.level(), 2 );
	work.begin( now + 60000, false );
	work.recordCpu( 1 );
	assert.equal( work.level(), 2 );
	now += 60000;
	frames( 250, 1 );
	assert.equal( work.level(), 2 );
	frames( 1, 1 );
	assert.equal( work.level(), 1 );
	frames( 250, 1 );
	assert.equal( work.level(), 0 );
	work.begin( now + 20, true );
	work.spend( 1.2 );
	work.spend( 1 );
	assert.equal( work.remaining(), 0 );
	work.begin( now + 40, true );
	assert.equal( work.remaining(), 2 );
});

/*
================
Terrain recovery

The worker samples a hill between a walk's distant endpoints. A receipt
which kept the same path must not reinterpret its height as an unsafe warp.
================
*/
test("a retained walking path preserves its displayed terrain pose across a stalled receipt", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	const from = { ...START, x: 50, y: 0 }, to = { ...START, x: 200, y: 0 };
	p.samples(
		new Map( [ [ 1, {
			atMs: 0,
			revision: 1,
			moving: true,
			from,
			to,
			transition: { relocation: 1, reason: "input", eligible: true }
		} ] ] )
	);
	p.pose( 1, START, 0 );
	const target = { ...START, x: 110, y: 12 };
	p.samples(
		new Map( [ [ 1, {
			atMs: 300,
			revision: 2,
			moving: true,
			from,
			to,
			transition: { relocation: 1, reason: "receipt", eligible: true, corridor: { from: target, to: target } }
		} ] ] )
	);
	assert.deepEqual( p.pose( 1, target, .3 ), START );
	for ( let i = 1; i <= 21; i++ ) {
		const shown = p.pose( 1, target, .3 + i * FRAME );
		assert.ok( shown.x >= START.x && shown.x <= target.x );
		assert.ok( shown.y >= START.y && shown.y <= target.y );
	}
	assert.ok( Math.abs( p.pose( 1, target, .7 ).x - target.x ) < .01 );
});

test("a receipt may rebase its path ahead of an unfinished visible recovery", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	const from = { ...START, x: 200, y: 0 }, to = { ...START, x: 50, y: 0 };
	const publish = ( revision, atMs, path, transition ) =>
		p.samples( new Map( [ [ 1, { atMs, revision, moving: true, ...path, transition } ] ] ) );
	publish( 1, 0, { from, to }, { relocation: 1, reason: "input", eligible: true } );
	p.pose( 1, START, 0 );
	const first = { ...START, x: 95 };
	publish( 2, 300, { from, to }, {
		relocation: 1,
		reason: "receipt",
		eligible: true,
		corridor: { from: first, to: first }
	} );
	assert.equal( p.pose( 1, first, .3 ).x, 100 );
	const next = { ...START, x: 90 };
	publish( 3, 316, { from: next, to }, {
		relocation: 1,
		reason: "receipt",
		eligible: true,
		corridor: { from: next, to: next }
	} );
	const shown = p.pose( 1, next, .316 ).x;
	assert.ok( shown > first.x && shown < 100, "retargeting continues the glide along its retained path" );
});

test("a receipt every displayed frame cannot starve an existing correction trajectory", () => {
	const p = createPosePresentation(), target = { ...START, x: 98.4 };
	p.origin( 0 );
	sample( p, 1, 0 );
	p.pose( 1, START, 0 );
	sample( p, 2, 16 );
	assert.equal( p.pose( 1, target, .016 ).x, START.x );
	let previous = START.x;
	for ( let i = 1; i <= 21; i++ ) {
		const now = .016 + i * FRAME;
		sample( p, i + 2, now * 1000 );
		const shown = p.pose( 1, target, now ).x;
		assert.ok( shown <= previous && shown >= target.x );
		previous = shown;
	}
	assert.ok( Math.abs( previous - target.x ) < .01 );
});

test("fresh input cannot carry residual correction across an unadmitted corner", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 1, 0 );
	p.pose( 1, START, 0 );
	const from = { ...START, x: 98.4 };
	sample( p, 2, 16 );
	p.pose( 1, from, .016 );
	const target = { ...from, z: START.z + .8 };
	p.samples(
		new Map( [ [ 1, {
			atMs: 32,
			revision: 3,
			moving: true,
			from,
			to: { ...from, z: 600 },
			transition: { relocation: 1, reason: "input", eligible: true }
		} ] ] )
	);
	const shown = p.pose( 1, target, .032 );
	assert.equal( shown.x, target.x );
	assert.equal( shown.z, target.z, "unsafe diagonal resets instead of cutting the corner" );
});
