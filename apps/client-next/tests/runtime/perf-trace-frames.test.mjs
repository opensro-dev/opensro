/*
===========================================================================

perf-trace-frames.test.mjs - per-frame cost of matched samples in a trace

frameCosts (tools/perf/core/trace.mjs) is what the analyzer's --frames
report reduces: which animation frames ran a function and what each of
those frames paid. A mean over all frames cannot tell work done every
frame from a large job done on some.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { frameCosts } from "../../tools/perf/core/trace.mjs";

/*
================
profileOf

A sampled profile: node 1 is the root, node 2 the frame function under
it, node 3 the function under test below node 2. Each sample is
[ time µs, leaf node ].
================
*/
function profileOf( samples ) {
	const nodes = new Map( [
		[ 1, { id: 1, callFrame: { functionName: "(root)" } } ],
		[ 2, { id: 2, parent: 1, callFrame: { functionName: "frame" } } ],
		[ 3, { id: 3, parent: 2, callFrame: { functionName: "chatLayout" } } ]
	] );
	return { nodes, samples: samples.map( row => row[1] ), times: samples.map( row => row[0] ) };
}

const MATCH = stack => stack.includes( 3 );

test("a frame is charged the matched samples inside it, and only frames inside the window are listed", () => {
	// Frames start at 1000, 2000, 3000 and 4000; the window is [1000, 4000).
	const profile = profileOf( [
		[ 1100, 3 ],
		[ 1400, 2 ],
		[ 2100, 2 ],
		[ 2900, 2 ],
		[ 3100, 3 ],
		[ 3300, 3 ],
		[ 3600, 2 ],
		[ 4100, 3 ],
		[ 4200, 2 ]
	] );
	const costs = frameCosts( profile, [ 1000, 2000, 3000, 4000 ], 1000, 4000, 10000, MATCH );
	assert.deepEqual( [ ...costs ], [ 300, 0, 500 ], "frame 1 ran it for 300 µs, frame 2 not at all, frame 3 for 500" );
});

test("a sample before the first frame is not charged to it, and a sampler gap is capped", () => {
	const profile = profileOf( [ [ 500, 3 ], [ 1100, 3 ], [ 9000, 2 ], [ 9100, 2 ] ] );
	// The sample at 500 lies before the first frame; the one at 1100 owns
	// 7900 µs until the next sample and is credited the 2000 µs cap.
	const costs = frameCosts( profile, [ 1000, 5000 ], 0, 10000, 2000, MATCH );
	assert.deepEqual( [ ...costs ], [ 2000, 0 ] );
});

test("no frames in the window is an empty result", () => {
	const profile = profileOf( [ [ 100, 3 ], [ 200, 2 ] ] );
	assert.equal( frameCosts( profile, [ 5000 ], 0, 1000, 2000, MATCH ).length, 0 );
});
