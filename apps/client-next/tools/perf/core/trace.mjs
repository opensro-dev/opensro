/*
===========================================================================

trace.mjs - threads, frames and sampled profiles of a DevTools trace

Owns reading a Chrome performance trace (the JSON DevTools saves) into
the pieces the analyzer reports on:

- threads: every (process, thread) with its name, a label for workers
  (the script they run) and its busy time;
- frames: the renderer main thread's animation frames;
- profiles: the V8 sampling profile of each profiled thread, rebuilt from
  its ProfileChunk events, with the sampled source position of each sample;
- process costs: wall and CPU time per frame of every GPU-process thread
  and of the renderer main thread inside a measured window (fps-bench
  --gpu-process).

Time attribution follows DevTools: a sample owns the time until the next
one. The sampler is irregular (on Windows its 90th percentile gap was
1.3 ms), so a gap above maxGap microseconds credits only maxGap to the
sample and books the rest as "(sampler gap)"; otherwise a native call that
ends a frame absorbs the idle time after it.

===========================================================================
*/
import { readFileSync } from "node:fs";

/*
================
loadTrace
================
*/
export function loadTrace( file ) {
	const parsed = JSON.parse( readFileSync( file, "utf8" ) );
	const events = Array.isArray( parsed ) ? parsed : parsed.traceEvents;
	if ( !Array.isArray( events ) ) throw new Error( `${file}: not a trace` );
	const threads = new Map();
	let start = Infinity, end = -Infinity;
	for ( const e of events ) {
		if ( e.name === "thread_name" && e.ph === "M" ) {
			threadOf( threads, e.pid, e.tid ).name = e.args?.name ?? "";
			continue;
		}
		if ( e.ph !== "X" || !Number.isFinite( e.dur ) ) continue;
		const thread = threadOf( threads, e.pid, e.tid );
		if ( e.name === "RunTask" ) {
			thread.busy += e.dur;
			start = Math.min( start, e.ts );
			end = Math.max( end, e.ts + e.dur );
		}
		thread.slices.set( e.name, (thread.slices.get( e.name ) ?? 0) + e.dur );
		const url = e.args?.data?.url;
		if ( e.name === "FunctionCall" && url && !thread.script ) thread.script = url;
	}
	for ( const thread of threads.values() ) thread.label = labelOf( thread );
	return { file, events, threads, start, end, profiles: readProfiles( events, threads ) };
}

/*
================
threadOf
================
*/
function threadOf( threads, pid, tid ) {
	const key = `${pid}/${tid}`;
	let thread = threads.get( key );
	if ( !thread ) {
		thread = { key, pid, tid, name: "", label: "", script: "", busy: 0, slices: new Map() };
		threads.set( key, thread );
	}
	return thread;
}

/*
================
labelOf

Workers are named by the script they run; the busiest renderer main is "main".
================
*/
function labelOf( thread ) {
	if ( thread.name === "DedicatedWorker thread" && thread.script ) {
		const pathname = new URL( thread.script, "http://x" ).pathname;
		return "worker " + pathname.split( "/" ).slice( -3 ).join( "/" );
	}
	return thread.name || thread.key;
}

/*
================
readProfiles

Rebuild each profiler independently: --cpu and --trace can record the same
thread with overlapping clocks and colliding node IDs. Prefer the internal
trace sampler, then the longest complete recording of that source. Never
concatenate independent clocks to make one apparently longer profile.
================
*/
export function readProfiles( events, threads ) {
	const owners = new Map();
	for ( const e of events ) {
		if ( e.name === "Profile" ) {
			owners.set( `${e.pid}:${e.id}`, {
				thread: `${e.pid}/${e.tid}`,
				start: e.args.data.startTime,
				source: e.args.data.source ?? "unknown"
			} );
		}
	}
	const recordings = new Map();
	for ( const e of events ) {
		if ( e.name !== "ProfileChunk" ) continue;
		const id = `${e.pid}:${e.id}`, owner = owners.get( id );
		if ( !owner ) continue;
		let profile = recordings.get( id );
		if ( !profile ) {
			profile = {
				id,
				source: owner.source,
				threadKey: owner.thread,
				thread: threads.get( owner.thread ),
				nodes: new Map(),
				samples: [],
				deltas: [],
				lines: [],
				columns: [],
				start: owner.start
			};
			recordings.set( id, profile );
		}
		const data = e.args.data, cpu = data.cpuProfile ?? {};
		for ( const node of cpu.nodes ?? [] ) profile.nodes.set( node.id, node );
		const samples = cpu.samples ?? [];
		for ( let i = 0; i < samples.length; i++ ) {
			profile.samples.push( samples[i] );
			profile.deltas.push( data.timeDeltas?.[i] ?? 0 );
			profile.lines.push( data.lines?.[i] ?? 0 );
			profile.columns.push( data.columns?.[i] ?? 0 );
		}
	}
	const profiles = new Map();
	for ( const profile of recordings.values() ) {
		let t = profile.start;
		profile.times = profile.deltas.map( d => (t += d) );
		// Parent links: chunks give parent on the node, or children on the parent.
		for ( const node of profile.nodes.values() ) {
			for ( const child of node.children ?? [] ) {
				const c = profile.nodes.get( child );
				if ( c && c.parent === undefined ) c.parent = node.id;
			}
		}
		if ( !profile.samples.length ) continue;
		const previous = profiles.get( profile.threadKey );
		const preferred = profile.source === "Internal", wasPreferred = previous?.source === "Internal";
		if (
			!previous || (preferred && !wasPreferred) ||
			(preferred === wasPreferred &&
				profile.times.at( -1 ) - profile.start > previous.times.at( -1 ) - previous.start)
		) profiles.set( profile.threadKey, profile );
	}
	return profiles;
}

/*
================
frameStarts

Animation frame start times (µs) on the renderer main thread.
================
*/
export function frameStarts( trace, main ) {
	return trace.events.filter( e =>
		e.pid === main.pid && e.tid === main.tid && e.name === "AnimationFrame" && e.ph === "b"
	).map( e => e.ts ).sort( ( a, b ) => a - b );
}

/*
================
mainThread

The renderer main thread with the most work.
================
*/
export function mainThread( trace ) {
	return [ ...trace.threads.values() ].filter( t => t.name === "CrRendererMain" ).sort( ( a, b ) =>
		b.busy - a.busy
	)[0];
}

/*
================
forEachSample

Calls visit( index, owned µs, stack of node ids leaf first ) for every
sample whose time falls in [from, to) (µs). Returns the booked gap µs.
================
*/
export function forEachSample( profile, from, to, maxGap, visit ) {
	let gaps = 0;
	const { samples, times, nodes } = profile;
	for ( let i = 0; i + 1 < samples.length; i++ ) {
		const at = times[i];
		if ( at < from || at >= to ) continue;
		let owned = times[i + 1] - at;
		if ( owned > maxGap ) {
			gaps += owned - maxGap;
			owned = maxGap;
		}
		const stack = [];
		// A truncated capture can name nodes its profile chunks never sent.
		for ( let id = samples[i]; id !== undefined && nodes.has( id ); id = nodes.get( id ).parent ) stack.push( id );
		visit( i, owned, stack );
	}
	return gaps;
}

/*
================
frameCosts

Inclusive time (µs) per animation frame of the samples that match: one
entry per frame of starts that begins inside [from, to), a frame being
the span from its start to the next one (the last runs to `to`).
matches( stack ) decides whether a sample belongs. A per-frame mean
hides the two shapes that matter most - work that runs on every frame,
and work that runs on some frames and is large when it does (a layout
rebuilt on half the frames at 7.6 ms each reads 3.7 ms/frame) - so the
caller reports how many frames paid and what a paying frame cost.
================
*/
export function frameCosts( profile, starts, from, to, maxGap, matches ) {
	const frames = starts.filter( t => t >= from && t < to );
	const costs = new Float64Array( frames.length );
	let frame = 0;
	forEachSample( profile, from, to, maxGap, ( i, owned, stack ) => {
		const at = profile.times[i];
		if ( !frames.length || at < frames[0] ) return;
		while ( frame + 1 < frames.length && frames[frame + 1] <= at ) frame++;
		if ( matches( stack ) ) costs[frame] += owned;
	} );
	return costs;
}

// ============================================================================

// Chrome's process names (process_name metadata) and thread names.
const GPU_PROCESS = "GPU Process";
const RENDERER_PROCESS = "Renderer";
const GPU_MAIN_THREAD = "CrGpuMain";
const RENDERER_MAIN_THREAD = "CrRendererMain";
const US_PER_MS = 1000;

/*
================
namesOf

Process names by pid and thread names by "pid/tid", from the trace's
metadata events. Ids are reused across traces; the names are the contract.
================
*/
function namesOf( events ) {
	const processes = new Map(), threads = new Map();
	for ( const e of events ) {
		if ( e.ph !== "M" ) continue;
		if ( e.name === "process_name" ) processes.set( e.pid, e.args?.name ?? "" );
		if ( e.name === "thread_name" ) threads.set( `${e.pid}/${e.tid}`, e.args?.name ?? "" );
	}
	return { processes, threads };
}

/*
================
threadTime

Wall and CPU µs of one thread's complete events inside [start, end) µs.
Wall is the union of the events' intervals, so nested slices count once.
CPU is the thread duration (tdur) of top-level events only: an event that
begins inside an earlier one is nested in it. Top level is decided over
the whole trace, before clipping, so a task that straddles a window edge
keeps its children nested; its CPU time is clipped in proportion to the
wall time inside. cpu is null when no top-level event carried a tdur.
================
*/
function threadTime( events, start, end ) {
	const sorted = [ ...events ].sort( ( a, b ) => a.ts - b.ts || b.dur - a.dur );
	const inside = ( from, to ) => Math.max( 0, Math.min( to, end ) - Math.max( from, start ) );
	let topEnd = -Infinity, wall = 0, cpu = 0, timed = false;
	for ( const e of sorted ) {
		const finish = e.ts + e.dur;
		if ( e.ts < topEnd ) {
			// Nested: only the part reaching past the enclosing task adds wall time.
			if ( finish > topEnd ) {
				wall += inside( topEnd, finish );
				topEnd = finish;
			}
			continue;
		}
		topEnd = finish;
		const clipped = inside( e.ts, finish );
		wall += clipped;
		if ( Number.isFinite( e.tdur ) ) {
			timed = true;
			if ( e.dur > 0 ) cpu += e.tdur * clipped / e.dur;
		}
	}
	return { wall, cpu: timed ? cpu : null };
}

/*
================
processCosts

Per frame cost of the GPU process's threads and of the renderer main
thread inside window ({ start, end } in trace µs), over frames presented
frames: for each thread its name, wall and CPU (thread time) ms per frame
and its busy share of the window. GPU threads come busiest first; gpuMain
is its CrGpuMain and renderer the busiest renderer main thread. Only
complete ("X") events count: Chrome's toplevel tasks are complete events.
================
*/
export function processCosts( events, window, frames ) {
	if ( !(window.end > window.start) ) throw Error( "processCosts: empty window" );
	if ( !(frames > 0) ) throw Error( "processCosts: no frames in the window" );
	const { processes, threads } = namesOf( events );
	const byThread = new Map();
	for ( const e of events ) {
		if ( e.ph !== "X" || !Number.isFinite( e.dur ) ) continue;
		const process = processes.get( e.pid );
		if ( process !== GPU_PROCESS && process !== RENDERER_PROCESS ) continue;
		const key = `${e.pid}/${e.tid}`;
		if ( process === RENDERER_PROCESS && threads.get( key ) !== RENDERER_MAIN_THREAD ) continue;
		let list = byThread.get( key );
		if ( !list ) byThread.set( key, list = [] );
		list.push( e );
	}
	const span = window.end - window.start;
	const gpu = [], renderers = [];
	for ( const [key, list] of byThread ) {
		const { wall, cpu } = threadTime( list, window.start, window.end );
		const row = {
			pid: list[0].pid,
			tid: list[0].tid,
			name: threads.get( key ) ?? "",
			wallMsPerFrame: wall / US_PER_MS / frames,
			cpuMsPerFrame: cpu === null ? null : cpu / US_PER_MS / frames,
			busy: wall / span
		};
		(processes.get( row.pid ) === GPU_PROCESS ? gpu : renderers).push( row );
	}
	gpu.sort( ( a, b ) => b.busy - a.busy );
	renderers.sort( ( a, b ) => b.busy - a.busy );
	return {
		frames,
		spanMs: span / US_PER_MS,
		gpuMain: gpu.find( row => row.name === GPU_MAIN_THREAD ) ?? null,
		gpu,
		renderer: renderers[0] ?? null
	};
}
