/*
===========================================================================

store.mjs - the action cache: records by key, outputs by content

	<root>/records/<kk>/<key>.json   what an action with that key wrote
	<root>/objects/<ss>/<sha256>     each output's bytes, once

The root is the build state beside the generated tree (generatedRoot.mjs
buildStatePath), so every worktree that shares the tree shares the cache:
a fresh release worktree finds what the last build stored.

A record is published only after every output it names is in the object
store, so a reader that finds a record finds its bytes. Both are written
through a temp name and a rename (stateFile.mjs for records, the same rule
for objects), and an object's name is its content, so two builds storing
the same action at once each publish complete, identical files and the
cache needs no lock of its own. (The plan's per-action rebuildLock would
also not exclude another worktree: only the generated-assets lock lives
beside the shared tree; every other lock lives in its own checkout.)

Materialization copies by default. A hard link is faster, but a tool that
later writes an output in place through the link would rewrite the stored
object, and every other path linked to it, without a trace; read-only
objects do not help on Windows, where they make the tools' own rm fail.
An action whose outputs are only ever replaced whole (rename, rm then
write) asks for links (action.link). Either way a target that already
holds the recorded bytes is left alone, and an object whose size or time
moved since it was stored (an in-place write through a link) is not
trusted: the record is dropped and the action runs again.

===========================================================================
*/
import { constants } from "node:fs";
import { copyFile, link, mkdir, readdir, rename, rm, stat } from "node:fs/promises";
import path from "node:path";
import { buildStatePath } from "../../lib/generatedRoot.mjs";
import { codeHash } from "../shared/codeStamp.mjs";
import { openFileHashCache } from "../shared/fileHashCache.mjs";
import { sha256File } from "../shared/hash.mjs";
import { readJsonOrUndefined } from "../shared/jsonOut.mjs";
import { writeStateJson } from "../shared/stateFile.mjs";
import { actionKey, discoveredName, MISSING_DIGEST, outputPath } from "./action.mjs";

export const RECORD_FORMAT = "sro-action-record";
export const RECORD_VERSION = 1;
export const DEFAULT_STORE_ROOT = buildStatePath( "actions" );
const FAN_OUT_CHARS = 2;
const HASH_CACHE_NAME = "file-hash-cache.json";

/*
================
openStore

Init for the cache. root defaults to the shared build state; hashCache
defaults to a file hash cache of the store's own (tests pass theirs). Not
the asset build's: a cache saves whole, so two owners of one file in one
build would each drop the other's entries. Call shutdown() once the build
is done: it saves the hash cache.
================
*/
export async function openStore( options = {} ) {
	const root = path.resolve( options.root ?? DEFAULT_STORE_ROOT );
	const hashCache = options.hashCache ?? (await openFileHashCache( path.join( root, HASH_CACHE_NAME ) ));
	const codeHashOf = options.codeHashOf ?? codeHash;
	const codeHashes = new Map();
	const store = {
		root,
		async hashFile( absolutePath ) {
			try {
				return await hashCache.hashFile( absolutePath );
			} catch ( error ) {
				if ( error?.code === "ENOENT" ) return undefined;
				throw error;
			}
		},
		codeHash( entry ) {
			// One closure walk per entry per build, however many actions share it.
			if ( !codeHashes.has( entry ) ) codeHashes.set( entry, codeHashOf( entry ) );
			return codeHashes.get( entry );
		},
		async shutdown() {
			await hashCache.save();
		}
	};
	return store;
}

/*
================
keyOf
================
*/
export function keyOf( store, action, env = process.env ) {
	return actionKey( action, {
		hashInput: file => store.hashFile( file ),
		codeHashOf: entry => store.codeHash( entry ),
		env
	} );
}

/*
================
recordPath / objectPath
================
*/
export function recordPath( store, key ) {
	return path.join( store.root, "records", key.slice( 0, FAN_OUT_CHARS ), `${key}.json` );
}

export function objectPath( store, sha256 ) {
	return path.join( store.root, "objects", sha256.slice( 0, FAN_OUT_CHARS ), sha256 );
}

/*
================
readRecord

The record for a key, or undefined when there is none, it is malformed,
or one of its objects is missing or moved since it was stored.

A record that carries discovered reads (reads.mjs) holds only while each
of them still has its recorded digest under action's discoverRoots: a
changed, deleted or newly present file is a miss. Without the action
such a record cannot be checked, and is not used. The digests come from
the file hash cache, so an unchanged tree costs a stat per read.
================
*/
export async function readRecord( store, key, action ) {
	const record = await readJsonOrUndefined( recordPath( store, key ) );
	if ( record?.format !== RECORD_FORMAT || record.version !== RECORD_VERSION || record.key !== key ) return undefined;
	if ( !Array.isArray( record.outputs ) ) return undefined;
	for ( const output of record.outputs ) {
		const info = await statOrUndefined( objectPath( store, output.sha256 ) );
		if ( !info || info.size !== output.size || info.mtimeMs !== output.objectMtimeMs ) return undefined;
	}
	if ( record.discovered !== undefined && !(await discoveredReadsHold( store, action, record.discovered )) ) {
		return undefined;
	}
	return record;
}

/*
================
discoveredReadsHold

True when every [root, name, digest] still names a file with that digest
(or a missing one for MISSING_DIGEST) under the action's roots.
================
*/
async function discoveredReadsHold( store, action, discovered ) {
	const roots = action?.discoverRoots;
	if ( !roots || !Array.isArray( discovered ) ) return false;
	for ( const [root, name, digest] of discovered ) {
		if ( !roots[root] ) return false;
		const file = path.join( roots[root], ...name.split( "/" ) );
		if ( ((await store.hashFile( file )) ?? MISSING_DIGEST) !== digest ) return false;
	}
	return true;
}

/*
================
dropRecord
================
*/
export async function dropRecord( store, key ) {
	await rm( recordPath( store, key ), { force: true } );
}

/*
================
storeOutputs

After a successful run: every declared output into the object store,
then the record. A declared output the run did not write fails the
action; nothing is recorded for it.

options:
	outputRoot  where the outputs were written (default the action's)
	reads       the files the run discovered (reads.mjs trackReads), for an
	            action that declares discoverRoots: each is recorded by
	            root, name and digest. A read outside every root fails the
	            action, as does a discovered read from an action without roots.
	result      a small JSON value the run returned (a model's clip list and
	            byte counts), kept in the record and handed back on a hit, so
	            a caller that builds a manifest from it need not rerun
================
*/
export async function storeOutputs( store, action, key, options = {} ) {
	const { outputRoot = action.outputRoot, reads, result } = options;
	const discovered = await discoveredDigests( store, action, reads );
	const outputs = [];
	for ( const name of action.outputs ) {
		const file = outputPath( action, name, outputRoot );
		const info = await statOrUndefined( file );
		if ( !info?.isFile() ) throw new Error( `action ${action.name}: declared output ${name} was not written` );
		const sha256 = await sha256File( file );
		const object = await putObject( store, file, sha256, action.link );
		outputs.push( { name, sha256, size: object.size, objectMtimeMs: object.mtimeMs } );
	}
	const record = {
		format: RECORD_FORMAT,
		version: RECORD_VERSION,
		name: action.name,
		key,
		outputs,
		...(discovered ? { discovered } : {}),
		...(result === undefined ? {} : { result })
	};
	await writeStateJson( recordPath( store, key ), record );
	return record;
}

/*
================
discoveredDigests

The record's discovered reads, sorted by root and name; undefined for an
action that declares no roots.
================
*/
async function discoveredDigests( store, action, reads ) {
	if ( !action.discoverRoots ) {
		if ( reads && reads.size > 0 ) {
			throw new Error( `action ${action.name}: discovered reads without discoverRoots` );
		}
		return undefined;
	}
	const named = [];
	for ( const file of reads ?? [] ) {
		const at = discoveredName( action, file );
		if ( !at ) throw new Error( `action ${action.name}: read ${file} lies outside its discoverRoots` );
		named.push( at );
	}
	// Root, then name, in code-unit order: the record is the same bytes on every host.
	named.sort( ( a, b ) => a[0] !== b[0] ? (a[0] < b[0] ? -1 : 1) : a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : 0 );
	const discovered = [];
	for ( const [root, name] of named ) {
		const file = path.join( action.discoverRoots[root], ...name.split( "/" ) );
		discovered.push( [ root, name, (await store.hashFile( file )) ?? MISSING_DIGEST ] );
	}
	return discovered;
}

/*
================
putObject

One output's bytes under their digest. An object already stored is kept
(it holds the same bytes by name); a new one is placed at a temp name
beside it (a hard link for an action that asked for links, so the output
and the store share one copy) and published exclusively (publishExclusive).
Returns the object's stat.
================
*/
async function putObject( store, file, sha256, wantLink ) {
	const target = objectPath( store, sha256 );
	const existing = await statOrUndefined( target );
	if ( existing ) {
		// A stored object is trusted only while it still holds the bytes it
		// is named for: an in-place write through a hard-linked output
		// rewrites it, and recording it as it stands would carry the damage
		// into a fresh record (#3652 review). The hash cache answers from the
		// stat while nothing moved; a moved object is re-read.
		if ( (await store.hashFile( target )) === sha256 ) return existing;
		await rm( target, { force: true } );
	}
	await mkdir( path.dirname( target ), { recursive: true } );
	const temporary = `${target}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
	try {
		await placeObject( file, temporary, wantLink );
		await publishExclusive( temporary, target );
	} finally {
		await rm( temporary, { force: true } );
	}
	return stat( target );
}

/*
================
publishExclusive

Names temporary as target unless target exists; the first writer's file
stands. Not a rename: on Windows a rename replaces an existing file, so
two builds storing the same bytes at once (two images that convert to one
PNG) would swap the object under a record that just stated it, and the
record would read as moved (the cold equivalence run lost 48 of 18,221
that way). A hard link is exclusive (EEXIST); a volume that refuses links
falls back to a rename only while the target is absent.
================
*/
async function publishExclusive( temporary, target ) {
	try {
		await link( temporary, target );
		return;
	} catch ( error ) {
		if ( error?.code === "EEXIST" ) return;
		if ( ![ "EXDEV", "EPERM", "ENOTSUP", "EMLINK" ].includes( error?.code ) ) throw error;
	}
	if ( await statOrUndefined( target ) ) return;
	await rename( temporary, target );
}

/*
================
materialize

Lays a record's outputs under outputRoot. A target that already holds the
recorded bytes is left alone (the file hash cache answers from its stat
when nothing moved); any other is replaced whole through a temp name.
Returns how many files were laid down.
================
*/
export async function materialize( store, action, record, outputRoot = action.outputRoot ) {
	let placed = 0;
	for ( const output of record.outputs ) {
		const target = outputPath( action, output.name, outputRoot );
		if ( (await store.hashFile( target )) === output.sha256 ) continue;
		await mkdir( path.dirname( target ), { recursive: true } );
		const temporary = `${target}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
		try {
			await placeObject( objectPath( store, output.sha256 ), temporary, action.link );
			await rm( target, { force: true } );
			await rename( temporary, target );
		} finally {
			await rm( temporary, { force: true } );
		}
		placed += 1;
	}
	return placed;
}

/*
================
placeObject

A hard link when the action asked for one and the volume allows it, else
a copy (a clone where the filesystem supports one).
================
*/
async function placeObject( object, target, wantLink ) {
	if ( wantLink ) {
		try {
			await link( object, target );
			return;
		} catch ( error ) {
			if ( ![ "EXDEV", "EPERM", "ENOTSUP", "EMLINK" ].includes( error?.code ) ) throw error;
		}
	}
	await copyFile( object, target, constants.COPYFILE_FICLONE );
}

/*
================
storeSize

The object and record counts, for the build's summary line.
================
*/
export async function storeSize( store ) {
	const count = async ( folder ) => {
		try {
			return (await readdir( path.join( store.root, folder ), { recursive: true, withFileTypes: true } ))
				.filter( entry => entry.isFile() ).length;
		} catch ( error ) {
			if ( error?.code === "ENOENT" ) return 0;
			throw error;
		}
	};
	return { objects: await count( "objects" ), records: await count( "records" ) };
}

/*
================
statOrUndefined
================
*/
async function statOrUndefined( file ) {
	try {
		return await stat( file );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	}
}
