/*
===========================================================================

run-tests.mjs - the client test run, reusing passes whose inputs did not change

	node tools/run-tests.mjs <dir|file>...

Every test file runs with tools/lib/test-input-recorder.mjs preloaded, which
logs each file, module and directory the test (and its threads and Node
children) touched. A passing file is recorded with the content hash of each
of those inputs. On the next run a file whose every input still hashes the
same is not run again: nothing it can observe has changed. A failing file is
never recorded, and a file that starts a non-Node program is never cached.

Hashes are memoised by size and modification time, so validating an
unchanged tree is a stat sweep. The cache lives in .state/client-test-cache
and is keyed by the Node version, platform and test environment; any change
there discards it. SRO_CHECK_FORCE=1 runs everything.

===========================================================================
*/
import { run } from "node:test";
import { spec } from "node:test/reporters";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { logPathFor } from "./lib/test-input-recorder.mjs";

const CACHE_FORMAT = 1;
const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const repoRoot = path.resolve( clientRoot, "../.." );
const cacheDir = process.env.SRO_TEST_CACHE_DIR ?? path.join( repoRoot, ".state", "client-test-cache" );
const indexPath = path.join( cacheDir, "index.json" );
const recorderUrl = pathToFileURL( path.join( clientRoot, "tools/lib/test-input-recorder.mjs" ) ).href;
// The environment a test can see beyond its files.
const ENV_KEYS = [ "TZ", "LANG", "NODE_ENV" ];
// Parallel stats: more pool threads than the default four, and enough
// requests in flight to keep them busy. Set before the pool first starts.
const PREFETCH_WORKERS = 64;
process.env.UV_THREADPOOL_SIZE ??= String( Math.min( 64, Math.max( 4, os.availableParallelism() ) ) );

/*
================
testFiles
================
*/
function testFiles( targets ) {
	const files = [];
	for ( const target of targets ) {
		const full = path.resolve( clientRoot, target );
		if ( fs.statSync( full ).isDirectory() ) {
			for ( const name of fs.readdirSync( full ).sort() ) {
				if ( name.endsWith( ".test.mjs" ) ) files.push( path.join( full, name ) );
			}
		} else files.push( full );
	}
	return files;
}

/*
================
cacheKey
================
*/
function cacheKey() {
	const env = Object.fromEntries(
		// Runner switches and inherited lock ownership are not test inputs.
		// A fresh lock token must not discard every unchanged test pass.
		Object.keys( process.env ).filter( key =>
			key.startsWith( "SRO_" ) && key !== "SRO_CHECK_FORCE" && !key.startsWith( "SRO_TEST_" ) &&
				!key.startsWith( "SRO_REBUILD_LOCK_" ) ||
			ENV_KEYS.includes( key )
		)
			.sort().map( key => [ key, process.env[key] ] )
	);
	return JSON.stringify( {
		format: CACHE_FORMAT,
		node: process.version,
		platform: process.platform,
		arch: process.arch,
		env
	} );
}

/*
================
readIndex
================
*/
function readIndex( key ) {
	const empty = { key, stats: {}, records: {} };
	if ( process.env.SRO_CHECK_FORCE === "1" ) return empty;
	try {
		const index = JSON.parse( fs.readFileSync( indexPath, "utf8" ) );
		return index.key === key ? index : empty;
	} catch {
		return empty;
	}
}

/*
================
createFingerprints

Content identity of one input, by kind. A file is its sha256, memoised
against its size and mtime; a listing is the sha256 of its sorted entries;
a probe is the file hash, "dir" or "missing". A read and a probe of the same
path share one result. prefetch() fills the memo for many inputs at once:
stats go to the libuv pool in parallel, which is what makes validating ten
thousand unchanged inputs cheap.
================
*/
function createFingerprints( index ) {
	const memo = new Map(), stats = index.stats;
	const slot = ( kind, file ) => (kind === "D" ? "D\t" : "F\t") + file;
	const listing = entries =>
		createHash( "sha256" ).update( entries.map( e => e.name + (e.isDirectory() ? "/" : "") ).sort().join( "\n" ) )
			.digest( "hex" );
	const known = ( file, stat ) => {
		const row = stats[file];
		return row && row[0] === stat.size && row[1] === stat.mtimeMs ? row[2] : undefined;
	};
	const remember = ( file, stat, bytes ) => {
		const sha = createHash( "sha256" ).update( bytes ).digest( "hex" );
		stats[file] = [ stat.size, stat.mtimeMs, sha ];
		return sha;
	};

	function fingerprint( kind, file ) {
		const key = slot( kind, file );
		if ( memo.has( key ) ) return memo.get( key );
		let value;
		try {
			const stat = fs.statSync( file );
			if ( kind === "D" ) {
				value = stat.isDirectory() ? listing( fs.readdirSync( file, { withFileTypes: true } ) ) : "not-dir";
			} else {
				value = stat.isDirectory() ?
					"dir" :
					known( file, stat ) ?? remember( file, stat, fs.readFileSync( file ) );
			}
		} catch {
			value = "missing";
		}
		memo.set( key, value );
		return value;
	}

	fingerprint.prefetch = async inputs => {
		const queue = [ ...new Set( inputs.map( ( [kind, file] ) => slot( kind, file ) ) ) ]
			.filter( key => !memo.has( key ) );
		let next = 0;
		async function worker() {
			while ( next < queue.length ) {
				const key = queue[next++], file = key.slice( 2 );
				let value;
				try {
					const stat = await fs.promises.stat( file );
					if ( key[0] === "D" ) {
						value = stat.isDirectory() ?
							listing( await fs.promises.readdir( file, { withFileTypes: true } ) ) :
							"not-dir";
					} else {
						value = stat.isDirectory() ?
							"dir" :
							known( file, stat ) ?? remember( file, stat, await fs.promises.readFile( file ) );
					}
				} catch {
					value = "missing";
				}
				memo.set( key, value );
			}
		}
		await Promise.all( Array.from( { length: PREFETCH_WORKERS }, worker ) );
	};
	return fingerprint;
}

/*
================
valid
================
*/
function valid( record, fingerprint, name ) {
	for ( const [id, value] of Object.entries( record.inputs ) ) {
		const tab = id.indexOf( "\t" );
		if ( fingerprint( id.slice( 0, tab ), id.slice( tab + 1 ) ) !== value ) {
			if ( process.env.SRO_TEST_CACHE_EXPLAIN ) process.stdout.write( `rerun ${name}: ${id} changed\n` );
			return false;
		}
	}
	return true;
}

/*
================
recordOf

The inputs a finished file logged, minus what it wrote. Null when it did
something no hash can capture.
================
*/
function recordOf( logDir, file, fingerprint ) {
	let lines;
	try {
		lines = fs.readFileSync( logPathFor( logDir, file ), "utf8" ).split( "\n" ).filter( Boolean );
	} catch {
		return null;
	}
	const blocked = lines.find( line => line.startsWith( "X\t" ) );
	if ( blocked ) {
		if ( process.env.SRO_TEST_CACHE_EXPLAIN ) {
			process.stdout.write( `uncached ${path.relative( clientRoot, file )}: ${blocked.slice( 2 )}\n` );
		}
		return null;
	}
	const written = new Set(
		lines.filter( line => line.startsWith( "W\t" ) ).map( line => line.slice( 2 ).toLowerCase() )
	);
	const inputs = {};
	for ( const line of lines ) {
		const kind = line[0], target = line.slice( 2 );
		if ( kind === "W" || written.has( target.toLowerCase() ) ) continue;
		// A probe and a read of the same file share the read's content hash.
		const id = (kind === "E" ? "E" : kind) + "\t" + target;
		inputs[id] = fingerprint( kind, target );
	}
	return { inputs };
}

/*
================
main
================
*/
async function main() {
	const started = performance.now();
	// Inside a node --test child, run() skips every file and reports nothing
	// failed; a pass must never be recorded from a run that ran nothing.
	if ( process.env.NODE_TEST_CONTEXT ) {
		throw new Error( "run-tests.mjs must run at top level, not inside node --test (NODE_TEST_CONTEXT is set)" );
	}
	const files = testFiles( process.argv.slice( 2 ) );
	if ( !files.length ) throw new Error( "Usage: node tools/run-tests.mjs <dir|file>..." );
	const index = readIndex( cacheKey() );
	const fingerprint = createFingerprints( index );
	const recorded = files.map( file => index.records[path.relative( repoRoot, file )] ).filter( Boolean );
	await fingerprint.prefetch(
		recorded.flatMap( record =>
			Object.keys( record.inputs ).map(
				id => [ id.slice( 0, id.indexOf( "\t" ) ), id.slice( id.indexOf( "\t" ) + 1 ) ]
			)
		)
	);
	const pending = files.filter( file => {
		const record = index.records[path.relative( repoRoot, file )];
		return !record || !valid( record, fingerprint, path.relative( clientRoot, file ) );
	} );
	const reused = files.length - pending.length;
	const failed = new Set(), passed = new Set();
	if ( pending.length ) {
		const logDir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-test-inputs-" ) );
		process.env.SRO_TEST_INPUT_LOG = logDir;
		// A runner started inside a recorded test owns its own tests.
		delete process.env.SRO_TEST_INPUT_OWNER;
		process.env.NODE_OPTIONS = `${process.env.NODE_OPTIONS ?? ""} --import ${recorderUrl}`.trim();
		try {
			const stream = run( { files: pending, concurrency: os.availableParallelism(), cwd: clientRoot } );
			// The stream hands listeners the event's data itself.
			stream.on( "test:fail", data => {
				if ( data?.file ) failed.add( path.resolve( data.file ) );
			} );
			stream.on( "test:pass", data => {
				if ( data?.file ) passed.add( path.resolve( data.file ) );
			} );
			stream.compose( spec ).pipe( process.stdout );
			await new Promise( ( resolve, reject ) => {
				stream.on( "end", resolve );
				stream.on( "error", reject );
			} );
			// Inputs are hashed after every test finished; a fresh fingerprint
			// sees files the run itself produced.
			const settled = createFingerprints( index );
			for ( const file of pending ) {
				const key = path.relative( repoRoot, file );
				delete index.records[key];
				// Only a file that passed at least one test and failed none.
				if ( failed.has( file ) || !passed.has( file ) ) continue;
				const record = recordOf( logDir, file, settled );
				if ( record ) index.records[key] = record;
			}
		} finally {
			fs.rmSync( logDir, { recursive: true, force: true } );
		}
	}
	fs.mkdirSync( cacheDir, { recursive: true } );
	const temporary = indexPath + "." + process.pid;
	fs.writeFileSync( temporary, JSON.stringify( index ) );
	fs.renameSync( temporary, indexPath );
	const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
	process.stdout.write(
		`tests: ${files.length} files, ${reused} reused unchanged, ${pending.length} run, ${failed.size} failed in ${seconds}s\n`
	);
	if ( failed.size ) process.exitCode = 1;
}

await main();
