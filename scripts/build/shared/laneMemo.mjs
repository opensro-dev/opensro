/*
===========================================================================

laneMemo.mjs - skip a resource-build lane whose inputs did not change

The resource build fans out into lanes whose filesystem inputs and outputs
are disjoint (resourceBuild.mjs). When the whole-build fingerprint misses,
one change usually concerns one lane, yet every lane ran again. A lane
memo records, after a successful build, what a lane's run depended on and
what it produced; the next build replays a lane whose record still holds
instead of running it.

A lane runs inside a complete read scope (graph/reads.mjs), and its
record holds what it read:
- reads: every file it opened, read or probed and every directory it
  listed, each with its digest (a file's sha256 through the memo's own
  file hash cache, a listing's names, MISSING_DIGEST for an absent one),
  taken after the build so they describe the tree the next build finds;
- roots: the roots only it reads (the NPC lane's server roster sources);
- code: the closure (JS imports and the Python they run, codeStamp.mjs) of
  each module that holds one of its steps, and resourceBuild.mjs (entry);
- environment: the SRO_* variables, less the knobs that only change speed.
A change to one data table then reruns only the lanes that read it: a
whole-root key reran all three lanes for any variant import (r204n,
Media_extracted/server_dep, #4337). A lane that starts a child process
or a worker reads what the scope cannot see; it is keyed instead on
everything it may read:
- data: the data roots (extracted retail data, the client executable, the
  particle archive, converted images), hashed after the start-up steps;
- startup: the stat signature of the files the start-up steps produced.
A record written before reads were recorded holds those two as well, and
replays by them until the lane next runs.
A replay also requires every file the lane claimed to still have the size
and mtime it had when recorded, so an output edited or deleted since is
rebuilt. The replay re-claims those files for the publication and returns
the recorded result. A lane that runs says which part changed.

Records are written only after the whole build succeeds (commit). A lane
whose result is not plain data (Map, Set, bytes and plain objects only) is
not recorded.

===========================================================================
*/
import { createHash } from "node:crypto";
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildStatePath, CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import { MISSING_DIGEST } from "../graph/action.mjs";
import { LISTED, trackReads } from "../graph/reads.mjs";
import { mapWithConcurrency } from "./asyncUtils.mjs";
import { codeHash } from "./codeStamp.mjs";
import { openFileHashCache } from "./fileHashCache.mjs";
import { claimPublicPaths, recordClaims } from "./publicationLedger.mjs";
import { writeStateJson } from "./stateFile.mjs";
import { buildKnobLines, hashRoots } from "./resourceBuildFingerprint.mjs";
import { rebuildRoot } from "../world/paths.mjs";

// v4: the knobs are buildKnobLines, the effective tomb posture included.
const MEMO_VERSION = 4;
const MEMO_ROOT = buildStatePath( "lane-memo" );
const STAT_CONCURRENCY = 64;
const KEY_PARTS = [ "startup", "environment", "entry", "roots" ];
// The parts a read-keyed record compares: its reads stand in for the
// data roots and the start-up outputs.
const READ_KEY_PARTS = [ "environment", "entry", "roots" ];
// A read path that names a directory rather than a file.
const DIRECTORY_DIGEST = "dir";
const HASH_CACHE_NAME = "file-hash-cache.json";

/*
================
environmentHash

The variables that change what the build writes: the whole-build
fingerprint's own knob lines (buildKnobLines: the raw knobs and the
postures derived from them). Every other SRO_* variable is either speed
only or per run (the rebuild lock's token).
================
*/
function environmentHash() {
	const hash = createHash( "sha256" );
	for ( const line of buildKnobLines() ) hash.update( `${line}\n` );
	return hash.digest( "hex" );
}

/*
================
outputSignatures

[publicPath, size, mtimeMs] for each claimed file, sorted; a missing file
is recorded as size -1 so its later appearance misses.
================
*/
export async function outputSignatures( publicPaths, publicRoot = CLIENT_PUBLIC_ROOT ) {
	const sorted = [ ...publicPaths ].sort();
	return mapWithConcurrency( sorted, STAT_CONCURRENCY, async ( publicPath ) => {
		try {
			const info = await stat( path.join( publicRoot, publicPath.replace( /^\/+/, "" ) ) );
			return [ publicPath, info.size, info.mtimeMs ];
		} catch ( error ) {
			if ( error?.code !== "ENOENT" ) throw error;
			return [ publicPath, -1, 0 ];
		}
	} );
}

/*
================
signatureHash
================
*/
export function signatureHash( signatures ) {
	const hash = createHash( "sha256" );
	for ( const row of signatures ) hash.update( `${row.join( "|" )}\n` );
	return hash.digest( "hex" );
}

/*
================
encodeResult

A lane result as JSON, or undefined when it holds anything but plain data.
Maps, Sets and byte arrays are tagged so decodeResult restores them.
================
*/
export function encodeResult( value ) {
	try {
		return JSON.stringify( toPlain( value ) );
	} catch {
		return undefined;
	}
}

/*
================
toPlain
================
*/
function toPlain( value ) {
	if ( value === null || typeof value !== "object" ) {
		if ( typeof value === "function" || typeof value === "symbol" || typeof value === "bigint" ) {
			throw new Error( "not plain data" );
		}
		return value;
	}
	if ( value instanceof Map ) {
		return { $map: [ ...value ].map( ( [key, item] ) => [ toPlain( key ), toPlain( item ) ] ) };
	}
	if ( value instanceof Set ) return { $set: [ ...value ].map( toPlain ) };
	if ( value instanceof Uint8Array ) return { $bytes: Buffer.from( value ).toString( "base64" ) };
	if ( Array.isArray( value ) ) return value.map( toPlain );
	const prototype = Object.getPrototypeOf( value );
	if ( prototype !== Object.prototype && prototype !== null ) throw new Error( "not plain data" );
	const plain = {};
	for ( const [key, item] of Object.entries( value ) ) {
		if ( item !== undefined ) plain[key] = toPlain( item );
	}
	return plain;
}

/*
================
decodeResult
================
*/
export function decodeResult( text ) {
	return JSON.parse( text, ( key, value ) => {
		if ( value && typeof value === "object" && !Array.isArray( value ) ) {
			if ( "$map" in value ) return new Map( value.$map );
			if ( "$set" in value ) return new Set( value.$set );
			if ( "$bytes" in value ) return Buffer.from( value.$bytes, "base64" );
		}
		return value;
	} );
}

/*
================
readDigest

The digest of one recorded read: a listing's sorted names, a file's
sha256, DIRECTORY_DIGEST for a directory probed, MISSING_DIGEST for an
absent path.
================
*/
async function readDigest( entry, hashCache ) {
	try {
		if ( entry.startsWith( LISTED ) ) {
			const names = (await readdir( entry.slice( LISTED.length ) )).sort();
			return createHash( "sha256" ).update( names.join( "\n" ) ).digest( "hex" );
		}
		const info = await stat( entry );
		if ( info.isDirectory() ) return DIRECTORY_DIGEST;
		return await hashCache.hashFile( entry, info );
	} catch ( error ) {
		if ( error?.code === "ENOENT" || error?.code === "ENOTDIR" ) return MISSING_DIGEST;
		throw error;
	}
}

/*
================
readDigests

[entry, digest] for each read, in code-unit order.
================
*/
async function readDigests( reads, hashCache ) {
	const sorted = [ ...reads ].sort();
	return mapWithConcurrency(
		sorted,
		STAT_CONCURRENCY,
		async entry => [ entry, await readDigest( entry, hashCache ) ]
	);
}

/*
================
movedRead

The first recorded read whose digest is not its recorded one, or
undefined when they all hold.
================
*/
async function movedRead( recorded, hashCache ) {
	const now = await mapWithConcurrency( recorded, STAT_CONCURRENCY, ( [entry] ) => readDigest( entry, hashCache ) );
	const index = now.findIndex( ( digest, at ) => digest !== recorded[at][1] );
	return index < 0 ? undefined : recorded[index][0];
}

/*
================
coveredFolderKey

True when a data key the record lacks is a per-folder key ("<label>/<name>",
resourceBuildFingerprint folderHashes) whose root hash the record holds:
that root hash covers the folder's files already, so the key's first
appearance is instrumentation, not a change (#4337). Any other missing key,
a new data root above all, is a change.
================
*/
function coveredFolderKey( key, recordedData ) {
	const slash = key.indexOf( "/" );
	return slash > 0 && key.slice( 0, slash ) in recordedData;
}

/*
================
changedParts

The names of the record parts that differ from this build's, so a lane
that runs says why.
================
*/
async function changedParts( recorded, current, hashCache ) {
	if ( !recorded ) return [ "record format" ];
	if ( Array.isArray( recorded.reads ) ) {
		const changed = READ_KEY_PARTS.filter( name => recorded[name] !== current[name] );
		for ( const file of new Set( [ ...Object.keys( recorded.code ?? {} ), ...Object.keys( current.code ) ] ) ) {
			if ( recorded.code?.[file] !== current.code[file] ) changed.push( `code ${file}` );
		}
		if ( changed.length > 0 ) return changed;
		const moved = await movedRead( recorded.reads, hashCache );
		return moved === undefined ? [] : [ `read ${moved}` ];
	}
	const changed = KEY_PARTS.filter( name => recorded[name] !== current[name] );
	const recordedData = recorded.data ?? {};
	// Keys the record holds still name every move and deletion.
	for ( const root of Object.keys( current.data ) ) {
		if (
			root in recordedData ?
				recordedData[root] !== current.data[root] :
				!coveredFolderKey( root, recordedData )
		) {
			changed.push( `data ${root}` );
		}
	}
	for ( const root of Object.keys( recordedData ) ) {
		if ( !(root in current.data) ) changed.push( `data ${root}` );
	}
	for ( const file of new Set( [ ...Object.keys( recorded.code ?? {} ), ...Object.keys( current.code ) ] ) ) {
		if ( recorded.code?.[file] !== current.code[file] ) changed.push( `code ${file}` );
	}
	return changed;
}

/*
================
rootsPart

The roots key part: the lane's own roots and its computed key. A lane
with neither keeps the empty part older records hold.
================
*/
async function rootsPart( roots, key ) {
	const hashed = roots.length > 0 ? await hashRoots( roots ) : "";
	return key ?
		`${hashed}\nkey ${await key()}` :
		hashed;
}

/*
================
createLaneMemo

options: upstream ({ data, startup } hashes of everything before the
lanes), force (run every lane), entry (resourceBuild.mjs, whose own text
joins every record), log, and for tests publicRoot and memoRoot.
================
*/
export function createLaneMemo( {
	upstream,
	force = false,
	entry,
	log = console.log,
	publicRoot = CLIENT_PUBLIC_ROOT,
	memoRoot = MEMO_ROOT
} ) {
	const pending = new Map();
	const environment = environmentHash();
	let entryHash, hashCache;

	/*
	================
	fileHashes

	The memo's own file hash cache: a cache saves whole, so it shares no
	file with another owner (store.mjs openStore).
	================
	*/
	async function fileHashes() {
		hashCache ??= await openFileHashCache( path.join( memoRoot, HASH_CACHE_NAME ) );
		return hashCache;
	}

	/*
	================
	laneParts
	================
	*/
	async function laneParts( { modules, roots = [], key } ) {
		entryHash ??= createHash( "sha256" ).update( await readFile( entry ) ).digest( "hex" );
		const code = {};
		for ( const module of modules ) {
			const file = String( module ).startsWith( "file:" ) ? fileURLToPath( module ) : module;
			code[path.relative( rebuildRoot, file ).split( path.sep ).join( "/" )] = await codeHash( file );
		}
		return {
			data: upstream.data,
			startup: upstream.startup,
			environment,
			entry: entryHash,
			roots: await rootsPart( roots, key ),
			code
		};
	}

	/*
	================
	replay

	The recorded result when the record's parts match and its outputs are
	untouched, else undefined.
	================
	*/
	async function replay( name, parts ) {
		let record;
		try {
			record = JSON.parse( await readFile( path.join( memoRoot, `${name}.json` ), "utf8" ) );
		} catch {
			return undefined;
		}
		if ( record?.version !== MEMO_VERSION ) return undefined;
		const changed = await changedParts( record.parts, parts, await fileHashes() );
		if ( changed.length > 0 ) {
			log( `[resource-build] lane ${name}: runs (${changed.join( ", " )} changed)` );
			return undefined;
		}
		const claims = record.outputs.map( row => row[0] );
		const now = await outputSignatures( claims, publicRoot );
		const moved = now.find( ( row, index ) => row.join( "|" ) !== record.outputs[index].join( "|" ) );
		if ( moved ) {
			log( `[resource-build] lane ${name}: runs (output ${moved[0]} changed)` );
			return undefined;
		}
		// Data keys the record predates (the per-folder keys #4337 added
		// beside each root hash) are instrumentation, not a change:
		// changedParts already proved every shared key equal, so the
		// record learns the new keys here, on the replay it earned.
		// Without this the first build carrying folder keys reruns every
		// lane and names folders that never moved.
		const dataKeyed = !Array.isArray( record.parts.reads );
		if ( dataKeyed && Object.keys( parts.data ?? {} ).some( key => !(key in (record.parts.data ?? {})) ) ) {
			record.parts.data = parts.data;
			try {
				await writeStateJson( path.join( memoRoot, `${name}.json` ), record );
			} catch {
				// Bookkeeping only: a later build learns the keys instead.
			}
		}
		claimPublicPaths( claims );
		return decodeResult( record.result );
	}

	return {
		/*
		================
		lane

		Runs the lane, or replays its record. inputs.modules are the files (URL
		or path) that hold the lane's steps; inputs.roots are fingerprint roots
		the lane alone reads beyond the shared upstream; inputs.key, when set,
		digests an input that is no file tree (the npc lane's server rosters).
		================
		*/
		async lane( name, inputs, run ) {
			const parts = await laneParts( inputs );
			if ( !force ) {
				const replayed = await replay( name, parts );
				if ( replayed !== undefined ) {
					log( `[resource-build] lane ${name}: unchanged, replayed` );
					return replayed;
				}
			}
			const claims = new Set();
			const { result, reads, opaque } = await trackReads( () => recordClaims( claims, run ), { complete: true } );
			const encoded = encodeResult( result );
			if ( encoded === undefined ) {
				log( `[resource-build] lane ${name}: result is not plain data; not recorded` );
			} else {
				if ( opaque ) log( `[resource-build] lane ${name}: keyed on the data roots (it started a ${opaque})` );
				pending.set( name, { parts, claims, result: encoded, reads: opaque ? undefined : reads } );
			}
			return result;
		},

		/*
		================
		commit

		Writes the records of the lanes that ran, once the build succeeded.
		The output signatures are taken now, after the pack tail, so they
		describe the files as the next build will find them.
		================
		*/
		async commit() {
			for ( const [name, record] of pending ) {
				const file = path.join( memoRoot, `${name}.json` );
				// A read-keyed record drops the coarse parts its reads replace.
				const { data, startup, ...rest } = record.parts;
				const parts = record.reads ?
					{ ...rest, reads: await readDigests( record.reads, await fileHashes() ) } :
					record.parts;
				const payload = {
					version: MEMO_VERSION,
					parts,
					outputs: await outputSignatures( record.claims, publicRoot ),
					result: record.result
				};
				await writeStateJson( file, payload );
			}
			pending.clear();
			await hashCache?.save();
		}
	};
}
