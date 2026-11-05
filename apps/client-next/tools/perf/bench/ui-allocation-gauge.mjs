/*
===========================================================================

ui-allocation-gauge.mjs - steady-state allocations per UI scene, as a gate

Usage:
  node --expose-gc tools/perf/bench/ui-allocation-gauge.mjs
       [--write] [--baseline FILE]

The #2534 decision record gives every ui.ts move a four-part
certificate; this is the allocation part. A move may not raise any
scene's steady-state allocations unless the PR names the allocation
and says why. The gauge steps each scene of the UI test fixture
(tests/helpers/ui-fixture.mjs, the same build the behaviour tests use)
for FRAMES frames after a warm-up. The measurement starts from the
settled heap navigation-memory-bench established (forced collections,
then process.memoryUsage()), then lets the frames allocate without
collecting: the heap growth over the run is the frames' churn, dead
young objects included, and the median of ROUNDS rounds is the row.
Bytes per frame is the number a move must not raise.

The committed baseline (tools/perf/ui-allocation-baseline.json) holds
one row per scene: { "bytesPerFrame": N } as measured, or
{ "bytesPerFrame": N, "named": "<what grew and why>" } - a row with a
named rise is the PR naming the allocation in the one place the gate
reads. --write refreshes the measured rows; a refreshed row that rose
without a "named" entry fails the same run, so the baseline cannot be
quietly re-based over a regression.

Scenes are the fixture's world scene today; the scene goldens (#2514)
add theirs by extending SCENES with a state the fixture accepts.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../.." );
export const BASELINE_PATH = path.join( clientRoot, "tools/perf/ui-allocation-baseline.json" );
const WARMUP_FRAMES = 30;
const FRAMES = 120;
const ROUNDS = 3;
// Measurement slack: a rise within this share of the baseline (or
// SLACK_BYTES, whichever is larger) is allocator noise, not a move's
// new allocation. Anything past it must be named in the baseline.
export const SLACK_RATIO = 0.10;
export const SLACK_BYTES = 4096;

// Scenes the gauge steps: a name and the fixture-state edit that opens
// it. The world HUD is the fixture's resting state.
export const SCENES = [
	{ name: "world-hud", open: state => state }
];

/*
================
settledHeap

Heap in use after collecting garbage twice (weak references and
finalizers can need the second pass). navigation-memory-bench's
measure, reused.
================
*/
function settledHeap() {
	globalThis.gc();
	globalThis.gc();
	const usage = process.memoryUsage();
	return usage.heapUsed + usage.arrayBuffers;
}

/*
================
compareSceneRows

The gate's whole decision, pure over rows: for each scene the baseline
and the measured row are compared. A scene missing on either side
fails (a move cannot delete a scene from the gauge, nor sneak one
in); a rise past the slack fails unless the baseline row names it.
Returns the failure list; empty is a pass.
================
*/
export function compareSceneRows( baseline, measured ) {
	const failures = [];
	for ( const name of Object.keys( measured ) ) {
		if ( !baseline[name] ) failures.push( `${name}: measured but missing from the baseline` );
	}
	for ( const [name, row] of Object.entries( baseline ) ) {
		const now = measured[name];
		if ( !now ) {
			failures.push( `${name}: in the baseline but not measured` );
			continue;
		}
		const slack = Math.max( row.bytesPerFrame * SLACK_RATIO, SLACK_BYTES );
		if ( now.bytesPerFrame > row.bytesPerFrame + slack && !row.named ) {
			failures.push(
				`${name}: allocations rose from ${row.bytesPerFrame} to ${now.bytesPerFrame} bytes/frame ` +
					`past the slack (${Math.round( slack )}); name the allocation in the baseline or remove it`
			);
		}
	}
	return failures;
}

/*
================
measureScene

Steps one scene FRAMES frames after warm-up and returns the median
round's bytes per frame: settled heap before the round, no collection
during it, heap in use after. The frame clock advances 16 ms per
frame and never repeats, so timers and animations see ordinary
forward time.
================
*/
async function measureScene( scene ) {
	const { uiFixture } = await import( "../../../tests/helpers/ui-fixture.mjs" );
	const fixture = uiFixture();
	try {
		const state = scene.open( fixture.state );
		let now = 1000;
		const step = () => fixture.ui.step( state, now += 16 );
		for ( let i = 0; i < WARMUP_FRAMES; i++ ) step();
		const rounds = [];
		for ( let round = 0; round < ROUNDS; round++ ) {
			const before = settledHeap();
			for ( let i = 0; i < FRAMES; i++ ) step();
			const usage = process.memoryUsage();
			rounds.push( (usage.heapUsed + usage.arrayBuffers - before) / FRAMES );
		}
		rounds.sort( ( a, b ) => a - b );
		return { bytesPerFrame: Math.round( rounds[Math.floor( rounds.length / 2 )] ) };
	} finally {
		fixture.dispose();
	}
}

/*
================
main

Measures every scene, then either writes the baseline (--write) and
still judges the rise in the same run, or judges against the committed
baseline. Exit 1 names each scene that rose unnamed.
================
*/
async function main( argv ) {
	if ( typeof globalThis.gc !== "function" ) {
		throw new Error( "Run with node --expose-gc." );
	}
	const write = argv.includes( "--write" );
	const baselineAt = argv.indexOf( "--baseline" );
	const baselinePath = baselineAt >= 0 ? path.resolve( argv[baselineAt + 1] ) : BASELINE_PATH;
	const measured = {};
	for ( const scene of SCENES ) measured[scene.name] = await measureScene( scene );
	let baseline = {};
	if ( fs.existsSync( baselinePath ) ) baseline = JSON.parse( fs.readFileSync( baselinePath, "utf8" ) );
	let failures;
	if ( write ) {
		// Judge rises against the old rows before replacing them: a
		// refreshed baseline must not quietly absorb a regression. New
		// scenes join here; that is the one write-only difference.
		const shared = Object.fromEntries( Object.entries( baseline ).filter( ( [name] ) => measured[name] ) );
		failures = compareSceneRows( shared, measured );
		const merged = { ...baseline };
		for ( const [name, row] of Object.entries( measured ) ) {
			merged[name] = { ...row, ...(baseline[name]?.named ? { named: baseline[name].named } : {}) };
		}
		fs.writeFileSync( baselinePath, JSON.stringify( merged, null, "\t" ) + "\n" );
		console.log( `wrote ${path.relative( clientRoot, baselinePath )}` );
	} else {
		failures = compareSceneRows( baseline, measured );
	}
	for ( const [name, row] of Object.entries( measured ) ) {
		console.log( `${name}: ${row.bytesPerFrame} bytes/frame` );
	}
	if ( failures.length ) {
		console.error( failures.join( "\n" ) );
		process.exitCode = 1;
		return;
	}
	console.log( "PASS" );
}

if ( process.argv[1] === import.meta.filename ) {
	main( process.argv.slice( 2 ) ).catch( error => {
		console.error( error );
		process.exitCode = 1;
	} );
}
