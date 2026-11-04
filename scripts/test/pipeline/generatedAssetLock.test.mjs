/*
===========================================================================

generatedAssetLock.test.mjs - publication and verification across worktrees

Real child processes use copied, unmodified Node and Python lock owners.
Synthetic junctions and SRO_GENERATED_ROOT redirects exercise the same shared
generated tree as local worktrees without reading or modifying licensed assets
or another process's locks.

===========================================================================
*/

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { isDeletePendingClaimError } from "../../rebuildLock.mjs";

const SCRIPTS_ROOT = fileURLToPath( new URL( "../../", import.meta.url ) );
// The caller's generated-root override would point every child at the real tree.
const LOCK_ENV = [ "SRO_REBUILD_LOCK_NAME", "SRO_REBUILD_LOCK_TOKEN", "SRO_REBUILD_LOCK_DIR", "SRO_GENERATED_ROOT" ];
// The lock owners and the generated-root owners they resolve the tree through.
const LOCK_SOURCES = [
	"rebuildLock.mjs",
	"rebuildLockCli.mjs",
	"rebuild_lock.py",
	"sro_paths.py",
	"lib/generatedRoot.mjs"
];
const NODE_DRIVER = `
const { withGeneratedAssetsLock } = await import('./scripts/rebuildLock.mjs');
console.log('TRYING');
await withGeneratedAssetsLock('test reader or publisher', async () => {
  console.log('ENTERED PID=' + process.pid);
  if (process.env.HOLD === '1') {
    await new Promise(resolve => process.stdin.once('data', resolve));
  }
  if (process.env.FAIL === '1') throw new Error('intentional callback failure');
}, { mode: process.env.MODE || 'write' });
`;
// The mutex-race regression child: a loader hook wraps node:fs/promises
// so the first mutex claim's mkdir removes the lock dir underneath
// itself - the empty-state release landing between the ensure and the
// claim, injected deterministically instead of raced. The hook file
// only forwards to the wrapper source beside it.
const MUTEX_RACE_HOOK_SOURCE = `import { readFileSync } from "node:fs";
const WRAPPER_SOURCE = readFileSync( new URL( "./mutexRaceFsWrapper.mjs", import.meta.url ), "utf8" );
export async function load( url, context, nextLoad ) {
	if ( url !== "node:fs/promises" ) return nextLoad( url, context );
	return { format: "module", shortCircuit: true, source: WRAPPER_SOURCE };
}
`;
const MUTEX_RACE_FS_WRAPPER_SOURCE = `import { promises as realFs } from "node:fs";
import path from "node:path";
let injected = false;
export async function mkdir( target, options ) {
	const text = String( target );
	if ( !injected && text.endsWith( "mutex" ) ) {
		injected = true;
		console.error( "INJECTED lock dir removal before the mutex claim" );
		await realFs.rm( path.dirname( text ), { recursive: true, force: true } );
	}
	return realFs.mkdir( target, options );
}
export const readFile = realFs.readFile;
export const readdir = realFs.readdir;
export const realpath = realFs.realpath;
export const rename = realFs.rename;
export const rm = realFs.rm;
export const stat = realFs.stat;
export const writeFile = realFs.writeFile;
`;
const MUTEX_RACE_DRIVER = `
import { register } from "node:module";
import path from "node:path";
import { pathToFileURL } from "node:url";
register( pathToFileURL( path.join( process.cwd(), "scripts", "mutexRaceHook.mjs" ) ) );
const { withGeneratedAssetsLock } = await import('./scripts/rebuildLock.mjs');
await withGeneratedAssetsLock('mutex race regression', async () => {
  console.log('ENTERED');
}, { mode: 'write' });
`;
const PYTHON_DRIVER = `
import os, sys
sys.path.insert(0, 'scripts')
from rebuild_lock import generated_assets_lock
print('TRYING', flush=True)
with generated_assets_lock('test reader or publisher', mode=os.environ.get('MODE', 'write')):
    print('ENTERED PID=%d' % os.getpid(), flush=True)
    if os.environ.get('HOLD') == '1':
        sys.stdin.readline()
    if os.environ.get('FAIL') == '1':
        raise RuntimeError('intentional callback failure')
`;

/*
================
fixture
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-asset-lock-" ) );
	const children = [];
	t.after( async () => {
		for ( const child of children ) {
			if ( child.exitCode === null ) child.kill();
		}
		await Promise.all( children.map( ( child ) => child.finished ) );
		await rm( root, { recursive: true, force: true } );
	} );
	const owner = path.join( root, "owner" );
	const alias = path.join( root, "alias" );
	const separate = path.join( root, "separate" );
	const redirected = path.join( root, "redirected" );
	for ( const checkout of [ owner, alias, separate, redirected ] ) {
		await mkdir( path.join( checkout, "scripts", "lib" ), { recursive: true } );
		for ( const name of LOCK_SOURCES ) {
			await copyFile( path.join( SCRIPTS_ROOT, name ), path.join( checkout, "scripts", name ) );
		}
	}
	await mkdir( path.join( owner, ".generated" ) );
	await mkdir( path.join( separate, ".generated" ) );
	await symlink( path.join( owner, ".generated" ), path.join( alias, ".generated" ), "junction" );
	return {
		root,
		owner,
		alias,
		separate,
		redirected,
		children,
		lockDir: path.join( owner, ".state/locks/generated-assets.lock" )
	};
}

/*
================
start
================
*/
function start( f, options ) {
	const env = { ...process.env };
	for ( const key of LOCK_ENV ) delete env[key];
	Object.assign( env, {
		SRO_REBUILD_LOCK_TIMEOUT_MS: "30000",
		SRO_REBUILD_LOCK_POLL_MS: "250",
		SRO_REBUILD_LOCK_STALE_MS: "60000",
		...options.env
	} );
	const python = options.language === "python";
	const child = spawn(
		python ? (process.platform === "win32" ? "py" : "python3") : process.execPath,
		python ? [ "-B", "-c", PYTHON_DRIVER ] : [ "--input-type=module", "-e", options.driver || NODE_DRIVER ],
		{ cwd: options.cwd, env, windowsHide: true, stdio: [ "pipe", "pipe", "pipe" ] }
	);
	child.output = "";
	child.stdout.on( "data", ( chunk ) => child.output += chunk );
	child.stderr.on( "data", ( chunk ) => child.output += chunk );
	child.finished = new Promise( ( resolve, reject ) => {
		child.once( "error", reject );
		child.once( "close", resolve );
	} );
	f.children.push( child );
	return child;
}

/*
================
entered
================
*/
async function entered( child ) {
	const deadline = Date.now() + 120000;
	while ( !child.output.includes( "ENTERED" ) ) {
		assert.equal( child.exitCode, null, child.output );
		assert.ok( Date.now() < deadline, child.output );
		await new Promise( ( resolve ) => setTimeout( resolve, 20 ) );
	}
}

/*
================
trying

The child reached its acquire loop (both drivers print TRYING first),
the signal blockedChild's refusal window starts from.
================
*/
async function trying( child ) {
	const deadline = Date.now() + 120000;
	while ( !child.output.includes( "TRYING" ) ) {
		assert.equal( child.exitCode, null, child.output );
		assert.ok( Date.now() < deadline, child.output );
		await new Promise( ( resolve ) => setTimeout( resolve, 20 ) );
	}
}

/*
================
blockedChild

A contender whose refusal is proven from lock state, the way queued()
proves it in the Python modes test: its own lock timeout (600 s) is a
wedged-child bound the pass path never approaches, never the verdict's
clock. A writer is proven by its waiting marker on disk; anything that
publishes nothing (a reader, or a contender stuck before it can
inspect) is proven by TRYING seen, still alive, and no ENTERED across
a refusal window a correct lock cannot cross however loaded the
machine is. Returns the live child; the caller kills it, or releases
its holder and admits it.

Its stale threshold is an hour, not the suite default's 60 s: a
blocked contender must never reap a holder whose heartbeat merely
starved on a loaded machine (the suite default would let it enter,
indistinguishable from a queueing bug). Reaping keeps its own tests,
with planted records and pid deaths. options.windowMs shortens the
refusal window where the lock's own grace is shorter than 5 s.
================
*/
async function blockedChild( f, options ) {
	const child = start( f, {
		...options,
		env: {
			...options.env,
			HOLD: "1",
			SRO_REBUILD_LOCK_TIMEOUT_MS: "600000",
			SRO_REBUILD_LOCK_STALE_MS: "3600000"
		}
	} );
	await trying( child );
	if ( (options.env?.MODE || "write") === "write" && options.marker !== false ) {
		await waitingForWriter( f );
	} else {
		const end = Date.now() + (options.windowMs ?? 5000);
		while ( Date.now() < end ) {
			assert.equal( child.exitCode, null, child.output );
			assert.ok( !child.output.includes( "ENTERED" ), child.output );
			await new Promise( ( resolve ) => setTimeout( resolve, 20 ) );
		}
	}
	assert.equal( child.exitCode, null, child.output );
	assert.ok( !child.output.includes( "ENTERED" ), child.output );
	return child;
}

for ( const holderLanguage of [ "node", "python" ] ) {
	for ( const readerLanguage of [ "node", "python" ] ) {
		test(`${holderLanguage} publisher blocks ${readerLanguage} verification through SRO_GENERATED_ROOT`, async ( t ) => {
			const f = await fixture( t );
			const holder = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1" } } );
			await entered( holder );
			const shared = { SRO_GENERATED_ROOT: path.join( f.owner, ".generated" ) };
			const reader = await blockedChild( f, { cwd: f.redirected, language: readerLanguage, env: shared } );
			reader.kill();
			await reader.finished;
			holder.stdin.end( "\n" );
			assert.equal( await holder.finished, 0, holder.output );
			const nextReader = start( f, { cwd: f.redirected, language: readerLanguage, env: shared } );
			assert.equal( await nextReader.finished, 0, nextReader.output );
			assert.match( nextReader.output, /ENTERED/ );
		});

		test(`${holderLanguage} publisher blocks ${readerLanguage} verification through a junction`, async ( t ) => {
			const f = await fixture( t );
			const holder = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1" } } );
			await entered( holder );
			const reader = await blockedChild( f, { cwd: f.alias, language: readerLanguage } );
			reader.kill();
			await reader.finished;
			assert.equal( holder.exitCode, null );
			holder.stdin.end( "\n" );
			assert.equal( await holder.finished, 0, holder.output );
			const nextReader = start( f, { cwd: f.alias, language: readerLanguage } );
			assert.equal( await nextReader.finished, 0, nextReader.output );
			assert.match( nextReader.output, /ENTERED/ );
		});
	}

	test(`${holderLanguage} does not steal a state claim before owner metadata is published`, async ( t ) => {
		const f = await fixture( t );
		// The transition mutex is the claim whose owner record follows its
		// mkdir; a bare mutex directory is a claim mid-publish, not litter.
		await mkdir( path.join( f.lockDir, "mutex" ), { recursive: true } );
		// The window stays under the locks' 5 s unpublished-mutex grace:
		// past it the bare claim is stealable by design, before it a
		// correct lock cannot admit the contender however loaded it is.
		const reader = await blockedChild( f, {
			cwd: f.alias,
			language: holderLanguage,
			marker: false,
			windowMs: 2500
		} );
		reader.kill();
		await reader.finished;
	});

	test(`${holderLanguage} treats a bare state directory as no hold at all`, async ( t ) => {
		const f = await fixture( t );
		// Reader/writer state lives in claim files inside the directory; the
		// directory alone excludes nobody (it is not the pre-RW claim).
		await mkdir( f.lockDir, { recursive: true } );
		const reader = start( f, { cwd: f.alias, language: holderLanguage } );
		assert.equal( await reader.finished, 0, reader.output );
		assert.match( reader.output, /ENTERED/ );
	});

	test(`${holderLanguage} releases ownership when verification fails`, async ( t ) => {
		const f = await fixture( t );
		const failed = start( f, { cwd: f.alias, language: holderLanguage, env: { FAIL: "1" } } );
		assert.notEqual( await failed.finished, 0, failed.output );
		assert.match( failed.output, /intentional callback failure/ );
		const reader = start( f, { cwd: f.owner, language: holderLanguage } );
		assert.equal( await reader.finished, 0, reader.output );
	});

	test(`${holderLanguage} inherits only ownership of the same physical tree`, async ( t ) => {
		const f = await fixture( t );
		const holder = start( f, { cwd: f.owner, language: "node", env: { HOLD: "1" } } );
		await entered( holder );
		const owner = JSON.parse( await readFile( path.join( f.lockDir, "owner.json" ), "utf8" ) );
		const inherited = {
			SRO_REBUILD_LOCK_NAME: owner.name,
			SRO_REBUILD_LOCK_TOKEN: owner.token,
			SRO_REBUILD_LOCK_DIR: owner.lockDir
		};
		const nested = start( f, { cwd: f.alias, language: holderLanguage, env: inherited } );
		assert.equal( await nested.finished, 0, nested.output );
		const separate = start( f, { cwd: f.separate, language: holderLanguage, env: { ...inherited, HOLD: "1" } } );
		await entered( separate );
		const separateOwner = JSON.parse(
			await readFile(
				path.join( f.separate, ".state/locks/generated-assets.lock/owner.json" ),
				"utf8"
			)
		);
		assert.equal( separateOwner.pid, Number( separate.output.match( /ENTERED PID=(\d+)/ )[1] ) );
		assert.notEqual( separateOwner.token, owner.token );
		separate.stdin.end( "\n" );
		holder.stdin.end( "\n" );
		assert.equal( await separate.finished, 0, separate.output );
		assert.equal( await holder.finished, 0, holder.output );
	});
}

/*
================
waitingForWriter

The writer-preference sync point: a writer publishes waiting/<token>.json
before it queues, so once the marker is on disk every new reader must
queue behind it.
================
*/
async function waitingForWriter( f ) {
	const waiting = path.join( f.lockDir, "waiting" );
	const deadline = Date.now() + 120000;
	for ( ;; ) {
		const names = await readdir( waiting ).catch( () => [] );
		if ( names.some( ( name ) => name.endsWith( ".json" ) ) ) return;
		assert.ok( Date.now() < deadline, "writer never published its waiting marker" );
		await new Promise( ( resolve ) => setTimeout( resolve, 20 ) );
	}
}

for ( const holderLanguage of [ "node", "python" ] ) {
	for ( const contenderLanguage of [ "node", "python" ] ) {
		test(`two readers (${holderLanguage} + ${contenderLanguage}) hold at once`, async ( t ) => {
			const f = await fixture( t );
			const first = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1", MODE: "read" } } );
			await entered( first );
			const second = start( f, { cwd: f.alias, language: contenderLanguage, env: { HOLD: "1", MODE: "read" } } );
			await entered( second );
			first.stdin.end( "\n" );
			second.stdin.end( "\n" );
			assert.equal( await first.finished, 0, first.output );
			assert.equal( await second.finished, 0, second.output );
		});

		test(`a ${contenderLanguage} writer waits for a ${holderLanguage} reader, then enters`, async ( t ) => {
			const f = await fixture( t );
			const reader = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1", MODE: "read" } } );
			await entered( reader );
			const blocked = await blockedChild( f, {
				cwd: f.alias,
				language: contenderLanguage,
				env: { MODE: "write" }
			} );
			blocked.kill();
			await blocked.finished;
			reader.stdin.end( "\n" );
			assert.equal( await reader.finished, 0, reader.output );
			const writer = start( f, { cwd: f.alias, language: contenderLanguage, env: { MODE: "write" } } );
			assert.equal( await writer.finished, 0, writer.output );
			assert.match( writer.output, /ENTERED/ );
		});

		test(`a ${contenderLanguage} reader queues behind a waiting ${holderLanguage} writer`, async ( t ) => {
			const f = await fixture( t );
			const reader = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1", MODE: "read" } } );
			await entered( reader );
			const writer = start( f, {
				cwd: f.alias,
				language: contenderLanguage,
				env: { HOLD: "1", MODE: "write", SRO_REBUILD_LOCK_TIMEOUT_MS: "600000" }
			} );
			await waitingForWriter( f );
			const queued = await blockedChild( f, {
				cwd: f.alias,
				language: contenderLanguage,
				env: { MODE: "read" }
			} );
			reader.stdin.end( "\n" );
			await entered( writer );
			// While the writer holds, another reader must queue instead of entering.
			const stillQueued = await blockedChild( f, {
				cwd: f.alias,
				language: contenderLanguage,
				env: { MODE: "read" }
			} );
			stillQueued.kill();
			await stillQueued.finished;
			writer.stdin.end( "\n" );
			assert.equal( await writer.finished, 0, writer.output );
			await entered( queued );
			queued.stdin.end( "\n" );
			assert.equal( await queued.finished, 0, queued.output );
		});
	}
}

for ( const [holderLanguage, contenderLanguage] of [ [ "node", "python" ], [ "python", "node" ] ] ) {
	test(`a killed ${holderLanguage} reader's claim is reaped for a ${contenderLanguage} writer`, async ( t ) => {
		const f = await fixture( t );
		const reader = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1", MODE: "read" } } );
		await entered( reader );
		const blocked = await blockedChild( f, { cwd: f.alias, language: contenderLanguage, env: { MODE: "write" } } );
		blocked.kill();
		await blocked.finished;
		reader.kill();
		await reader.finished;
		const writer = start( f, { cwd: f.alias, language: contenderLanguage, env: { MODE: "write" } } );
		assert.equal( await writer.finished, 0, writer.output );
		assert.match( writer.output, /ENTERED/ );
	});

	test(`a killed ${holderLanguage} writer's claim is reaped for a ${contenderLanguage} reader`, async ( t ) => {
		const f = await fixture( t );
		const writer = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1", MODE: "write" } } );
		await entered( writer );
		const blocked = await blockedChild( f, { cwd: f.alias, language: contenderLanguage, env: { MODE: "read" } } );
		blocked.kill();
		await blocked.finished;
		writer.kill();
		await writer.finished;
		const reader = start( f, { cwd: f.alias, language: contenderLanguage, env: { MODE: "read" } } );
		assert.equal( await reader.finished, 0, reader.output );
		assert.match( reader.output, /ENTERED/ );
	});
}

test("a contender whose lock dir is removed before its mutex claim still enters", async ( t ) => {
	const f = await fixture( t );
	// The hook and wrapper land in the copied scripts tree the child
	// imports its lock from; the injection is the removal itself, so
	// the verdict cannot depend on how the processes are scheduled.
	await writeFile( path.join( f.owner, "scripts", "mutexRaceHook.mjs" ), MUTEX_RACE_HOOK_SOURCE );
	await writeFile( path.join( f.owner, "scripts", "mutexRaceFsWrapper.mjs" ), MUTEX_RACE_FS_WRAPPER_SOURCE );
	const child = start( f, { cwd: f.owner, language: "node", driver: MUTEX_RACE_DRIVER } );
	assert.equal( await child.finished, 0, child.output );
	assert.match( child.output, /INJECTED lock dir removal/ );
	assert.match( child.output, /ENTERED/ );
});

for ( const language of [ "node", "python" ] ) {
	test(`a ${language} contender with a short lock timeout expires while its holder never releases`, async ( t ) => {
		const f = await fixture( t );
		const holder = start( f, { cwd: f.owner, language, env: { HOLD: "1", MODE: "write" } } );
		await entered( holder );
		const blocked = start( f, {
			cwd: f.alias,
			language,
			env: { MODE: "write", SRO_REBUILD_LOCK_TIMEOUT_MS: "500" }
		} );
		assert.notEqual( await blocked.finished, 0, blocked.output );
		assert.match( blocked.output, /timed out waiting/ );
		assert.doesNotMatch( blocked.output, /ENTERED/ );
		holder.stdin.end( "\n" );
		assert.equal( await holder.finished, 0, holder.output );
	});
}

test("a writer whose heartbeat went stale is reaped even while its pid lives", async ( t ) => {
	const f = await fixture( t );
	// A live pid to plant on the stale record (its own reader claim does not
	// block the reader contender; only the planted writer record can).
	const sleeper = start( f, { cwd: f.owner, language: "node", env: { HOLD: "1", MODE: "read" } } );
	await entered( sleeper );
	const pid = Number( sleeper.output.match( /ENTERED PID=(\d+)/ )[1] );
	const staleAt = new Date( Date.now() - 120000 ).toISOString();
	const planted = {
		name: "generated-assets",
		label: "planted stale writer",
		token: "planted-stale-writer",
		pid,
		ppid: 1,
		user: "test",
		host: "test",
		cwd: f.owner,
		command: "planted",
		lockDir: f.lockDir,
		startedAt: staleAt,
		heartbeatAt: staleAt,
		mode: "write"
	};
	await writeFile( path.join( f.lockDir, "writer.json" ), `${JSON.stringify( planted, null, 2 )}\n`, "utf8" );
	await writeFile( path.join( f.lockDir, "owner.json" ), `${JSON.stringify( planted, null, 2 )}\n`, "utf8" );
	const reader = start( f, { cwd: f.alias, language: "python", env: { MODE: "read" } } );
	assert.equal( await reader.finished, 0, reader.output );
	assert.match( reader.output, /ENTERED/ );
	sleeper.stdin.end( "\n" );
	assert.equal( await sleeper.finished, 0, sleeper.output );
});

test("a reader nests inside a writer, and a writer refuses to nest inside a reader", async ( t ) => {
	const f = await fixture( t );
	const writer = start( f, { cwd: f.owner, language: "node", env: { HOLD: "1", MODE: "write" } } );
	await entered( writer );
	const writerOwner = JSON.parse( await readFile( path.join( f.lockDir, "owner.json" ), "utf8" ) );
	const writerEnv = {
		SRO_REBUILD_LOCK_NAME: writerOwner.name,
		SRO_REBUILD_LOCK_TOKEN: writerOwner.token,
		SRO_REBUILD_LOCK_DIR: writerOwner.lockDir,
		SRO_REBUILD_LOCK_MODE: "write"
	};
	const nestedReader = start( f, { cwd: f.alias, language: "python", env: { ...writerEnv, MODE: "read" } } );
	assert.equal( await nestedReader.finished, 0, nestedReader.output );
	assert.match( nestedReader.output, /ENTERED/ );
	writer.stdin.end( "\n" );
	assert.equal( await writer.finished, 0, writer.output );

	const reader = start( f, { cwd: f.owner, language: "node", env: { HOLD: "1", MODE: "read" } } );
	await entered( reader );
	const readerOwner = JSON.parse( await readFile( path.join( f.lockDir, "owner.json" ), "utf8" ) );
	const readerEnv = {
		SRO_REBUILD_LOCK_NAME: readerOwner.name,
		SRO_REBUILD_LOCK_TOKEN: readerOwner.token,
		SRO_REBUILD_LOCK_DIR: readerOwner.lockDir,
		SRO_REBUILD_LOCK_MODE: "read"
	};
	const nestedRead = start( f, { cwd: f.alias, language: "python", env: { ...readerEnv, MODE: "read" } } );
	assert.equal( await nestedRead.finished, 0, nestedRead.output );
	assert.match( nestedRead.output, /ENTERED/ );
	for ( const language of [ "node", "python" ] ) {
		const nestedWrite = start( f, { cwd: f.alias, language, env: { ...readerEnv, MODE: "write" } } );
		assert.notEqual( await nestedWrite.finished, 0, nestedWrite.output );
		assert.match( nestedWrite.output, /cannot be taken inside the read lock/ );
		assert.doesNotMatch( nestedWrite.output, /ENTERED/ );
	}
	reader.stdin.end( "\n" );
	assert.equal( await reader.finished, 0, reader.output );
});

test("the command-line entry prints status and runs a command under a read hold", async ( t ) => {
	const f = await fixture( t );
	const env = { ...process.env };
	for ( const key of LOCK_ENV ) delete env[key];
	const cli = ( args ) =>
		new Promise( ( resolve, reject ) => {
			const child = spawn( process.execPath, [ path.join( f.owner, "scripts/rebuildLock.mjs" ), ...args ], {
				cwd: f.owner,
				env,
				windowsHide: true
			} );
			let output = "";
			child.stdout.on( "data", ( chunk ) => output += chunk );
			child.stderr.on( "data", ( chunk ) => output += chunk );
			child.once( "error", reject );
			child.once( "close", ( code ) => resolve( { code, output } ) );
		} );
	const status = await cli( [ "--name", "generated-assets", "--status" ] );
	assert.equal( status.code, 0, status.output );
	assert.match( status.output, /generated-assets is not locked\./ );
	const ran = await cli( [
		"--mode",
		"read",
		"--label",
		"cli smoke",
		"--",
		process.execPath,
		"-e",
		"console.log('UNDER CLI')"
	] );
	assert.equal( ran.code, 0, ran.output );
	assert.match( ran.output, /UNDER CLI/ );
});

test("a mutex claim retries only the Windows delete-pending errors", () => {
	assert.equal( isDeletePendingClaimError( { code: "EPERM" }, "win32" ), true );
	assert.equal( isDeletePendingClaimError( { code: "EACCES" }, "win32" ), true );
	assert.equal( isDeletePendingClaimError( { code: "EPERM" }, "linux" ), false );
	assert.equal( isDeletePendingClaimError( { code: "EEXIST" }, "win32" ), false );
});
