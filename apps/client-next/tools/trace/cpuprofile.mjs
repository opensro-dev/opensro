/*
===========================================================================

cpuprofile.mjs - where a sampled CPU profile spends its time

Usage:
  node tools/trace/cpuprofile.mjs PROFILE.cpuprofile [more...] [options]

Options:
  --top N          rows per table (default 30)
  --lines FN       self time by source line inside functions whose key
                   starts with FN
  --children FN    time under each direct callee of functions whose key
                   starts with FN, and their own self time
  --maps DIR       production source maps (beta package private/maps)
  --dev-root DIR   client root a dev server served (default: this client)

A .cpuprofile is what the CDP Profiler domain (or DevTools' JavaScript
profiler, or node --cpu-prof) records: a call tree and its samples, with
no per-call cost to the code measured. Several profiles of the same run
(one per phase) are summed. Frames are mapped back to client source as
analyze.mjs does; times are per sampled second of profile.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createSymbolizer } from "./source-map.mjs";

const CLIENT_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );

/*
================
parseArgs
================
*/
function parseArgs( argv ) {
	const options = { files: [], top: 30, lines: null, children: null, maps: null, devRoot: CLIENT_ROOT };
	for ( let i = 0; i < argv.length; i++ ) {
		const arg = argv[i], value = () => argv[++i];
		if ( arg === "--top" ) options.top = Number( value() );
		else if ( arg === "--lines" ) options.lines = value();
		else if ( arg === "--children" ) options.children = value();
		else if ( arg === "--maps" ) options.maps = value();
		else if ( arg === "--dev-root" ) options.devRoot = value();
		else options.files.push( arg );
	}
	if ( !options.files.length ) {
		throw Error( "usage: cpuprofile.mjs PROFILE.cpuprofile [more...] [--top N] [--lines FN] [--children FN]" );
	}
	return options;
}

/*
================
table
================
*/
function table( map, seconds, top ) {
	return [ ...map ].sort( ( a, b ) => b[1] - a[1] ).slice( 0, top ).map( ( [key, us] ) =>
		`${(us / 1000 / seconds).toFixed( 3 ).padStart( 9 )} ms/s  ${key}`
	).join( "\n" );
}

/*
================
add
================
*/
function add( map, key, us ) {
	map.set( key, (map.get( key ) ?? 0) + us );
}

/*
================
report
================
*/
async function report( options ) {
	const symbolizer = await createSymbolizer( { maps: options.maps, root: options.devRoot } );
	const self = new Map(), total = new Map(), lines = new Map(), children = new Map();
	let sampled = 0;
	for ( const file of options.files ) {
		const profile = JSON.parse( readFileSync( file, "utf8" ) );
		await symbolizer.prepare( new Set( profile.nodes.map( node => node.callFrame.url ) ) );
		const parent = new Map(), keyOf = new Map(), nodeSelf = new Map();
		for ( const node of profile.nodes ) for ( const child of node.children ?? [] ) parent.set( child, node.id );
		for ( const node of profile.nodes ) {
			const frame = symbolizer.frame( node.callFrame );
			keyOf.set( node.id, frame.file ? `${frame.name} ${frame.file}:${frame.line}` : frame.name );
		}
		// A sample owns the time until the next one.
		for ( let i = 0; i < profile.samples.length; i++ ) {
			const us = profile.timeDeltas[i + 1] ?? 0, id = profile.samples[i];
			sampled += us;
			add( self, keyOf.get( id ), us );
			add( nodeSelf, id, us );
			const seen = new Set();
			for ( let at = id; at !== undefined; at = parent.get( at ) ) {
				const frame = keyOf.get( at );
				if ( seen.has( frame ) ) continue;
				seen.add( frame );
				add( total, frame, us );
			}
			if ( !options.children ) continue;
			// The callee of the innermost matching frame on this stack.
			for ( let at = id, below = null; at !== undefined; below = at, at = parent.get( at ) ) {
				if ( !keyOf.get( at ).startsWith( options.children ) ) continue;
				add( children, below === null ? "(self)" : keyOf.get( below ), us );
				break;
			}
		}
		if ( !options.lines ) continue;
		// Line ticks carry no time: share the node's self time by its ticks.
		for ( const node of profile.nodes ) {
			if ( !keyOf.get( node.id ).startsWith( options.lines ) || !node.positionTicks ) continue;
			const ticks = node.positionTicks.reduce( ( a, t ) => a + t.ticks, 0 );
			for ( const tick of node.positionTicks ) {
				const at = symbolizer.position( node.callFrame.url, tick.line, 1 );
				const key = at ? `${at.file}:${at.line}` : `${node.callFrame.url}:${tick.line}`;
				add( lines, key, (nodeSelf.get( node.id ) ?? 0) * tick.ticks / ticks );
			}
		}
	}
	const seconds = sampled / 1e6;
	const out = [
		`# ${options.files.map( f => path.basename( f ) ).join( ", " )}  (${seconds.toFixed( 2 )} s sampled)`
	];
	out.push( "\n## self\n" + table( self, seconds, options.top ) );
	out.push( "\n## inclusive\n" + table( total, seconds, options.top ) );
	if ( options.children ) {
		out.push( `\n## children of ${options.children}\n` + table( children, seconds, options.top ) );
	}
	if ( options.lines ) out.push( `\n## lines of ${options.lines}\n` + table( lines, seconds, options.top ) );
	console.log( out.join( "\n" ) );
}

await report( parseArgs( process.argv.slice( 2 ) ) );
