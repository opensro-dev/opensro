/*
===========================================================================

perf-gpu-time.test.mjs - tests for tools/perf/core/gpu-time.mjs

A frame's GPU time is the sum of its timed passes; the summary's
percentiles are over frames and its pass table is costliest first.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { summarizeGpuTiming } from "../../tools/perf/core/gpu-time.mjs";

test("frame totals are pass sums; passes come costliest first", () => {
	const samples = [ 1, 2, 3, 4 ].map( ( n, sequence ) => ({
		sequence,
		passes: [ { name: "main-pass", ms: n }, { name: "deferred-particles", ms: .5 } ]
	}) );
	const gpu = summarizeGpuTiming( samples );
	if ( !gpu ) throw Error( "no summary" );
	assert.equal( gpu.frames, 4 );
	assert.equal( gpu.meanMs, 3 );
	assert.equal( gpu.p50Ms, 3.5 );
	assert.equal( gpu.p99Ms, 4.5 );
	assert.deepEqual( gpu.passes.map( pass => pass.name ), [ "main-pass", "deferred-particles" ] );
	assert.equal( gpu.passes[0].meanMs, 2.5 );
});

test("no completed sample is no reading, not zero", () => {
	assert.equal( summarizeGpuTiming( [] ), null );
});
