/*
===========================================================================

verify-execution.mjs - the resolved execution graph obeys execution-contract.json

Checks the graph execution-flow.mjs resolves from the sources against the
reviewed contract: sealed native frame barriers, unresolved execution,
external capabilities used outside their owner, cross-module calls without
a grant, the declared call order of each sequence, and async functions
reachable from the frame.

The graph is indexed once (calls by caller, boundaries by call site, grants
by consumer, provider and function), so each check is a lookup rather than
a scan over the whole graph or contract.

===========================================================================
*/
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { main, root } from "./project.mjs";
import { executionFlow } from "./execution-flow.mjs";
import { rules } from "./verify-capabilities.mjs";

const RUNTIME = "src/engine/runtime/runtime.ts";
// Array methods that run their callbacks before returning.
const SYNCHRONOUS_CALLBACKS = /:(?:map|forEach|filter|some|every|sort|reduce|find)$/;

/*
================
verifyBarriers

A sealed frame barrier names the exact source bytes it was proven against,
the native evidence and its regression tests.
================
*/
function verifyBarriers( base, barriers, issues ) {
	for ( const barrier of barriers ) {
		const source = path.join( base, barrier.file ), evidence = path.join( base, barrier.evidence );
		if (
			!barrier.reason || !fs.existsSync( source ) ||
			createHash( "sha256" ).update( fs.readFileSync( source ) ).digest( "hex" ) !== barrier.sourceSha256 ||
			!fs.existsSync( evidence ) || !barrier.tests?.length ||
			barrier.tests.some( test => !fs.existsSync( path.join( base, test ) ) )
		) {
			issues.push( `Unverified native frame barrier: ${barrier.file}#${barrier.function}` );
		} else if (
			!JSON.parse( fs.readFileSync( evidence, "utf8" ) ).functions.some( fn => fn.va === barrier.nativeFunction )
		) {
			issues.push( `Missing native frame barrier evidence: ${barrier.file}#${barrier.function}` );
		}
	}
}

/*
================
sameOwner

A call needs no grant inside one file, between a module and its owner,
within one internal group, or into the shared contracts and foundation.
================
*/
function sameOwner( manifest, from, to ) {
	return to === from || manifest.modules[to] === from || manifest.internals[to] === from ||
		manifest.internals[from] &&
			(manifest.internals[from] === to || manifest.internals[from] === manifest.internals[to]) ||
		to.startsWith( "src/engine/contracts/" ) || to.startsWith( "src/engine/foundation/" );
}

/*
================
verifyCalls
================
*/
function verifyCalls( graph, defs, manifest, contract, issues ) {
	const invoked = new Set( graph.calls.flatMap( c => c.targets ) );
	const boundaries = new Map();
	for ( const b of contract.boundaries ) {
		const key = `${b.file}\0${b.expression}\0${b.function}`;
		// The first matching boundary wins, as a linear find would.
		if ( !boundaries.has( key ) ) boundaries.set( key, b );
	}
	const grants = new Set();
	for ( const g of contract.capabilities ) {
		for ( const fn of g.functions ) grants.add( `${g.consumer}\0${g.provider}\0${fn}` );
	}
	for ( const call of graph.calls ) {
		const fn = defs.get( call.caller );
		if ( !call.targets.length && !call.external ) {
			const boundary = boundaries.get( `${call.file}\0${call.expression}\0${fn?.name ?? null}` );
			if ( !boundary || boundary.kind === "unwired" && invoked.has( call.caller ) ) {
				issues.push( `${call.file}:${call.line}: unresolved execution ${call.expression}` );
			}
		}
		const externalName = call.external?.split( ":" ).at( -1 );
		if ( Object.hasOwn( rules, externalName ) && !rules[externalName].includes( call.file ) ) {
			issues.push( `${call.file}:${call.line}: resolved external ${externalName} escapes its owner` );
		}
		for ( const target of [ ...call.targets, ...(call.external ? call.callbacks.map( c => c.target ) : []) ] ) {
			const to = defs.get( target );
			if ( sameOwner( manifest, call.file, to.file ) ) continue;
			if ( !grants.has( `${call.file}\0${to.file}\0${to.name}` ) ) {
				issues.push( `${call.file}:${call.line}: undeclared capability to ${to.file}#${to.name}` );
			}
		}
	}
}

/*
================
verifySequences

Each declared sequence's calls appear in order among its owner's calls.
Generic owner registration conservatively unions same-named methods. A
call-site selector narrows the check while STILL requiring the declared
resolved provider; spelling alone cannot satisfy a target or grant a
capability.
================
*/
function verifySequences( graph, defs, callsByCaller, contract, issues ) {
	for ( const sequence of contract.sequences ) {
		const owner = graph.functions.find( f => f.file === sequence.file && f.name === sequence.function );
		const calls = callsByCaller.get( owner?.id ) ?? [];
		let previous = -1;
		for ( const entry of sequence.calls ) {
			const target = typeof entry === "string" ? entry : entry.target;
			const expression = typeof entry === "string" ? undefined : entry.expression;
			const index = calls.findIndex( c =>
				(expression === undefined || c.expression === expression) && (target.startsWith( "external:" ) ?
					c.external?.endsWith( ":" + target.slice( 9 ) ) :
					c.targets.some( id => {
						const f = defs.get( id );
						return `${f.file}#${f.name}` === target;
					} ))
			);
			if ( index <= previous || index < 0 ) {
				const order = sequence.calls.map( c => typeof c === "string" ? c : `${c.expression} (${c.target})` );
				issues.push( `${sequence.file}#${sequence.function}: required order ${order.join( " -> " )}` );
			}
			previous = index;
		}
	}
}

/*
================
verifyFrameClosure

Everything the frame reaches synchronously must be synchronous, except a
sealed barrier.
================
*/
function verifyFrameClosure( graph, defs, callsByCaller, barriers, issues ) {
	const frame = graph.functions.find( f => f.file === RUNTIME && f.name === "frame" );
	const reachable = new Set( frame ? [ frame.id ] : [] ), pending = [ ...reachable ];
	if ( !frame ) issues.push( "Missing runtime frame entry" );
	while ( pending.length ) {
		const id = pending.pop(), def = defs.get( id );
		if ( def.async && !barriers.some( b => b.file === def.file && b.function === def.name ) ) {
			issues.push( `Async function in frame closure: ${id}` );
		}
		for ( const call of callsByCaller.get( id ) ?? [] ) {
			const callbacks = SYNCHRONOUS_CALLBACKS.test( call.external ?? "" ) ?
				call.callbacks.map( c => c.target ) :
				[];
			for ( const target of [ ...call.targets, ...callbacks ] ) {
				if ( !reachable.has( target ) ) {
					reachable.add( target );
					pending.push( target );
				}
			}
		}
	}
}

/*
================
verifyExecution
================
*/
export function verifyExecution( base = root, graph = executionFlow( base ) ) {
	const manifest = JSON.parse( fs.readFileSync( path.join( base, "src/engine/ownership.json" ), "utf8" ) );
	const contract = JSON.parse( fs.readFileSync( path.join( base, "execution-contract.json" ), "utf8" ) );
	const defs = new Map( graph.functions.map( f => [ f.id, f ] ) );
	const callsByCaller = new Map();
	for ( const call of graph.calls ) {
		const list = callsByCaller.get( call.caller );
		if ( list ) list.push( call );
		else callsByCaller.set( call.caller, [ call ] );
	}
	const barriers = contract.frameBarriers ?? [], issues = [];
	verifyBarriers( base, barriers, issues );
	verifyCalls( graph, defs, manifest, contract, issues );
	verifySequences( graph, defs, callsByCaller, contract, issues );
	verifyFrameClosure( graph, defs, callsByCaller, barriers, issues );
	return issues;
}

if ( process.argv[1] === import.meta.filename ) main( () => verifyExecution() );
