/*
===========================================================================

heap.mjs - allocation report from a DevTools heap timeline

Usage:
  node --max-old-space-size=14000 tools/trace/heap.mjs FILE.heaptimeline [--top N] [--callers FN]

A heap timeline ("Allocation instrumentation on timeline") holds the heap
snapshot taken at its end plus an allocation trace tree: every allocating
call stack with the count and bytes it allocated during recording. This
reports:

- churn: bytes and objects allocated per function (self and inclusive),
  which is what the garbage collector later pays for;
- retained: bytes still alive at the end, by the stack that allocated them;
- rate: allocation per second over the recording (samples);
- callers (--callers FN): the stacks that call functions whose key starts
  with FN, by the bytes those calls allocated.

Files can exceed V8's largest string, so only the needed sections are
sliced out by bracket matching and parsed one at a time.

===========================================================================
*/
import { openSync, readSync, fstatSync, closeSync } from "node:fs";
import path from "node:path";

const CHUNK = 64 << 20;

/*
================
readFile

The whole file as one Buffer (Buffers are not limited like strings).
================
*/
function readFile( file ) {
	const fd = openSync( file, "r" ), size = fstatSync( fd ).size, buffer = Buffer.allocUnsafe( size );
	for ( let at = 0; at < size; ) at += readSync( fd, buffer, at, Math.min( CHUNK, size - at ), at );
	closeSync( fd );
	return buffer;
}

/*
================
section

The JSON value after "key": in buffer, parsed. Brackets are matched
outside strings, so the slice is exactly one array or object.
================
*/
function section( buffer, key ) {
	const marker = Buffer.from( `"${key}":` ), at = buffer.indexOf( marker );
	if ( at < 0 ) return null;
	let i = at + marker.length;
	while ( buffer[i] === 0x20 || buffer[i] === 0x0a || buffer[i] === 0x0d ) i++;
	const open = buffer[i], close = open === 0x5b ? 0x5d : 0x7d;
	let depth = 0, inString = false;
	for ( let j = i; j < buffer.length; j++ ) {
		const c = buffer[j];
		if ( inString ) {
			if ( c === 0x5c ) j++;
			else if ( c === 0x22 ) inString = false;
			continue;
		}
		if ( c === 0x22 ) inString = true;
		else if ( c === open ) depth++;
		else if ( c === close && --depth === 0 ) return JSON.parse( buffer.toString( "utf8", i, j + 1 ) );
	}
	throw new Error( `Unterminated section ${key}` );
}

/*
================
numbers

A large flat number array section, parsed without one giant string.
================
*/
function numbers( buffer, key ) {
	const marker = Buffer.from( `"${key}":[` ), at = buffer.indexOf( marker );
	if ( at < 0 ) return null;
	const out = [];
	let value = 0, digits = false, negative = false;
	for ( let i = at + marker.length; i < buffer.length; i++ ) {
		const c = buffer[i];
		if ( c >= 0x30 && c <= 0x39 ) {
			value = value * 10 + (c - 0x30);
			digits = true;
		} else if ( c === 0x2d ) negative = true;
		else {
			if ( digits ) out.push( negative ? -value : value );
			value = 0;
			digits = false;
			negative = false;
			if ( c === 0x5d ) break;
		}
	}
	return out;
}

/*
================
table
================
*/
function table( map, top, unit = "KB" ) {
	return [ ...map ].sort( ( a, b ) => b[1].size - a[1].size ).slice( 0, top ).map( ( [k, v] ) =>
		`${(v.size / 1024).toFixed( 0 ).padStart( 10 )} ${unit} ${String( v.count ).padStart( 9 )} objs  ${k}`
	).join( "\n" );
}

/*
================
report
================
*/
function report( file, top, callersOf ) {
	const buffer = readFile( file );
	const meta = section( buffer, "snapshot" ).meta;
	const infos = section( buffer, "trace_function_infos" ) ?? [];
	const tree = section( buffer, "trace_tree" ) ?? [];
	const strings = section( buffer, "strings" ) ?? [];
	const infoWidth = meta.trace_function_info_fields.length;
	const nameOf = index => {
		const base = index * infoWidth, name = strings[infos[base + 1]] || "(anonymous)";
		const script = strings[infos[base + 2]] ?? "", line = infos[base + 4];
		const file = script ? script.split( /[\\/]/ ).slice( -2 ).join( "/" ).replace( /\?.*$/, "" ) : "";
		return file ? `${name} ${file}:${line + 1}` : name;
	};
	// trace_tree nodes: [ id, function_info_index, count, size, [ children... ] ].
	const self = new Map(), inclusive = new Map(), byNode = new Map(), callers = new Map();
	let allocated = 0, objects = 0;
	/*
	================
	walk
	================
	*/
	function walk( nodes, stack ) {
		for ( let i = 0; i < nodes.length; i += 5 ) {
			const id = nodes[i], fn = nameOf( nodes[i + 1] ), count = nodes[i + 2], size = nodes[i + 3];
			byNode.set( id, [ ...stack, fn ] );
			if ( size ) {
				const row = self.get( fn ) ?? { size: 0, count: 0 };
				row.size += size;
				row.count += count;
				self.set( fn, row );
				allocated += size;
				objects += count;
				if ( callersOf && fn.startsWith( callersOf ) ) {
					const key = stack.slice( -3 ).join( " > " ) || "(root)",
						row = callers.get( key ) ?? { size: 0, count: 0 };
					row.size += size;
					row.count += count;
					callers.set( key, row );
				}
				const seen = new Set();
				for ( const frame of [ ...stack, fn ] ) {
					if ( seen.has( frame ) ) continue;
					seen.add( frame );
					const total = inclusive.get( frame ) ?? { size: 0, count: 0 };
					total.size += size;
					total.count += count;
					inclusive.set( frame, total );
				}
			}
			walk( nodes[i + 4], stack.length > 64 ? stack : [ ...stack, fn ] );
		}
	}
	walk( tree, [] );
	// Live objects at the end, by the stack that allocated them.
	const retained = new Map();
	const nodes = numbers( buffer, "nodes" ) ?? [];
	const width = meta.node_fields.length,
		sizeAt = meta.node_fields.indexOf( "self_size" ),
		traceAt = meta.node_fields.indexOf( "trace_node_id" );
	let live = 0;
	for ( let i = 0; i < nodes.length; i += width ) {
		const trace = nodes[i + traceAt];
		if ( !trace ) continue;
		const stack = byNode.get( trace );
		if ( !stack ) continue;
		const key = stack.slice( -3 ).join( " < " ), row = retained.get( key ) ?? { size: 0, count: 0 };
		row.size += nodes[i + sizeAt];
		row.count++;
		retained.set( key, row );
		live += nodes[i + sizeAt];
	}
	const samples = numbers( buffer, "samples" ) ?? [];
	const out = [
		`# ${path.basename( file )}`,
		`allocated during recording: ${
			(allocated / 1048576).toFixed( 1 )
		} MB in ${objects} objects; traced live at end ${(live / 1048576).toFixed( 1 )} MB`
	];
	if ( samples.length >= 4 ) {
		const seconds = (samples[samples.length - 2] - samples[0]) / 1e6;
		out.push(
			`recording ${seconds.toFixed( 1 )} s, ${
				(allocated / 1048576 / Math.max( seconds, 1e-3 )).toFixed( 1 )
			} MB/s allocated`
		);
	}
	out.push( "\n## Allocated by function (self)\n" + table( self, top ) );
	out.push( "\n## Allocated by function (inclusive)\n" + table( inclusive, top ) );
	out.push( "\n## Live at end, by allocating stack (innermost last)\n" + table( retained, top ) );
	if ( callersOf ) out.push( `\n## Callers of ${callersOf} (outermost first)\n` + table( callers, top ) );
	console.log( out.join( "\n" ) );
}

const args = process.argv.slice( 2 ), topAt = args.indexOf( "--top" ), callersAt = args.indexOf( "--callers" );
const top = topAt >= 0 ? Number( args[topAt + 1] ) : 30, callersOf = callersAt >= 0 ? args[callersAt + 1] : undefined;
const file = args.find( ( a, i ) => !a.startsWith( "--" ) && args[i - 1] !== "--top" && args[i - 1] !== "--callers" );
if ( !file ) throw new Error( "usage: heap.mjs FILE.heaptimeline [--top N] [--callers FN]" );
report( file, top, callersOf );
