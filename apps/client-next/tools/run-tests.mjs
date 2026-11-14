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

tools/lib/test-process-watchdog.mjs is preloaded too: a test process dies
when its resident memory passes TEST_RSS_MB or when this runner is gone, so
neither a runaway test nor an orphan can take the machine.

Hashes are memoised by size and modification time, so validating an
unchanged tree is a stat sweep. The records live in the main checkout's
.state/client-test-cache, shared by every worktree: an input inside the
checkout is named relative to it (tools/lib/test-cache-ids.mjs), so a pass
in one worktree is reused by another whose files hash the same, and a fresh
worktree starts warm (ops #335: 177 s cold, 9 s warm). The size and mtime
memo names absolute paths, so it stays in each checkout's own .state. Both
are keyed by the Node version, platform and test environment; any change
there discards them. SRO_CHECK_FORCE=1 runs everything.

Every run times the files it actually executed and prints the thirty
slowest; a file over its per-file budget (TEST_FILE_BUDGET_MS, when set) fails the
run, and a run over its whole-suite budget (TEST_SUITE_BUDGET_MS, when
one is set) fails too (#2907, #2929), so suite time cannot creep back
one file at a time or in aggregate.

===========================================================================
*/
import { run } from "node:test";
import { spec } from "node:test/reporters";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { resolveMainCheckout } from "../../../scripts/lib/generatedRoot.mjs";
import { decodeTarget, encodeTarget } from "./lib/test-cache-ids.mjs";
import { logPathFor } from "./lib/test-input-recorder.mjs";

// 2: checkout-relative input ids, and the stat memo split from the records.
const CACHE_FORMAT = 2;
const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const repoRoot = path.resolve( clientRoot, "../.." );
const CACHE_SUBDIR = path.join( ".state", "client-test-cache" );
// SRO_TEST_CACHE_DIR (the runner's own tests) holds both files in one folder.
const cacheDir = process.env.SRO_TEST_CACHE_DIR ?? path.join( resolveMainCheckout( repoRoot ), CACHE_SUBDIR );
const statsDir = process.env.SRO_TEST_CACHE_DIR ?? path.join( repoRoot, CACHE_SUBDIR );
const indexPath = path.join( cacheDir, "index.json" );
const statsPath = path.join( statsDir, "stats.json" );
const recorderUrl = pathToFileURL( path.join( clientRoot, "tools/lib/test-input-recorder.mjs" ) ).href;
const watchdogUrl = pathToFileURL( path.join( clientRoot, "tools/lib/test-process-watchdog.mjs" ) ).href;
// The environment a test can see beyond its files.
const ENV_KEYS = [ "TZ", "LANG", "NODE_ENV" ];
// Parallel stats: more pool threads than the default four, and enough
// requests in flight to keep them busy. Set before the pool first starts.
const PREFETCH_WORKERS = 64;
process.env.UV_THREADPOOL_SIZE ??= String( Math.min( 64, Math.max( 4, os.availableParallelism() ) ) );
// The JS heap ceiling of every test process, in MB. A test process can
// outlive this runner: a caller that stops reading early (a truncated pipe)
// kills the runner, and on Windows its children run on. One spinning in a
// loop then allocated until the machine ran out (2026-10-27: 25-40 GB each,
// twice at once). V8 aborts at the ceiling even inside a synchronous loop,
// where no timer or watchdog can run. No suite file needs more than about a
// gigabyte. SRO_TEST_HEAP_MB overrides it for the runner's own test.
const TEST_HEAP_MB = Number( process.env.SRO_TEST_HEAP_MB ) || 4096;
// The resident memory ceiling of every test process, in MB, enforced by
// tools/lib/test-process-watchdog.mjs: the heap ceiling above does not
// bound typed arrays or Buffers. The heap ceiling plus RSS_HEADROOM_MB
// for buffers, code and the runtime. SRO_TEST_RSS_MB overrides it for the
// watchdog's own test.
const RSS_HEADROOM_MB = 2048;
const TEST_RSS_MB = Number( process.env.SRO_TEST_RSS_MB ) || TEST_HEAP_MB + RSS_HEADROOM_MB;
// A file that runs longer than this fails the run and names itself, so
// suite time cannot creep back file by file (the Go server's 30 s budget
// is the same idea). Only files that actually ran are timed; a reused
// pass spent nothing. The budget is wall time, so it binds only in the
// lane that measured it: the CI source lane sets 20 s via
// SRO_TEST_FILE_BUDGET_MS (the slowest ordinary file took 14.6 s there;
// docs/agents/client-test-profile.md), and the named caps below bind
// with it. No cap by default, as the whole-suite budget: the full check
// runs every file beside its other heavy gates, and its wall times
// measured the contention, not the files (#3459: ui-behavior 48 s alone,
// 83-114 s inside the check).
const TEST_FILE_BUDGET_MS = Number( process.env.SRO_TEST_FILE_BUDGET_MS ) || 0;
// The whole run's wall budget (#2929): the five-minute client budget
// #2907 named, enforced on the run itself instead of only implied by
// the per-file caps, so aggregate creep fails too. No cap by default,
// for the same unmeasured-full-lane reason; the CI source lane sets
// 300 s via SRO_TEST_SUITE_BUDGET_MS (it measured 79-113 s there).
const TEST_SUITE_BUDGET_MS = Number( process.env.SRO_TEST_SUITE_BUDGET_MS ) || 0;
// How many of the slowest files each run names: enough that the suite
// profile (docs/agents/client-test-profile.md, #2929) can be written
// straight from a CI log line.
const SLOWEST_PRINTED = 30;
// Named per-file caps replace the default for the files listed there
// (fileBudgets below); the ledger only ever shrinks.
const BUDGET_LEDGER = path.join( clientRoot, "tests", "architecture", "test-file-budgets.txt" );

/*
================
fileBudgets

The named per-file caps, as client-relative forward-slash paths to
milliseconds. The ledger names the files that already ran longer than the
default when the budget arrived - execution.test.mjs measured 109 s on
the two-core CI runner against 4.5 s on a 16-core workstation (#2907) -
so the budget can bind every other file today instead of waiting for
that one to be split. An entry naming a file that no longer exists
fails the run, the test-type-debt rule: debt is deleted, not carried.
================
*/
function fileBudgets() {
	const budgets = new Map();
	if ( !fs.existsSync( BUDGET_LEDGER ) ) return budgets;
	for ( const raw of fs.readFileSync( BUDGET_LEDGER, "utf8" ).split( /\r?\n/ ) ) {
		const line = raw.trim();
		if ( !line || line.startsWith( "#" ) ) continue;
		const match = /^(\S+)\s+(\d+)$/.exec( line );
		if ( !match ) throw new Error( `test-file-budgets: unreadable line: ${raw}` );
		if ( !fs.existsSync( path.join( clientRoot, match[1] ) ) ) {
			throw new Error( `test-file-budgets: ${match[1]} no longer exists; remove its line` );
		}
		budgets.set( match[1].replaceAll( "\\", "/" ), Number( match[2] ) * 1000 );
	}
	return budgets;
}

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
	const index = { key, stats: {}, records: {} };
	if ( process.env.SRO_CHECK_FORCE === "1" ) return index;
	index.records = readKeyed( indexPath, key, "records" );
	index.stats = readKeyed( statsPath, key, "stats" );
	return index;
}

/*
================
readKeyed

One cache file's field, or empty when the file is missing, unreadable or
written under another key.
================
*/
function readKeyed( file, key, field ) {
	try {
		const body = JSON.parse( fs.readFileSync( file, "utf8" ) );
		return body.key === key && body[field] ? body[field] : {};
	} catch {
		return {};
	}
}

/*
================
writeKeyed

Replaces one cache file atomically (a temporary file renamed over it).
The records file is shared by every worktree, and on Windows a rename onto
a file another run is renaming or reading fails with EPERM (four writers
racing on one file failed 90% of renames). A lost write costs a rerun,
never a wrong reuse, so it is reported and the run's verdict stands.
================
*/
function writeKeyed( file, body ) {
	const temporary = file + "." + process.pid;
	try {
		fs.mkdirSync( path.dirname( file ), { recursive: true } );
		fs.writeFileSync( temporary, JSON.stringify( body ) );
		fs.renameSync( temporary, file );
	} catch ( error ) {
		fs.rmSync( temporary, { force: true } );
		process.stdout.write( `test cache: not saved (${error.code ?? error.message}): ${file}\n` );
	}
}

/*
================
worktreeLink

The identity of a linked worktree's .git file: the main checkout it
names, not its bytes. Its one line is "gitdir: <hub>/.git/worktrees/<name>",
and the only thing read from it in a test process is the hub
(resolveMainCheckout); git itself runs as a child, which no record covers.
Hashing the bytes made every worktree's records stale in every other
worktree: 190 of 766 runtime files reran on 11-14 for this alone (ops #335).
Undefined for any other file.
================
*/
function worktreeLink( file, stat ) {
	if ( path.basename( file ) !== ".git" || !stat.isFile() ) return undefined;
	try {
		return "worktree of " + resolveMainCheckout( path.dirname( file ) );
	} catch {
		return undefined; // an unreadable link is hashed like any file
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
					worktreeLink( file, stat ) ?? known( file, stat ) ??
						remember( file, stat, fs.readFileSync( file ) );
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
							worktreeLink( file, stat ) ?? known( file, stat ) ??
								remember( file, stat, await fs.promises.readFile( file ) );
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
		if ( fingerprint( id.slice( 0, tab ), decodeTarget( repoRoot, id.slice( tab + 1 ) ) ) !== value ) {
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
		// The id names the target relative to the checkout when it is inside
		// it, so another worktree can validate the record.
		const id = (kind === "E" ? "E" : kind) + "\t" + encodeTarget( repoRoot, target );
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
				id => [
					id.slice( 0, id.indexOf( "\t" ) ),
					decodeTarget( repoRoot, id.slice( id.indexOf( "\t" ) + 1 ) )
				]
			)
		)
	);
	const pending = files.filter( file => {
		const record = index.records[path.relative( repoRoot, file )];
		return !record || !valid( record, fingerprint, path.relative( clientRoot, file ) );
	} );
	const reused = files.length - pending.length;
	const failed = new Set(), passed = new Set();
	// Each file this run executed: its new record, or null when it is not
	// to be reused (failed, passed nothing, or did something no hash holds).
	const changes = new Map();
	// Per-file wall time of the files that run (#2907), from each file
	// node's own reported duration. Reused files never appear here -
	// they spent nothing this run.
	const fileSeconds = new Map();
	if ( pending.length ) {
		const logDir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-test-inputs-" ) );
		process.env.SRO_TEST_INPUT_LOG = logDir;
		// A runner started inside a recorded test owns its own tests.
		delete process.env.SRO_TEST_INPUT_OWNER;
		// Each test process watches its memory and this runner (the watchdog).
		// A nested runner names itself, so its own tests watch it.
		process.env.SRO_TEST_RUNNER_PID = String( process.pid );
		process.env.SRO_TEST_RSS_MB = String( TEST_RSS_MB );
		process.env.NODE_OPTIONS = `${
			process.env.NODE_OPTIONS ?? ""
		} --max-old-space-size=${TEST_HEAP_MB} --import ${recorderUrl} --import ${watchdogUrl}`.trim();
		try {
			const stream = run( { files: pending, concurrency: os.availableParallelism(), cwd: clientRoot } );
			// The stream hands listeners the event's data itself.
			stream.on( "test:fail", data => {
				if ( data?.file ) failed.add( path.resolve( data.file ) );
			} );
			stream.on( "test:pass", data => {
				if ( data?.file ) passed.add( path.resolve( data.file ) );
			} );
			stream.on( "test:complete", data => {
				// The file's own node (its name is its path) reports the
				// file's whole duration, process start included.
				if ( data?.file && data.name === data.file && data.details?.duration_ms !== undefined ) {
					fileSeconds.set( path.resolve( data.file ), data.details.duration_ms / 1000 );
				}
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
				// Only a file that passed at least one test and failed none.
				const record = failed.has( file ) || !passed.has( file ) ? null : recordOf( logDir, file, settled );
				changes.set( key, record );
			}
		} finally {
			fs.rmSync( logDir, { recursive: true, force: true } );
		}
	}
	// The records are shared: another worktree may have written since this
	// run read them, so only this run's own files change in the newest copy.
	// Two runs finishing together can still lose one's records; that costs
	// a rerun, never a wrong reuse, since every input is hashed on read.
	const records = readKeyed( indexPath, index.key, "records" );
	for ( const [key, record] of changes ) {
		if ( record ) records[key] = record;
		else delete records[key];
	}
	writeKeyed( indexPath, { key: index.key, records } );
	writeKeyed( statsPath, { key: index.key, stats: index.stats } );
	const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
	process.stdout.write(
		`tests: ${files.length} files, ${reused} reused unchanged, ${pending.length} run, ${failed.size} failed in ${seconds}s\n`
	);
	// The slowest files name themselves every run, budgeted or not, so
	// the next profile is a log line away (#2907, docs/agents/client-gates.md).
	const slowest = [ ...fileSeconds ].sort( ( a, b ) => b[1] - a[1] );
	if ( slowest.length ) {
		process.stdout.write(
			`tests slowest: ${
				slowest.slice( 0, SLOWEST_PRINTED ).map(
					( [file, s] ) => `${path.relative( clientRoot, file )} ${s.toFixed( 1 )}s`
				).join( ", " )
			}\n`
		);
	}
	const budgets = fileBudgets();
	const capOf = file =>
		budgets.get( path.relative( clientRoot, file ).replaceAll( "\\", "/" ) ) ??
			TEST_FILE_BUDGET_MS;
	const overBudget = TEST_FILE_BUDGET_MS > 0 ? slowest.filter( ( [file, s] ) => s * 1000 > capOf( file ) ) : [];
	for ( const [file, s] of overBudget ) {
		process.stdout.write(
			`tests budget: ${path.relative( clientRoot, file )} ran ${s.toFixed( 1 )}s, over its ` +
				`${capOf( file ) / 1000}s per-file budget (#2907)\n`
		);
	}
	const suiteOver = TEST_SUITE_BUDGET_MS > 0 && Number( seconds ) * 1000 > TEST_SUITE_BUDGET_MS;
	if ( suiteOver ) {
		process.stdout.write(
			`tests budget: the suite ran ${seconds}s, over its ` +
				`${TEST_SUITE_BUDGET_MS / 1000}s whole-suite budget (#2929)\n`
		);
	}
	if ( failed.size || overBudget.length || suiteOver ) process.exitCode = 1;
}

await main();
