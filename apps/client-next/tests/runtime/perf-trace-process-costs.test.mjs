/*
===========================================================================

perf-trace-process-costs.test.mjs - GPU-process time per frame in a trace

processCosts (tools/perf/core/trace.mjs) is what fps-bench --gpu-process
reports: wall and CPU time per frame of each GPU-process thread and of
the renderer main thread inside a measured window. Processes and threads
are found by their metadata names, nested slices count once, and a task
that straddles the window edge counts only its part inside.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { processCosts } from "../../tools/perf/core/trace.mjs";

const GPU = 7, RENDERER = 9, BROWSER = 3;

/*
================
metadata
================
*/
function metadata() {
	return [
		{ ph: "M", name: "process_name", pid: GPU, tid: 0, args: { name: "GPU Process" } },
		{ ph: "M", name: "process_name", pid: RENDERER, tid: 0, args: { name: "Renderer" } },
		{ ph: "M", name: "process_name", pid: BROWSER, tid: 0, args: { name: "Browser" } },
		{ ph: "M", name: "thread_name", pid: GPU, tid: 1, args: { name: "CrGpuMain" } },
		{ ph: "M", name: "thread_name", pid: GPU, tid: 2, args: { name: "VizCompositorThread" } },
		{ ph: "M", name: "thread_name", pid: RENDERER, tid: 1, args: { name: "CrRendererMain" } },
		{ ph: "M", name: "thread_name", pid: RENDERER, tid: 5, args: { name: "Compositor" } },
		// The browser's main thread shares the GPU main thread's tid and a name prefix.
		{ ph: "M", name: "thread_name", pid: BROWSER, tid: 1, args: { name: "CrBrowserMain" } }
	];
}

/*
================
task
================
*/
function task( pid, tid, ts, dur, tdur ) {
	return { ph: "X", name: "RunTask", cat: "toplevel", pid, tid, ts, dur, ...(tdur === undefined ? {} : { tdur }) };
}

const close = ( actual, expected ) => assert.ok( Math.abs( actual - expected ) < 1e-9, `${actual} != ${expected}` );

test("nested slices count once, a straddling task counts its inside part, threads are found by name", () => {
	// Window [1000, 5000) µs, 4 frames.
	const events = [
		...metadata(),
		// CrGpuMain: a task straddling the window start (500..1500, 800 µs CPU)
		// whose child lies inside the window; then one with a nested child.
		task( GPU, 1, 500, 1000, 800 ),
		task( GPU, 1, 1100, 200, 150 ),
		task( GPU, 1, 2000, 1000, 600 ),
		task( GPU, 1, 2200, 300, 250 ),
		// A thread whose events carry no thread duration.
		task( GPU, 2, 3000, 400 ),
		task( RENDERER, 1, 1000, 2000, 1800 ),
		// Not the renderer main thread, and not the GPU process.
		task( RENDERER, 5, 1000, 3000, 3000 ),
		task( BROWSER, 1, 1000, 3000, 3000 )
	];
	const costs = processCosts( events, { start: 1000, end: 5000 }, 4 );
	close( costs.spanMs, 4 );
	assert.equal( costs.gpuMain.name, "CrGpuMain" );
	// Wall: 500 inside the straddling task + 1000 = 1500 µs; CPU: 800 / 2 + 600.
	close( costs.gpuMain.wallMsPerFrame, 1.5 / 4 );
	close( costs.gpuMain.cpuMsPerFrame, 1.0 / 4 );
	close( costs.gpuMain.busy, 1500 / 4000 );
	const viz = costs.gpu.find( row => row.name === "VizCompositorThread" );
	assert.equal( viz.cpuMsPerFrame, null, "no tdur means unknown CPU, not zero" );
	close( viz.wallMsPerFrame, 0.4 / 4 );
	assert.deepEqual( costs.gpu.map( row => row.name ), [ "CrGpuMain", "VizCompositorThread" ], "busiest first" );
	assert.equal( costs.renderer.name, "CrRendererMain" );
	close( costs.renderer.cpuMsPerFrame, 1.8 / 4 );
});

test("a slice that outlasts its enclosing task adds only its overhang", () => {
	const events = [ ...metadata(), task( GPU, 1, 0, 1000, 900 ), task( GPU, 1, 800, 400, 400 ) ];
	const costs = processCosts( events, { start: 0, end: 2000 }, 1 );
	close( costs.gpuMain.wallMsPerFrame, 1.2 );
	close( costs.gpuMain.cpuMsPerFrame, 0.9 );
});
