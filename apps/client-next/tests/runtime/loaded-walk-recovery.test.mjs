/*
===========================================================================

loaded-walk-recovery.test.mjs - late samples cannot turn walking into a dash

Rendering continues at fifty frames per second while the worker is stalled.
Recovery must retain the admitted path and bound its temporary catch-up speed.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );

/*
================
pose
================
*/
function pose( x ) {
	return { regionId: 24744, x, y: 10, z: 500, angle: 0 };
}

test("vertical-only accepted recovery still settles", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	const from = pose( 100 ), to = { ...from, y: 20 };
	presentation.samples( new Map( [ [ 1, { atMs: 0, revision: 1, moving: false } ] ] ) );
	presentation.pose( 1, from, 0 );
	presentation.samples(
		new Map( [ [ 1, {
			atMs: 1000,
			revision: 2,
			moving: false,
			walkingPath: [ from, to ],
			transition: { relocation: 0, reason: "receipt", eligible: true }
		} ] ] )
	);
	let shown = presentation.pose( 1, to, 1 );
	assert.deepEqual( shown, from );
	for ( let frame = 1; frame <= 100; frame++ ) {
		shown = presentation.pose( 1, to, 1 + frame * .02 );
		assert.equal( shown.x, from.x );
		assert.equal( shown.z, from.z );
	}
	assert.ok( Math.abs( shown.y - to.y ) < .01 );
});

/*
================
Late walking samples respect the displayed movement budget
================
*/
test("a worker stall at fifty rendered FPS has bounded recovery speed", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	const from = pose( 100 ), to = pose( 200 ), walkingPath = [ from, to ];
	let previous = from;
	for ( let frame = 0; frame <= 160; frame++ ) {
		const now = frame * 20;
		const atMs = now > 200 && now < 1200 ? 200 : Math.min( now, 2000 );
		const target = pose( 100 + atMs * .05 );
		presentation.samples(
			new Map( [ [ 1, {
				atMs,
				revision: 1,
				moving: atMs < 2000,
				from,
				to,
				durationMs: 2000,
				walkingPath
			} ] ] )
		);
		const drawn = presentation.pose( 1, target, now / 1000 );
		assert.ok( drawn.x >= previous.x - 1e-8, "recovery must not reverse a straight walk" );
		assert.ok( drawn.x - previous.x <= 1.5 + 1e-8, `frame ${frame}: ${drawn.x - previous.x} units in 20 ms` );
		assert.deepEqual( presentation.pose( 1, target, now / 1000 ), drawn, "camera and body share one step" );
		previous = drawn;
	}
	assert.ok( Math.abs( previous.x - to.x ) < .01, "recovery eventually reaches the authoritative endpoint" );
});

for ( const terrain of [ "flat", "hill" ] ) {
	for ( const blocked of [ false, true ] ) {
		test(`late ${terrain} travel stays bounded with blocked lookahead ${blocked}`, () => {
			const presentation = createPosePresentation();
			presentation.origin( 0 );
			const from = pose( 100 ), corner = pose( 130 );
			const to = { ...pose( 160 ), y: terrain === "hill" ? 70 : 10 };
			const walkingPath = [ from, corner, to ];
			presentation.samples(
				new Map( [ [ 1, {
					atMs: 0,
					revision: 1,
					moving: true,
					from,
					to: corner,
					durationMs: 600,
					walkingPath
				} ] ] )
			);
			presentation.pose( 1, from, 0 );
			presentation.samples(
				new Map( [ [ 1, {
					atMs: 1200,
					revision: 2,
					moving: false,
					from: blocked ? to : undefined,
					to: blocked ? to : undefined,
					durationMs: blocked ? 100 : undefined,
					walkingPath,
					transition: { relocation: 0, reason: "receipt", eligible: true }
				} ] ] )
			);
			let previous = presentation.pose( 1, to, 1.2 );
			assert.deepEqual( previous, from, "the delayed arrival preserves the last shown pose" );
			for ( let frame = 1; frame <= 120; frame++ ) {
				const drawn = presentation.pose( 1, to, 1.2 + frame * .02 );
				assert.ok( drawn.x >= previous.x && drawn.x - previous.x <= 1.5 + 1e-8 );
				const height = drawn.x <= corner.x ? 10 : 10 + (drawn.x - corner.x) * (to.y - 10) / 30;
				assert.ok( Math.abs( drawn.y - height ) < 1e-7, "recovery follows the sampled hill" );
				previous = drawn;
			}
			assert.ok( Math.abs( previous.x - to.x ) < .01 );
		});
	}
}

for ( const speed of [ 20, 50, 150 ] ) {
	test(`steady fifty FPS keeps native ${speed} speed without introducing correction`, () => {
		const presentation = createPosePresentation();
		presentation.origin( 0 );
		const from = pose( 100 ), to = pose( 100 + speed * 2 ), walkingPath = [ from, to ];
		for ( let frame = 0; frame <= 100; frame++ ) {
			const atMs = frame * 20, target = pose( 100 + speed * atMs / 1000 );
			presentation.samples(
				new Map( [ [ 1, {
					atMs,
					revision: 1,
					moving: atMs < 2000,
					from,
					to,
					durationMs: 2000,
					walkingPath
				} ] ] )
			);
			const shown = presentation.pose( 1, target, atMs / 1000 );
			assert.ok( Math.abs( shown.x - target.x ) < 1e-8 );
			assert.deepEqual( { ...shown, x: target.x }, target );
		}
	});
}
