/*
===========================================================================

graph.mjs - run a set of actions in dependency order, from the cache

runGraph keys each action once its dependencies have finished (an
action's inputs may be their outputs, so its key is known only then),
then either lays its recorded outputs down (a hit) or runs it and stores
what it wrote (a miss). Independent actions run at once, bounded by one
shared budget (SRO_BUILD_JOBS through buildParallelism.mjs).

Two actions with the same key in one build run once: the second waits
for the first and materializes the record it stored. After a failure no
new action starts; the ones already running finish, and the call rejects
with the first error. Nothing is recorded for a failed action, so the
next build runs it again, and everything downstream of it.

Init and shutdown are the store's (openStore / shutdown); a graph run is
one call.

===========================================================================
*/
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { buildJobs } from "../shared/buildParallelism.mjs";
import { createLimiter } from "../shared/asyncUtils.mjs";
import { defineAction, outputPath } from "./action.mjs";
import { dropRecord, keyOf, materialize, readRecord, storeOutputs } from "./store.mjs";
import { trackReads } from "./reads.mjs";

/*
================
orderActions

The actions by name, after checking the graph: a duplicate name, two
actions declaring one output file, a dependency on an unknown action and
a cycle are refused before anything runs. Two writers of one file would
run at once and the record of either could hold the other's bytes.
================
*/
export function orderActions( specs ) {
	const byName = new Map();
	const writerOf = new Map();
	for ( const spec of specs ) {
		const action = defineAction( spec );
		if ( byName.has( action.name ) ) throw new Error( `two actions are named ${action.name}` );
		byName.set( action.name, action );
		for ( const name of action.outputs ) {
			// Case-folded: the build runs on case-insensitive file systems.
			const file = outputPath( action, name ).toLowerCase();
			const other = writerOf.get( file );
			if ( other ) {
				throw new Error( `actions ${other} and ${action.name} both write ${outputPath( action, name )}` );
			}
			writerOf.set( file, action.name );
		}
	}
	for ( const action of byName.values() ) {
		for ( const dep of action.deps ) {
			if ( !byName.has( dep ) ) throw new Error( `action ${action.name} depends on unknown action ${dep}` );
		}
	}
	const state = new Map();
	const visit = ( action, trail ) => {
		if ( state.get( action.name ) === "done" ) return;
		if ( state.get( action.name ) === "visiting" ) {
			throw new Error( `action dependency cycle: ${[ ...trail, action.name ].join( " -> " )}` );
		}
		state.set( action.name, "visiting" );
		for ( const dep of action.deps ) visit( byName.get( dep ), [ ...trail, action.name ] );
		state.set( action.name, "done" );
	};
	for ( const action of byName.values() ) visit( action, [] );
	return byName;
}

/*
================
runGraph

Runs every action. options:
	store   an open store (store.mjs openStore)
	jobs    how many actions run at once (default buildJobs())
	env     the environment actions are keyed and run under
	log     receives one line per action
Returns { name: { key, hit, placed, result } }: result is what the run
returned, or on a hit what the run that stored the record returned.
================
*/
export async function runGraph( specs, options ) {
	const { store } = options;
	if ( !store ) throw new Error( "runGraph needs an open store" );
	const env = options.env ?? process.env;
	const log = options.log ?? (() => {});
	const byName = orderActions( specs );
	const limit = createLimiter( options.jobs ?? buildJobs() );
	const started = new Map();
	const byKey = new Map();
	const results = {};
	let failure;

	const runOne = async ( action ) => {
		const key = await keyOf( store, action, env );
		// The same key twice in one build: the second waits on the first.
		const earlier = byKey.get( key );
		if ( earlier ) {
			await earlier;
			return reuse( action, key );
		}
		const done = (async () => {
			const record = await readRecord( store, key, action );
			if ( record ) {
				return { hit: true, placed: await materialize( store, action, record ), result: record.result };
			}
			await dropRecord( store, key );
			// The action writes its declared outputs; their folders are the graph's.
			for ( const name of action.outputs ) {
				await mkdir( path.dirname( outputPath( action, name ) ), { recursive: true } );
			}
			// A step that finds its inputs as it runs has them recorded
			// (reads.mjs); any other step's reads are its declared inputs.
			const context = { action, key, env, outputRoot: action.outputRoot };
			const { reads, result } = action.discoverRoots ?
				await trackReads( () => action.run( context ) ) :
				{ reads: undefined, result: await action.run( context ) };
			await storeOutputs( store, action, key, { reads, result } );
			return { hit: false, placed: action.outputs.length, result };
		})();
		byKey.set( key, done );
		const outcome = await done;
		return { key, ...outcome };
	};

	const reuse = async ( action, key ) => {
		const record = await readRecord( store, key, action );
		if ( !record ) throw new Error( `action ${action.name}: shares key ${key} with an action that stored nothing` );
		return { key, hit: true, placed: await materialize( store, action, record ), result: record.result };
	};

	const start = ( action ) => {
		if ( started.has( action.name ) ) return started.get( action.name );
		const promise = (async () => {
			await Promise.all( action.deps.map( dep => start( byName.get( dep ) ) ) );
			return limit( async () => {
				// After a failure nothing new starts.
				if ( failure ) throw failure;
				try {
					const result = await runOne( action );
					log( `[graph] ${action.name}: ${result.hit ? "hit" : "ran"} (${result.placed} placed)` );
					results[action.name] = result;
					return result;
				} catch ( error ) {
					failure ??= error;
					throw error;
				}
			} );
		})();
		// A rejection is reported once, below; keep it from going unhandled.
		promise.catch( () => {} );
		started.set( action.name, promise );
		return promise;
	};

	await Promise.allSettled( [ ...byName.values() ].map( start ) );
	if ( failure ) throw failure;
	return results;
}
