/*
===========================================================================

profile.mjs - CPU and allocation call trees as weighted stacks

Two profile shapes come out of the CDP domains (and DevTools):

- a CPU profile (.cpuprofile, Profiler.stop): flat nodes with children
  ids, samples and the time deltas between them;
- a sampled heap profile (.heapprofile, HeapProfiler.stopSampling): a
  nested tree from head, each node with its sampled self size, and the
  samples themselves. Sampled with the collected-object flags on, it is an
  allocation profile: what the code allocates, live or not.

Both reduce to one thing the tools aggregate: a call stack (symbolized
"name file:line" keys, innermost first) with a weight, microseconds or
bytes. CPU nodes also carry per-line ticks for --lines.

===========================================================================
*/
import { readFileSync } from "node:fs";

/*
================
readProfile

The profile in file: { kind: "cpu" | "heap", nodes (id -> node), parent
(id -> parent id), weights (id -> weight) }.
================
*/
export function readProfile( file ) {
	const raw = JSON.parse( readFileSync( file, "utf8" ) );
	const nodes = new Map(), parent = new Map(), weights = new Map();
	if ( raw.head ) {
		// Sampled heap profile: nested nodes; the samples weigh their nodes.
		const stack = [ raw.head ];
		while ( stack.length ) {
			const node = stack.pop();
			nodes.set( node.id, node );
			for ( const child of node.children ?? [] ) {
				parent.set( child.id, node.id );
				stack.push( child );
			}
		}
		if ( raw.samples?.length ) {
			for ( const sample of raw.samples ) {
				weights.set( sample.nodeId, (weights.get( sample.nodeId ) ?? 0) + sample.size );
			}
		} else for ( const node of nodes.values() ) if ( node.selfSize ) weights.set( node.id, node.selfSize );
		return { kind: "heap", nodes, parent, weights };
	}
	for ( const node of raw.nodes ) {
		nodes.set( node.id, node );
		for ( const child of node.children ?? [] ) parent.set( child, node.id );
	}
	// A sample owns the time until the next one.
	for ( let i = 0; i < raw.samples.length; i++ ) {
		const id = raw.samples[i];
		weights.set( id, (weights.get( id ) ?? 0) + (raw.timeDeltas[i + 1] ?? 0) );
	}
	return { kind: "cpu", nodes, parent, weights };
}

/*
================
urlsOf

Every script URL a profile names, for symbolizer.prepare.
================
*/
export function urlsOf( profile ) {
	return new Set( [ ...profile.nodes.values() ].map( node => node.callFrame.url ).filter( Boolean ) );
}

/*
================
keyOf

The "name file:line" key of a call frame.
================
*/
export function keyOf( symbolizer, callFrame ) {
	const frame = symbolizer.frame( callFrame );
	return frame.file ? `${frame.name} ${frame.file}:${frame.line}` : frame.name;
}

/*
================
forEachStack

Calls visit( stack, weight ) for every weighted node, the stack its keys
innermost first.
================
*/
export function forEachStack( profile, symbolizer, visit ) {
	const keys = new Map();
	const key = id => {
		let value = keys.get( id );
		if ( value === undefined ) {
			value = keyOf( symbolizer, profile.nodes.get( id ).callFrame );
			keys.set( id, value );
		}
		return value;
	};
	for ( const [id, weight] of profile.weights ) {
		const stack = [];
		for ( let at = id; at !== undefined; at = profile.parent.get( at ) ) stack.push( key( at ) );
		visit( stack, weight );
	}
}

/*
================
forEachLine

CPU profiles only: calls visit( "file:line", weight ) for the source lines
of nodes whose key starts with prefix, sharing each node's weight by its
line ticks.
================
*/
export function forEachLine( profile, symbolizer, prefix, visit ) {
	for ( const node of profile.nodes.values() ) {
		const weight = profile.weights.get( node.id );
		if ( !weight || !node.positionTicks?.length || !keyOf( symbolizer, node.callFrame ).startsWith( prefix ) ) {
			continue;
		}
		const ticks = node.positionTicks.reduce( ( sum, tick ) => sum + tick.ticks, 0 );
		for ( const tick of node.positionTicks ) {
			const at = symbolizer.position( node.callFrame.url, tick.line, 1 );
			visit( at ? `${at.file}:${at.line}` : `${node.callFrame.url}:${tick.line}`, weight * tick.ticks / ticks );
		}
	}
}
