/*
===========================================================================

trace-model.mjs - threads, frames and sampled profiles of a DevTools trace

Owns reading a Chrome performance trace (the JSON DevTools saves) into
the pieces the analyzer reports on:

- threads: every (process, thread) with its name, a label for workers
  (the script they run) and its busy time;
- frames: the renderer main thread's animation frames;
- profiles: the V8 sampling profile of each profiled thread, rebuilt from
  its ProfileChunk events, with the sampled source position of each sample.

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

One profile per thread that has a Profile event, rebuilt from its chunks.
================
*/
function readProfiles( events, threads ) {
	const owners = new Map();
	for ( const e of events ) {
		if ( e.name === "Profile" ) {
			owners.set( `${e.pid}:${e.id}`, { thread: `${e.pid}/${e.tid}`, start: e.args.data.startTime } );
		}
	}
	const profiles = new Map();
	for ( const e of events ) {
		if ( e.name !== "ProfileChunk" ) continue;
		const owner = owners.get( `${e.pid}:${e.id}` );
		if ( !owner ) continue;
		let profile = profiles.get( owner.thread );
		if ( !profile ) {
			profile = {
				thread: threads.get( owner.thread ),
				nodes: new Map(),
				samples: [],
				deltas: [],
				lines: [],
				columns: [],
				start: owner.start
			};
			profiles.set( owner.thread, profile );
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
	for ( const profile of profiles.values() ) {
		let t = profile.start;
		profile.times = profile.deltas.map( d => (t += d) );
		// Parent links: chunks give parent on the node, or children on the parent.
		for ( const node of profile.nodes.values() ) {
			for ( const child of node.children ?? [] ) {
				const c = profile.nodes.get( child );
				if ( c && c.parent === undefined ) c.parent = node.id;
			}
		}
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
		for ( let id = samples[i]; id !== undefined; id = nodes.get( id )?.parent ) stack.push( id );
		visit( i, owned, stack );
	}
	return gaps;
}
