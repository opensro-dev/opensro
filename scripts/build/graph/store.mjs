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
import { actionKey, outputPath } from "./action.mjs";

export const RECORD_FORMAT = "sro-action-record";
export const RECORD_VERSION = 1;
export const DEFAULT_STORE_ROOT = buildStatePath( "actions" );
const FAN_OUT_CHARS = 2;

/*
================
openStore

Init for the cache. root defaults to the shared build state; hashCache
defaults to the file hash cache beside it (tests pass their own). Call
shutdown() once the build is done: it saves the hash cache.
================
*/
export async function openStore( options = {} ) {
	const root = path.resolve( options.root ?? DEFAULT_STORE_ROOT );
	const hashCache = options.hashCache ?? (await openFileHashCache());
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
================
*/
export async function readRecord( store, key ) {
	const record = await readJsonOrUndefined( recordPath( store, key ) );
	if ( record?.format !== RECORD_FORMAT || record.version !== RECORD_VERSION || record.key !== key ) return undefined;
	if ( !Array.isArray( record.outputs ) ) return undefined;
	for ( const output of record.outputs ) {
		const info = await statOrUndefined( objectPath( store, output.sha256 ) );
		if ( !info || info.size !== output.size || info.mtimeMs !== output.objectMtimeMs ) return undefined;
	}
	return record;
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
================
*/
export async function storeOutputs( store, action, key, outputRoot = action.outputRoot ) {
	const outputs = [];
	for ( const name of action.outputs ) {
		const file = outputPath( action, name, outputRoot );
		const info = await statOrUndefined( file );
		if ( !info?.isFile() ) throw new Error( `action ${action.name}: declared output ${name} was not written` );
		const sha256 = await sha256File( file );
		const object = await putObject( store, file, sha256 );
		outputs.push( { name, sha256, size: object.size, objectMtimeMs: object.mtimeMs } );
	}
	const record = { format: RECORD_FORMAT, version: RECORD_VERSION, name: action.name, key, outputs };
	await writeStateJson( recordPath( store, key ), record );
	return record;
}

/*
================
putObject

One output's bytes under their digest. An object already stored is kept
(it holds the same bytes by name); a new one is copied to a temp name
beside it and renamed into place. Returns the object's stat.
================
*/
async function putObject( store, file, sha256 ) {
	const target = objectPath( store, sha256 );
	const existing = await statOrUndefined( target );
	if ( existing ) return existing;
	await mkdir( path.dirname( target ), { recursive: true } );
	const temporary = `${target}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
	try {
		await copyFile( file, temporary, constants.COPYFILE_FICLONE );
		try {
			await rename( temporary, target );
		} catch ( error ) {
			// Another writer published the same digest first: its file stands.
			if ( !(await statOrUndefined( target )) ) throw error;
		}
	} finally {
		await rm( temporary, { force: true } );
	}
	return stat( target );
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
