/*
===========================================================================

laneMemo.mjs - skip a resource-build lane whose inputs did not change

The resource build fans out into lanes whose filesystem inputs and outputs
are disjoint (resourceBuild.mjs). When the whole-build fingerprint misses,
one change usually concerns one lane, yet every lane ran again. A lane
memo records, after a successful build, what a lane's run depended on and
what it produced; the next build replays a lane whose record still holds
instead of running it.

A lane's record holds everything the lane may read:
- data: the data roots (extracted retail data, the client executable, the
  particle archive, converted images), hashed after the start-up steps;
- startup: the stat signature of the files the start-up steps produced;
- roots: the roots only it reads (the NPC lane's server roster sources);
- code: the closure (JS imports and the Python they run, codeStamp.mjs) of
  each module that holds one of its steps, and resourceBuild.mjs (entry);
- environment: the SRO_* variables, less the knobs that only change speed.
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
import { mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import { mapWithConcurrency } from "./asyncUtils.mjs";
import { codeHash } from "./codeStamp.mjs";
import { claimPublicPaths, recordClaims } from "./publicationLedger.mjs";
import { ENV_KNOBS, hashRoots } from "./resourceBuildFingerprint.mjs";
import { rebuildRoot } from "../world/paths.mjs";

const MEMO_VERSION = 3;
const MEMO_ROOT = path.join( rebuildRoot, ".state", "lane-memo" );
const STAT_CONCURRENCY = 64;
const KEY_PARTS = [ "startup", "environment", "entry", "roots" ];

/*
================
environmentHash

The variables that change what the build writes: the whole-build
fingerprint's own list (ENV_KNOBS). Every other SRO_* variable is either
speed only or per run (the rebuild lock's token).
================
*/
function environmentHash() {
	const hash = createHash( "sha256" );
	for ( const name of ENV_KNOBS ) hash.update( `${name}=${process.env[name] ?? ""}\n` );
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
changedParts

The names of the record parts that differ from this build's, so a lane
that runs says why.
================
*/
function changedParts( recorded, current ) {
	if ( !recorded ) return [ "record format" ];
	const changed = KEY_PARTS.filter( name => recorded[name] !== current[name] );
	for ( const root of new Set( [ ...Object.keys( recorded.data ?? {} ), ...Object.keys( current.data ) ] ) ) {
		if ( recorded.data?.[root] !== current.data[root] ) changed.push( `data ${root}` );
	}
	for ( const file of new Set( [ ...Object.keys( recorded.code ?? {} ), ...Object.keys( current.code ) ] ) ) {
		if ( recorded.code?.[file] !== current.code[file] ) changed.push( `code ${file}` );
	}
	return changed;
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
	let entryHash;

	/*
	================
	laneParts
	================
	*/
	async function laneParts( { modules, roots = [] } ) {
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
			roots: roots.length > 0 ? await hashRoots( roots ) : "",
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
		const changed = changedParts( record.parts, parts );
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
		claimPublicPaths( claims );
		return decodeResult( record.result );
	}

	return {
		/*
		================
		lane

		Runs the lane, or replays its record. inputs.modules are the files (URL
		or path) that hold the lane's steps; inputs.roots are fingerprint roots
		the lane alone reads beyond the shared upstream.
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
			const result = await recordClaims( claims, run );
			const encoded = encodeResult( result );
			if ( encoded === undefined ) {
				log( `[resource-build] lane ${name}: result is not plain data; not recorded` );
			} else {
				pending.set( name, { parts, claims, result: encoded } );
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
			await mkdir( memoRoot, { recursive: true } );
			for ( const [name, record] of pending ) {
				const file = path.join( memoRoot, `${name}.json` );
				const payload = {
					version: MEMO_VERSION,
					parts: record.parts,
					outputs: await outputSignatures( record.claims, publicRoot ),
					result: record.result
				};
				await writeFile( `${file}.tmp`, JSON.stringify( payload ) );
				await rename( `${file}.tmp`, file );
			}
			pending.clear();
		}
	};
}
