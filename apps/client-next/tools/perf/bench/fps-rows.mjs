/*
===========================================================================

fps-rows.mjs - the printed rows of fps-bench.mjs

fps-bench.mjs measures; this module only formats one result into the
lines it prints (frame times, counts, GPU process threads, page threads,
GPU time and the poses the GPU path did not take).

===========================================================================
*/
import { gpuTimeRow } from "../core/gpu-time.mjs";

/*
================
fpsRow

One measured row: frame times, counts, then the GPU process, GPU time and
pose eligibility lines beneath it.
================
*/
export function fpsRow( result ) {
	const counts = Object.entries( result.counts ).map( ( [k, v] ) => `${k} ${v}` ).join( ", " );
	const allocated = result.allocatedMBs === null ?
		"" :
		`  alloc ${result.allocatedMBs.toFixed( 1 )} MB/s ${
			(result.allocatedMBs * 1024 / result.fps).toFixed( 0 )
		} KB/f`;
	return `${result.name.padEnd( 13 )} ${result.fps.toFixed( 0 ).padStart( 5 )} fps  frame p50 ${
		result.p50.toFixed( 2 )
	} p99 ${result.p99.toFixed( 2 )} max ${result.max.toFixed( 1 )} ms  main ${result.main.toFixed( 2 )} world ${
		result.world.toFixed( 2 )
	} ms/f${allocated}${counts ? "  | " + counts : ""}${gpuRow( result.gpuProcess )}${gpuTimeRow( result.gpuTime )}${
		poseRow( result.poseEligibility )
	}`;
}

/*
================
poseRow

The poses the GPU path did not take, by model and reason (--counts).
================
*/
function poseRow( eligibility ) {
	if ( !eligibility ) return "";
	const reasons = Object.entries( eligibility.cpuSampleReasons ?? {} )
		.map( ( [reason, n] ) => `${reason} ${n}` )
		.join( ", " );
	const sampled = `\n${"".padEnd( 13 )} poses ${eligibility.unique}: gpu ${eligibility.gpuSamples}, cpu ${
		reasons || "none"
	}`;
	if ( !eligibility.cpuModels?.length ) return sampled;
	const models = eligibility.cpuModels.slice( 0, 6 )
		.map( row => `${row.model.split( "/" ).pop()} x${row.actors} (${row.reason})` )
		.join( ", " );
	return `${sampled}\n${"".padEnd( 13 )} cpu-only models: ${models}`;
}

/*
================
gpuRow
================
*/
function gpuRow( gpu ) {
	if ( !gpu ) return "";
	const ms = value => value === null || value === undefined ? "?" : value.toFixed( 2 );
	const threads = gpu.threads.map( t => `${t.name} ${(t.busy * 100).toFixed( 0 )}%` ).join( ", " );
	return `\n${"".padEnd( 13 )} gpu process ${gpu.main?.name ?? "(no main thread)"} cpu ${
		ms( gpu.main?.cpuMsPerFrame )
	} wall ${ms( gpu.main?.wallMsPerFrame )} ms/f over ${gpu.frames} traced frames (${
		gpu.fps.toFixed( 0 )
	} fps)  | ${threads}${pageRow( gpu.pageThreads )}`;
}

/*
================
pageRow

The page's own threads (main, workers, compositor) past the busy share,
so a worker's cost is read beside the main thread's.
================
*/
function pageRow( threads ) {
	if ( !threads?.length ) return "";
	const ms = value => value === null || value === undefined ? "?" : value.toFixed( 2 );
	return `\n${"".padEnd( 13 )} page threads cpu ms/f: ${
		threads.map( t => `${t.name} ${ms( t.cpuMsPerFrame )}` ).join( ", " )
	}`;
}
