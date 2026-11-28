/*
===========================================================================

gpu-time.mjs - the GPU's own time per frame in a bench window

The renderer times its passes with timestamp queries when the page is
opened with gpu-timing=1 (bootstrap.ts diagnosticsFromQuery, device.ts
createGpuTiming): each completed sample is one frame's passes and their
GPU ms. fps-bench --gpu-time reads the samples a window completed and
summarizes them here, so a reading names the GPU's cost beside the CPU's:
a frame the main thread finishes in 2 ms is still a 4 ms frame when the
GPU needs 4.

The timestamps are written inside the measured frames, so a --gpu-time run
compares only with another --gpu-time run.

===========================================================================
*/

/*
================
gpuTimingCursor

The last completed sample's sequence on the page, so a window reads only
what it completed.
================
*/
export async function gpuTimingCursor( page ) {
	const stats = await page.evaluate( () => globalThis.__benchRuntime.gpuTiming() );
	if ( !stats.enabled || !stats.supported ) {
		throw Error( "gpu-time: the renderer has no GPU timing (timestamp-query missing, or gpu-timing=1 not set)" );
	}
	return stats.samples.reduce( ( last, sample ) => Math.max( last, sample.sequence ), -1 );
}

/*
================
gpuTimingSince

The samples completed after cursor, and the stats' failure counts.
================
*/
export async function gpuTimingSince( page, cursor ) {
	const stats = await page.evaluate( () => globalThis.__benchRuntime.gpuTiming() );
	return {
		samples: stats.samples.filter( sample => sample.sequence > cursor ),
		failed: stats.failed,
		skipped: stats.skipped,
		omittedPasses: stats.omittedPasses
	};
}

/*
================
percentile
================
*/
function percentile( sorted, p ) {
	return sorted.length ? sorted[Math.min( sorted.length - 1, Math.floor( p * sorted.length ) )] : 0;
}

/*
================
summarizeGpuTiming

Per frame GPU ms (the sum of the frame's timed passes): mean, p50 and p99,
and each pass's mean, costliest first. Null when no sample completed.
================
*/
export function summarizeGpuTiming( samples ) {
	if ( !samples.length ) return null;
	const totals = [], passes = new Map();
	for ( const sample of samples ) {
		let total = 0;
		for ( const pass of sample.passes ) {
			total += pass.ms;
			passes.set( pass.name, (passes.get( pass.name ) ?? 0) + pass.ms );
		}
		totals.push( total );
	}
	const sorted = [ ...totals ].sort( ( a, b ) => a - b );
	return {
		frames: samples.length,
		meanMs: totals.reduce( ( a, b ) => a + b, 0 ) / totals.length,
		p50Ms: percentile( sorted, .5 ),
		p99Ms: percentile( sorted, .99 ),
		passes: [ ...passes ]
			.map( ( [name, ms] ) => ({ name, meanMs: ms / samples.length }) )
			.sort( ( a, b ) => b.meanMs - a.meanMs )
	};
}

/*
================
gpuTimeRow

The row's GPU line.
================
*/
export function gpuTimeRow( gpu ) {
	if ( !gpu ) return "";
	const passes = gpu.passes.slice( 0, 6 ).map( pass => `${pass.name} ${pass.meanMs.toFixed( 2 )}` ).join( ", " );
	return `\n${"".padEnd( 13 )} gpu time ${gpu.meanMs.toFixed( 2 )} ms/f (p50 ${gpu.p50Ms.toFixed( 2 )} p99 ${
		gpu.p99Ms.toFixed( 2 )
	}) over ${gpu.frames} timed frames  | ${passes}`;
}
