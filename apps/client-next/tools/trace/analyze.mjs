/*
===========================================================================

analyze.mjs - performance report from a DevTools trace

Usage:
  node --max-old-space-size=12000 tools/trace/analyze.mjs TRACE.json [options]

Options:
  --window A:B        seconds from the trace start (default: all)
  --maps DIR          production source maps (beta package private/maps)
  --dev-root DIR      client root a dev server served (default: this client)
  --top N             rows per table (default 25)
  --gap US            sampler gap credited to one sample (default 2000)
  --long MS           frame interval reported as long (default 25)
  --thread LABEL      profile table for threads whose label contains LABEL
  --children FN       direct callees of functions whose key starts with FN
  --lines FN          self time by source line inside FN
  --compare TRACE[@A:B]  per-function ms/frame change against another trace
  --json              machine-readable output

Sections: threads and their busy time (workers named by script), the GPU
process, frame rate per second and frame interval percentiles, long frames
with what ran in them, and per-frame main-thread self and inclusive time
by function. Main-thread costs are per frame; worker costs per second.

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadTrace, frameStarts, mainThread, forEachSample } from "./trace-model.mjs";
import { createSymbolizer } from "./source-map.mjs";

const CLIENT_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );
const US = 1000;

/*
================
parseArgs
================
*/
function parseArgs( argv ) {
	const options = { top: 25, gap: 2000, long: 25, devRoot: CLIENT_ROOT };
	const rest = [];
	for ( let i = 0; i < argv.length; i++ ) {
		const arg = argv[i];
		const value = () => argv[++i];
		if ( arg === "--window" ) options.window = value();
		else if ( arg === "--maps" ) options.maps = value();
		else if ( arg === "--dev-root" ) options.devRoot = value();
		else if ( arg === "--top" ) options.top = Number( value() );
		else if ( arg === "--gap" ) options.gap = Number( value() );
		else if ( arg === "--long" ) options.long = Number( value() );
		else if ( arg === "--thread" ) options.thread = value();
		else if ( arg === "--children" ) options.children = value();
		else if ( arg === "--lines" ) options.lines = value();
		else if ( arg === "--compare" ) options.compare = value();
		else if ( arg === "--json" ) options.json = true;
		else rest.push( arg );
	}
	if ( !rest[0] ) throw new Error( "usage: analyze.mjs TRACE.json [options] (see the file banner)" );
	options.trace = rest[0];
	return options;
}

/*
================
windowOf

[from, to) in trace microseconds for "A:B" seconds after the trace start.
================
*/
function windowOf( trace, spec ) {
	if ( !spec ) return [ trace.start, trace.end ];
	const [a, b] = spec.split( ":" ).map( Number );
	return [ trace.start + a * 1e6, trace.start + (Number.isFinite( b ) ? b : Infinity) * 1e6 ];
}

/*
================
keyer

Memoized "name file:line" keys of profile nodes.
================
*/
function keyer( symbolizer, profile ) {
	const keys = new Map();
	return id => {
		let key = keys.get( id );
		if ( key === undefined ) {
			const node = profile.nodes.get( id ), frame = symbolizer.frame( node.callFrame );
			key = frame.file ? `${frame.name} ${frame.file}:${frame.line}` : frame.name;
			keys.set( id, key );
		}
		return key;
	};
}

/*
================
attribute

Self and inclusive µs by function key over a window.
================
*/
function attribute( profile, window, symbolizer, gap ) {
	const key = keyer( symbolizer, profile ), self = new Map(), total = new Map();
	let sampled = 0;
	const gaps = forEachSample( profile, window[0], window[1], gap, ( i, owned, stack ) => {
		sampled += owned;
		const leaf = key( stack[0] );
		self.set( leaf, (self.get( leaf ) ?? 0) + owned );
		const seen = new Set();
		for ( const id of stack ) {
			const k = key( id );
			if ( seen.has( k ) ) continue;
			seen.add( k );
			total.set( k, (total.get( k ) ?? 0) + owned );
		}
	} );
	return { self, total, sampled, gaps };
}

/*
================
children

Time of each direct callee under functions whose key starts with prefix.
================
*/
function children( profile, window, symbolizer, gap, prefix ) {
	const key = keyer( symbolizer, profile ), by = new Map();
	let sum = 0;
	forEachSample( profile, window[0], window[1], gap, ( i, owned, stack ) => {
		const at = stack.findIndex( id => key( id ).startsWith( prefix ) );
		if ( at < 0 ) return;
		const k = at === 0 ? "(self)" : key( stack[at - 1] );
		by.set( k, (by.get( k ) ?? 0) + owned );
		sum += owned;
	} );
	return { by, sum };
}

/*
================
lines

Self time by source line inside functions whose key starts with prefix.
================
*/
function lines( profile, window, symbolizer, gap, prefix ) {
	const key = keyer( symbolizer, profile ), by = new Map();
	let sum = 0;
	forEachSample( profile, window[0], window[1], gap, ( i, owned, stack ) => {
		if ( !key( stack[0] ).startsWith( prefix ) ) return;
		const url = profile.nodes.get( stack[0] ).callFrame.url;
		const at = symbolizer.position( url, profile.lines[i], profile.columns[i] );
		const k = at ? `${at.file}:${at.line}` : `served ${profile.lines[i]}:${profile.columns[i]}`;
		by.set( k, (by.get( k ) ?? 0) + owned );
		sum += owned;
	} );
	return { by, sum };
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
frameReport

Frame rate per second, interval percentiles and the long frames.
================
*/
function frameReport( trace, main, window, long ) {
	const starts = frameStarts( trace, main ).filter( t => t >= window[0] && t < window[1] );
	const perSecond = new Map(), intervals = [], longFrames = [];
	for ( let i = 0; i < starts.length; i++ ) {
		const second = Math.floor( (starts[i] - trace.start) / 1e6 );
		perSecond.set( second, (perSecond.get( second ) ?? 0) + 1 );
		if ( i + 1 < starts.length ) {
			const interval = (starts[i + 1] - starts[i]) / US;
			intervals.push( interval );
			if ( interval > long ) longFrames.push( { at: starts[i], end: starts[i + 1], ms: interval } );
		}
	}
	const sorted = [ ...intervals ].sort( ( a, b ) => a - b );
	return {
		frames: starts.length,
		perSecond,
		p50: percentile( sorted, .5 ),
		p90: percentile( sorted, .9 ),
		p99: percentile( sorted, .99 ),
		max: sorted.at( -1 ) ?? 0,
		longFrames
	};
}

/*
================
windowBusy

RunTask time of each thread inside the window, and the time of the other
slices on it, so windows of different traces compare like for like.
================
*/
function windowBusy( trace, window ) {
	const busy = new Map(), slices = new Map();
	for ( const e of trace.events ) {
		if ( e.ph !== "X" || !Number.isFinite( e.dur ) ) continue;
		const from = Math.max( e.ts, window[0] ), to = Math.min( e.ts + e.dur, window[1] );
		if ( to <= from ) continue;
		const key = `${e.pid}/${e.tid}`;
		if ( e.name === "RunTask" ) busy.set( key, (busy.get( key ) ?? 0) + to - from );
		else {
			let names = slices.get( key );
			if ( !names ) slices.set( key, names = new Map() );
			names.set( e.name, (names.get( e.name ) ?? 0) + to - from );
		}
	}
	return { busy, slices };
}

/*
================
table
================
*/
function table( map, scale, top, unit ) {
	return [ ...map ].sort( ( a, b ) => b[1] - a[1] ).slice( 0, top ).map( ( [k, v] ) =>
		`${(v / scale).toFixed( 3 ).padStart( 9 )} ${unit}  ${k}`
	).join( "\n" );
}

/*
================
analyzeTrace

Everything the report needs for one trace and window.
================
*/
async function analyzeTrace( file, spec, options ) {
	const trace = loadTrace( file ), main = mainThread( trace ), window = windowOf( trace, spec );
	const symbolizer = await createSymbolizer( { maps: options.maps, root: options.devRoot } );
	const urls = new Set();
	for ( const profile of trace.profiles.values() ) {
		for ( const node of profile.nodes.values() ) urls.add( node.callFrame.url );
	}
	await symbolizer.prepare( urls );
	const frames = frameReport( trace, main, window, options.long );
	const mainProfile = trace.profiles.get( main.key );
	const mainCost = mainProfile ? attribute( mainProfile, window, symbolizer, options.gap ) : null;
	return { trace, main, window, symbolizer, frames, mainProfile, mainCost };
}

/*
================
report
================
*/
async function report( options ) {
	const a = await analyzeTrace( options.trace, options.window, options );
	const { trace, main, window, symbolizer, frames, mainProfile, mainCost } = a;
	const seconds = (window[1] === Infinity ? trace.end : Math.min( window[1], trace.end )) -
		Math.max( window[0], trace.start );
	const span = seconds / 1e6;
	const out = [];
	out.push( `# ${path.basename( trace.file )}  window ${options.window ?? "all"}  (${span.toFixed( 1 )} s)` );
	out.push( "\n## Threads (RunTask time in the window: ms, % of window, ms per frame)" );
	const { busy, slices } = windowBusy( trace, [
		Math.max( window[0], trace.start ),
		Math.min( window[1], trace.end )
	] );
	for ( const [key, time] of [ ...busy ].sort( ( x, y ) => y[1] - x[1] ).slice( 0, 14 ) ) {
		const thread = trace.threads.get( key );
		const top = [ ...(slices.get( key ) ?? []) ].sort( ( x, y ) => y[1] - x[1] ).slice( 0, 4 )
			.map( ( [k, v] ) => `${k} ${(v / US).toFixed( 0 )}` ).join( ", " );
		const perFrame = time / US / Math.max( 1, frames.frames );
		out.push(
			`${(time / US).toFixed( 0 ).padStart( 8 )} ms ${(time / seconds * 100).toFixed( 0 ).padStart( 4 )}% ${
				perFrame.toFixed( 2 ).padStart( 6 )
			} ms/f  ${thread?.label ?? key} [${key}]  ${top}`
		);
	}
	out.push( "\n## Frames" );
	out.push(
		`frames ${frames.frames}, interval ms p50 ${frames.p50.toFixed( 1 )} p90 ${frames.p90.toFixed( 1 )} p99 ${
			frames.p99.toFixed( 1 )
		} max ${frames.max.toFixed( 1 )}`
	);
	out.push( "per second: " + [ ...frames.perSecond ].map( ( [s, n] ) => `${s}:${n}` ).join( " " ) );
	if ( mainProfile && frames.longFrames.length ) {
		out.push( `\n## Long frames (> ${options.long} ms): what ran` );
		for ( const frame of frames.longFrames.slice( 0, 20 ) ) {
			const cost = attribute( mainProfile, [ frame.at, frame.end ], symbolizer, options.gap );
			const top = [ ...cost.total ].filter( ( [k] ) => !/^\((root|program|idle)\)/.test( k ) )
				.sort( ( x, y ) => y[1] - x[1] ).slice( 1, 6 ).map( ( [k, v] ) => `${k} ${(v / US).toFixed( 1 )}` )
				.join( " | " );
			out.push( `${((frame.at - trace.start) / 1e6).toFixed( 2 )} s ${frame.ms.toFixed( 0 )} ms: ${top}` );
		}
	}
	if ( mainCost ) {
		const perFrame = Math.max( 1, frames.frames ) * US;
		out.push(
			`\n## Main thread per frame (sampled ${(mainCost.sampled / perFrame).toFixed( 2 )} ms, sampler gap ${
				(mainCost.gaps / perFrame).toFixed( 2 )
			} ms)`
		);
		out.push( "### self\n" + table( mainCost.self, perFrame, options.top, "ms/f" ) );
		out.push( "### inclusive\n" + table( mainCost.total, perFrame, options.top, "ms/f" ) );
	}
	for ( const profile of trace.profiles.values() ) {
		if ( profile === mainProfile ) continue;
		if ( options.thread && !profile.thread.label.includes( options.thread ) ) continue;
		const cost = attribute( profile, window, symbolizer, options.gap );
		if ( cost.sampled < 5 * US ) continue;
		out.push(
			`\n## ${profile.thread.label} (sampled ${(cost.sampled / US).toFixed( 0 )} ms, ${
				(cost.sampled / seconds * 100).toFixed( 0 )
			}% of window)`
		);
		out.push( "### self\n" + table( cost.self, span * US, Math.min( options.top, 15 ), "ms/s" ) );
		out.push( "### inclusive\n" + table( cost.total, span * US, Math.min( options.top, 15 ), "ms/s" ) );
	}
	for ( const [label, run] of [ [ "children", children ], [ "lines", lines ] ] ) {
		if ( !options[label] ) continue;
		for ( const profile of trace.profiles.values() ) {
			const result = run( profile, window, symbolizer, options.gap, options[label] );
			if ( !result.sum ) continue;
			const scale = profile === mainProfile ? Math.max( 1, frames.frames ) * US : US,
				unit = profile === mainProfile ? "ms/f" : "ms";
			out.push(
				`\n## ${label} of ${options[label]} on ${profile.thread.label} (${
					(result.sum / scale).toFixed( 3 )
				} ${unit})`
			);
			out.push( table( result.by, scale, options.top, unit ) );
		}
	}
	if ( options.compare && mainCost ) {
		const [file, spec] = options.compare.split( "@" );
		const b = await analyzeTrace( file, spec, options );
		const perA = Math.max( 1, frames.frames ) * US, perB = Math.max( 1, b.frames.frames ) * US;
		const delta = new Map();
		// Builds move code: match a function by name and file, not by line.
		const stable = k => k.replace( /:\d+$/, "" );
		for ( const [k, v] of b.mainCost.total ) delta.set( stable( k ), (delta.get( stable( k ) ) ?? 0) + v / perB );
		for ( const [k, v] of mainCost.total ) delta.set( stable( k ), (delta.get( stable( k ) ) ?? 0) - v / perA );
		out.push(
			`\n## Compare: ${path.basename( file )} minus this trace, inclusive ms/frame (fps ${
				frames.frames / span | 0
			} vs ${b.frames.frames} frames)`
		);
		out.push( "### grew\n" + table( delta, 1, options.top, "ms/f" ) );
		out.push(
			"### shrank\n" + table( new Map( [ ...delta ].map( ( [k, v] ) => [ k, -v ] ) ), 1, options.top, "ms/f" )
		);
	}
	if ( options.json ) {
		console.log( JSON.stringify( {
			trace: trace.file,
			window: options.window ?? "all",
			frames: { ...frames, perSecond: Object.fromEntries( frames.perSecond ) },
			threads: [ ...trace.threads.values() ].map( t => ({ label: t.label, key: t.key, busyMs: t.busy / US }) ),
			mainSelf: mainCost && Object.fromEntries( [ ...mainCost.self ].map( ( [k, v] ) => [ k, v / US ] ) ),
			mainTotal: mainCost && Object.fromEntries( [ ...mainCost.total ].map( ( [k, v] ) => [ k, v / US ] ) )
		} ) );
		return;
	}
	console.log( out.join( "\n" ) );
}

await report( parseArgs( process.argv.slice( 2 ) ) );
