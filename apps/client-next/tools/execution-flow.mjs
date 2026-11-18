import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import path from "node:path";
import fs from "node:fs";
import { root, ts } from "./project.mjs";
import { loadFacts } from "./execution-facts.mjs";
import { seedExecutionGraph } from "./warm-cache.mjs";

/*
================
executionFlow

The resolved execution graph for a client tree. For the real client (the
common case: `verify:execution` and the architecture tests both ask for it)
the graph is cached under temp/cache/execution-flow, keyed by a hash of
everything it is computed from: the TypeScript version, tsconfig.json, the
ownership manifest, these analysis tools and every source file. Any change to
an input changes the key, so a stale graph can never be returned.
================
*/
export function executionFlow( base = root ) {
	if ( path.resolve( base ) !== path.resolve( root ) ) return computeExecutionFlow( base );
	const key = executionFlowKey( base );
	const cacheFile = path.join( base, "temp", "cache", "execution-flow", `${key}.json` );
	if ( fs.existsSync( cacheFile ) ) return JSON.parse( fs.readFileSync( cacheFile, "utf8" ) );
	// Another checkout may already have computed this exact graph; the key
	// hashes every input, so a shared hit is this tree's graph (warm-cache.mjs).
	const shared = seedExecutionGraph( base, key );
	if ( shared ) return shared;
	const graph = computeExecutionFlow( base );
	const directory = path.dirname( cacheFile );
	fs.mkdirSync( directory, { recursive: true } );
	// Only the current tree's graph is worth keeping; drop superseded ones.
	for ( const stale of fs.readdirSync( directory ) ) {
		if ( !stale.startsWith( key ) ) fs.rmSync( path.join( directory, stale ), { force: true } );
	}
	// Write then rename: concurrent checks may compute the same graph at once,
	// and a reader must never see a half-written file.
	const temporary = `${cacheFile}.${process.pid}.tmp`;
	fs.writeFileSync( temporary, JSON.stringify( graph ) );
	fs.renameSync( temporary, cacheFile );
	return graph;
}

/*
================
executionFlowKey
================
*/
function executionFlowKey( base ) {
	const hash = createHash( "sha256" );
	hash.update( ts.version );
	const add = file => {
		hash.update( path.relative( base, file ).replaceAll( "\\", "/" ) );
		hash.update( "\0" );
		hash.update( fs.readFileSync( file ) );
		hash.update( "\0" );
	};
	for ( const file of [ "tsconfig.json", "src/engine/ownership.json" ] ) add( path.join( base, file ) );
	const tools = path.dirname( fileURLToPath( import.meta.url ) );
	for ( const file of [ "execution-flow.mjs", "execution-facts.mjs", "project.mjs" ] ) {
		add( path.join( tools, file ) );
	}
	const walk = directory => {
		for (
			const entry of fs.readdirSync( directory, { withFileTypes: true } ).sort( ( a, b ) =>
				a.name.localeCompare( b.name )
			)
		) {
			const full = path.join( directory, entry.name );
			if ( entry.isDirectory() ) walk( full );
			else add( full );
		}
	};
	walk( path.join( base, "src" ) );
	return hash.digest( "hex" );
}

/*
================
computeExecutionFlow

Inclusion-based value flow. Function values, returned objects, aliases and
parent-issued callbacks propagate to a fixed point. This is conservative:
branches are unioned, never assumed unreachable to make a gate pass.

The checker's part, each file's facts, comes from execution-facts.mjs (per
file, cached by type closure); the fixed point runs here over all of them.
The real client uses the shared store; a scratch tree extracts in process.
================
*/
function computeExecutionFlow( base ) {
	const facts = loadFacts( base, path.resolve( base ) === path.resolve( root ) );
	const values = new Map(), constraints = [], properties = [], calls = [], definitions = new Map();
	const objects = new Set(), maps = new Set(), parameters = new Map(), functionByNode = new Map();
	const EMPTY = new Set();
	const slot = key => {
		if ( !values.has( key ) ) values.set( key, new Set() );
		return key;
	};
	const put = ( key, value ) => values.get( slot( key ) ).add( value );
	const propertyKey = ( objectKey, name ) => `P:${objectKey}#${name}`;
	for ( const file of facts ) {
		for ( const fn of file.functions ) {
			definitions.set( fn.id, { id: fn.id, file: file.file, name: fn.name, line: fn.line, async: fn.async } );
			parameters.set( fn.id, fn.params );
			functionByNode.set( fn.node, fn.id );
		}
		for ( const key of file.objects ) objects.add( key );
		for ( const key of file.maps ) maps.add( key );
	}
	for ( const file of facts ) {
		for ( const [key, value] of file.puts ) put( key, value );
		for ( const pair of file.edges ) constraints.push( pair );
		for ( const p of file.properties ) properties.push( { ...p, file: file.file } );
		for ( const c of file.calls ) {
			const call = { ...c, file: file.file, targets: new Set() };
			calls.push( call );
			for ( const declaration of c.declarations ) {
				const target = functionByNode.get( declaration );
				if ( target ) put( c.expr, `F:${target}` );
			}
		}
	}
	// Worklist fixed point. Inclusion constraints, property reads and writes,
	// Map get/set and call-target binding are monotone set unions, so
	// propagating only keys whose values grew reaches the same least fixed point
	// as re-merging every constraint each pass (which this replaced: it spent
	// most of the analysis re-merging unchanged sets).
	const successors = new Map(), queue = [], queued = new Set(), handled = new Map();
	const index = ( table, key, row ) => {
		const rows = table.get( key );
		if ( rows ) rows.push( row );
		else table.set( key, [ row ] );
	};
	const enqueue = key => {
		if ( !queued.has( key ) ) {
			queued.add( key );
			queue.push( key );
		}
	};
	const flow = ( from, to ) => {
		slot( from );
		slot( to );
		let next = successors.get( from );
		if ( !next ) {
			next = new Set();
			successors.set( from, next );
		}
		if ( next.has( to ) ) return;
		next.add( to );
		const source = values.get( from ), target = values.get( to );
		let grew = false;
		for ( const v of source ) {
			if ( !target.has( v ) ) {
				target.add( v );
				grew = true;
			}
		}
		if ( grew ) enqueue( to );
	};
	const reads = new Map(), mapCalls = new Map(), callees = new Map();
	for ( const p of properties ) index( reads, p.object, p );
	for ( const c of calls ) {
		if ( c.receiver ) index( mapCalls, c.receiver, c );
		index( callees, c.expr, c );
	}
	for ( const [from, to] of constraints ) flow( from, to );
	for ( const [key, set] of values ) if ( set.size ) enqueue( key );
	for ( let head = 0; head < queue.length; head++ ) {
		const key = queue[head];
		queued.delete( key );
		const current = values.get( key );
		for ( const to of successors.get( key ) ?? [] ) {
			const target = values.get( to );
			let grew = false;
			for ( const v of current ) {
				if ( !target.has( v ) ) {
					target.add( v );
					grew = true;
				}
			}
			if ( grew ) enqueue( to );
		}
		let done = handled.get( key );
		if ( !done ) {
			done = new Set();
			handled.set( key, done );
		}
		for ( const v of current ) {
			if ( done.has( v ) ) continue;
			done.add( v );
			const object = v.startsWith( "O:" ) ? v.slice( 2 ) : null;
			if ( object && objects.has( object ) ) {
				for ( const p of reads.get( key ) ?? [] ) {
					if ( p.write ) flow( p.value, propertyKey( object, p.name ) );
					else flow( propertyKey( object, p.name ), p.value );
				}
			}
			if ( object && maps.has( object ) ) {
				for ( const c of mapCalls.get( key ) ?? [] ) {
					if ( c.member === "set" && c.args[1] ) {
						flow( c.args[1], propertyKey( object, "[[MapValues]]" ) );
						flow( key, c.node );
					}
					if ( c.member === "get" ) flow( propertyKey( object, "[[MapValues]]" ), c.node );
				}
			}
			const id = v.startsWith( "F:" ) ? v.slice( 2 ) : null;
			if ( id && parameters.has( id ) ) {
				const params = parameters.get( id );
				for ( const c of callees.get( key ) ?? [] ) {
					c.targets.add( id );
					flow( `R:${id}`, c.node );
					c.args.forEach( ( arg, i ) => {
						if ( params[i] ) flow( arg, params[i] );
					} );
				}
			}
		}
		if ( head > 4096 && head * 2 > queue.length ) {
			queue.splice( 0, head + 1 );
			head = -1;
		}
	}
	// The function values a slot holds, sorted: the output must not depend on
	// the order the worklist happened to reach them in.
	const functionsIn = key =>
		[ ...(values.get( key ) ?? EMPTY) ]
			.filter( v => v.startsWith( "F:" ) && definitions.has( v.slice( 2 ) ) )
			.map( v => v.slice( 2 ) )
			.sort();
	const issues = [], output = [], events = [];
	for ( const p of properties ) {
		if ( !p.write ) continue;
		for ( const id of functionsIn( p.value ) ) events.push( { file: p.file, event: p.name, target: id } );
	}
	for ( const c of calls ) {
		const targets = [ ...c.targets ].sort();
		if ( !targets.length && !c.external ) issues.push( `${c.file}:${c.line}: unresolved execution ${c.text}` );
		const callbacks = [];
		c.args.forEach( ( arg, argument ) => {
			for ( const target of functionsIn( arg ) ) callbacks.push( { argument, target } );
		} );
		output.push( {
			file: c.file,
			line: c.line,
			caller: c.owner,
			expression: c.expression,
			targets,
			external: c.external,
			callbacks
		} );
	}
	const ownership = JSON.parse( fs.readFileSync( path.join( base, "src/engine/ownership.json" ), "utf8" ) );
	return {
		version: 2,
		analysis:
			"Conservative fixed-point function/object value flow; external APIs are declared boundaries, not JavaScript execution proofs.",
		entry: ownership.bootstrap,
		workers: Object.entries( ownership.workers ).map( ( [entry, parent] ) => ({ entry, parent }) ),
		functions: [ ...definitions.values() ],
		calls: output,
		events,
		issues
	};
}
