/*
===========================================================================

rebuildLock.mjs - serialize generated asset publication and verification

Node and Python publishers share physical-tree lock ownership, including
worktrees whose generated directory is a junction into another checkout.

The lock is reader/writer: verification (client checks and tests) only reads
the generated tree, so any number of readers hold the lock at once, while a
publisher (an asset build) holds it alone. A writer that has registered its
intent blocks new readers, so a stream of checks cannot starve a build.
Both this file and rebuild_lock.py speak the same on-disk protocol:

	<name>.lock/            state directory (not itself the claim)
		writer.json         the one writer's owner record, when held
		readers/<token>.json  one owner record per reader
		waiting/<token>.json  one owner record per waiting writer
		owner.json          status mirror: the writer record, or one reader's
		mutex/              mkdir-claimed while a transition is decided

All state transitions happen under the mutex; the holds themselves are the
files above, heartbeated by their owners and reaped by contenders when the
owner's pid is gone or its heartbeat goes stale. An owner.json with no
"mode" field is a pre-reader/writer holder and is treated as a writer, so
an older checkout's exclusive hold still excludes a newer reader or writer
(and a newer writer still publishes an owner.json an older contender waits
on). Nothing here changes what the lock guards: only who may share it.

The command-line front end (run a command under the lock, and the
--status view) lives in rebuildLockCli.mjs; this module is the protocol
and the library every build and check imports.

===========================================================================
*/

import { readFileSync, rmSync } from "node:fs";
import { mkdir, readFile, readdir, realpath, rename, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { GENERATED_ROOT } from "./lib/generatedRoot.mjs";
import { runCommandUnderLock } from "./rebuildLockCli.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const locksRoot = path.join( rebuildRoot, ".state", "locks" );

const LOCK_NAME_ENV = "SRO_REBUILD_LOCK_NAME";
const LOCK_TOKEN_ENV = "SRO_REBUILD_LOCK_TOKEN";
const LOCK_DIR_ENV = "SRO_REBUILD_LOCK_DIR";
const LOCK_MODE_ENV = "SRO_REBUILD_LOCK_MODE";
const DEFAULT_POLL_MS = 5000;
export const DEFAULT_STALE_MS = 12 * 60 * 60 * 1000;
const HEARTBEAT_MS = 10000;
const OWNER_PUBLICATION_GRACE_MS = 60000;
// The mutex is held for a handful of file operations, never across a wait,
// so a live holder older than this is stopped, not slow.
const MUTEX_STALE_MS = 60000;
const MUTEX_SPIN_MS = 10000;
const MUTEX_SPIN_SLEEP_MS = 15;
const persistentLocks = new Map();

export const GENERATED_ASSETS_LOCK_NAME = "generated-assets";
export const GENERATED_ASSETS_LOCK_LABEL = "generated asset rebuild";

/*
================
withGeneratedAssetsLock
================
*/
export async function withGeneratedAssetsLock( label, fn, options = {} ) {
	return withRebuildLock(
		{
			name: GENERATED_ASSETS_LOCK_NAME,
			label,
			...options
		},
		fn
	);
}

/*
================
withRebuildLock

Modes: "write" (the default) excludes everyone, as the lock always did;
"read" shares with other readers. A nested acquisition in the same process
(the SRO_REBUILD_LOCK_* env a lock wrapper hands its children) is a no-op,
except a write inside a read hold: the ancestor excludes nobody, so the
nested writer could neither safely proceed nor safely wait for its own
ancestor, and it fails loudly instead of corrupting the tree.
================
*/
export async function withRebuildLock( options, fn ) {
	const name = normalizeLockName( options.name ?? GENERATED_ASSETS_LOCK_NAME );
	const label = options.label ?? name;
	const mode = normalizeMode( options.mode );
	const lockDir = await rebuildLockDirectory( name );
	if (
		process.env[LOCK_NAME_ENV] === name && process.env[LOCK_TOKEN_ENV] &&
		process.env[LOCK_DIR_ENV] === lockDir
	) {
		const heldMode = process.env[LOCK_MODE_ENV] ?? "write";
		if ( heldMode === "write" || mode === "read" ) {
			return fn();
		}
		throw new Error(
			`[rebuild-lock] ${label}: a write lock cannot be taken inside the read lock this process already holds; run it as its own top-level command.`
		);
	}

	const timeoutMs = numberFromEnv( "SRO_REBUILD_LOCK_TIMEOUT_MS", options.timeoutMs ?? 0 );
	const pollMs = Math.max( 250, numberFromEnv( "SRO_REBUILD_LOCK_POLL_MS", options.pollMs ?? DEFAULT_POLL_MS ) );
	const staleMs = Math.max(
		60000,
		numberFromEnv( "SRO_REBUILD_LOCK_STALE_MS", options.staleMs ?? DEFAULT_STALE_MS )
	);
	const lock = await acquireRebuildLock( {
		name,
		label,
		mode,
		timeoutMs,
		pollMs,
		staleMs,
		quiet: options.quiet === true
	} );
	try {
		return await fn();
	} finally {
		await lock.release();
	}
}

/*
================
readRebuildLock
================
*/
export async function readRebuildLock( name = GENERATED_ASSETS_LOCK_NAME ) {
	const safeName = normalizeLockName( name );
	const lockDir = await rebuildLockDirectory( safeName );
	return readOwner( lockDir );
}

/**
 * Acquire a lock for the lifetime of this Node process.
 *
 * Vite can reload its config inside the same process, so repeated acquisition
 * of the same name is deliberately idempotent. The synchronous exit cleanup
 * matters on Windows: an async finally block is not guaranteed to finish when
 * Ctrl+C or taskkill tears down a long-running dev server.
 */
/*
================
acquirePersistentRebuildLock
================
*/
export async function acquirePersistentRebuildLock( options ) {
	const name = normalizeLockName( options.name ?? GENERATED_ASSETS_LOCK_NAME );
	const existing = persistentLocks.get( name );
	if ( existing ) {
		return existing;
	}

	const lock = await acquireRebuildLock( {
		name,
		label: options.label ?? name,
		mode: normalizeMode( options.mode ),
		timeoutMs: options.timeoutMs ?? 0,
		pollMs: Math.max( 250, options.pollMs ?? DEFAULT_POLL_MS ),
		staleMs: Math.max( 60000, options.staleMs ?? DEFAULT_STALE_MS ),
		quiet: options.quiet === true
	} );
	const cleanupOnExit = () => {
		// The state directory is shared now; remove only this hold's files.
		for ( const file of lock.ownFiles ) {
			try {
				rmSync( file, { force: true } );
			} catch {
				// The next contender validates owner liveness and removes stale state.
			}
		}
		if ( lock.mode === "write" ) {
			try {
				const owner = JSON.parse( readFileSync( path.join( lock.lockDir, "owner.json" ), "utf8" ) );
				if ( owner?.token === lock.token ) {
					rmSync( path.join( lock.lockDir, "owner.json" ), { force: true } );
				}
			} catch {
				// Same stale-state reasoning as above.
			}
		}
	};
	process.once( "exit", cleanupOnExit );

	const persistentLock = {
		...lock,
		async release() {
			process.off( "exit", cleanupOnExit );
			persistentLocks.delete( name );
			await lock.release();
		}
	};
	persistentLocks.set( name, persistentLock );
	return persistentLock;
}

/*
================
acquireRebuildLock
================
*/
async function acquireRebuildLock( { name, label, mode, timeoutMs, pollMs, staleMs, quiet = false } ) {
	const lockDir = await rebuildLockDirectory( name );
	const ownerPath = path.join( lockDir, "owner.json" );
	const writerPath = path.join( lockDir, "writer.json" );
	const readersDir = path.join( lockDir, "readers" );
	const waitingDir = path.join( lockDir, "waiting" );
	const token = `${process.pid}-${Date.now()}-${Math.random().toString( 16 ).slice( 2 )}`;
	const record = ownerRecord( { name, label, token, lockDir, mode } );
	const myReaderPath = path.join( readersDir, `${token}.json` );
	const myWaitingPath = path.join( waitingDir, `${token}.json` );
	const startedAt = Date.now();
	const deadline = timeoutMs > 0 ? startedAt + timeoutMs : Number.POSITIVE_INFINITY;
	let nextNoticeAt = 0;
	let waitingPublished = false;
	let heartbeat = null;

	await mkdir( path.dirname( lockDir ), { recursive: true } );

	const startHeartbeat = ( targets ) => {
		heartbeat = setInterval( () => {
			for ( const target of targets ) {
				writeStateFile( target, { ...record, mode, heartbeatAt: new Date().toISOString() } )
					.catch( () => {} );
			}
			if ( mode === "read" ) {
				refreshOwnSummary( ownerPath, token ).catch( () => {} );
			}
		}, HEARTBEAT_MS );
		heartbeat.unref?.();
	};

	for ( ;; ) {
		const inspection = await inspectUnderMutex( lockDir, staleMs, deadline, async ( state ) => {
			if ( mode === "read" ) {
				const blocker = state.writer ?? state.waiting[0];
				if ( blocker ) {
					return { acquired: false, blocker };
				}
				await writeStateFile( myReaderPath, { ...record, mode: "read" } );
				await writeStateFile( ownerPath, { ...record, mode: "read" } );
				return { acquired: true };
			}
			// A writer publishes its intent before it waits, so readers that
			// arrive from this moment on queue behind it (writer preference).
			if ( !waitingPublished ) {
				await writeStateFile( myWaitingPath, { ...record, mode: "waiting" } );
				waitingPublished = true;
				state.waiting.push( { ...record, mode: "waiting" } );
			}
			const blocker = state.writer ?? state.readers[0];
			if ( blocker ) {
				return { acquired: false, blocker };
			}
			await rm( myWaitingPath, { force: true } );
			waitingPublished = false;
			await writeStateFile( writerPath, { ...record, mode: "write" } );
			await writeStateFile( ownerPath, { ...record, mode: "write" } );
			return { acquired: true };
		} );

		if ( inspection === null ) {
			// The mutex stayed busy past its spin budget; count it as a wait.
		} else if ( inspection.acquired ) {
			startHeartbeat( mode === "read" ? [ myReaderPath ] : [ writerPath, ownerPath ] );
			process.env[LOCK_NAME_ENV] = name;
			process.env[LOCK_TOKEN_ENV] = token;
			process.env[LOCK_DIR_ENV] = lockDir;
			process.env[LOCK_MODE_ENV] = mode;
			if ( !quiet ) {
				console.error(
					`[rebuild-lock] ${label}: acquired ${relativeLockPath( lockDir )} (pid ${process.pid}, ${mode}).`
				);
			}

			return {
				lockDir,
				token,
				mode,
				ownFiles: mode === "read" ? [ myReaderPath ] : [ writerPath ],
				env: {
					[LOCK_NAME_ENV]: name,
					[LOCK_TOKEN_ENV]: token,
					[LOCK_DIR_ENV]: lockDir,
					[LOCK_MODE_ENV]: mode
				},
				async release() {
					if ( heartbeat ) {
						clearInterval( heartbeat );
					}
					await releaseUnderMutex( lockDir, staleMs, { mode, token, myReaderPath, writerPath, ownerPath } );
					if ( process.env[LOCK_TOKEN_ENV] === token ) {
						delete process.env[LOCK_NAME_ENV];
						delete process.env[LOCK_TOKEN_ENV];
						delete process.env[LOCK_DIR_ENV];
						delete process.env[LOCK_MODE_ENV];
					}
					if ( !quiet ) {
						console.error( `[rebuild-lock] ${label}: released ${relativeLockPath( lockDir )}.` );
					}
				}
			};
		}

		// A waiting writer keeps its intent fresh so contenders never reap it
		// as stale while it legitimately queues behind long readers.
		if ( mode === "write" && waitingPublished ) {
			await writeStateFile( myWaitingPath, { ...record, mode: "waiting" } ).catch( () => {} );
		}

		const now = Date.now();
		if ( now >= nextNoticeAt ) {
			if ( !quiet ) {
				const blocker = inspection?.blocker;
				console.error(
					`[rebuild-lock] ${label}: waiting for ${
						blocker ? formatOwner( blocker ) : "lock state to settle"
					} (${relativeLockPath( lockDir )}).`
				);
			}
			nextNoticeAt = now + Math.max( pollMs, 30000 );
		}

		if ( now >= deadline ) {
			if ( waitingPublished ) {
				await rm( myWaitingPath, { force: true } );
			}
			const blocker = inspection?.blocker;
			throw new Error(
				`[rebuild-lock] ${label}: timed out waiting for ${
					blocker ? formatOwner( blocker ) : "another rebuild"
				}.`
			);
		}

		await sleep( pollMs );
	}
}

/*
================
releaseUnderMutex

Removes this hold's claim, restores the status mirror for whoever remains,
and removes the state directory itself once the lock is fully idle (the
pre-reader/writer observable: no lock directory, no lock).
================
*/
async function releaseUnderMutex( lockDir, staleMs, hold ) {
	// Releasing must not give up: an unreleased claim with a live pid behind
	// it blocks writers until its heartbeat goes stale hours later. The
	// mutex always frees (its holder's section is milliseconds, and a dead
	// holder is reaped), so retry until it is taken.
	let mutex = null;
	while ( !mutex ) {
		mutex = await acquireStateMutex( lockDir, Number.POSITIVE_INFINITY );
		if ( !mutex ) {
			await sleep( MUTEX_SPIN_SLEEP_MS );
		}
	}
	try {
		if ( hold.mode === "read" ) {
			await rm( hold.myReaderPath, { force: true } );
		} else {
			const writer = await readStateFile( hold.writerPath );
			if ( writer?.record?.token === hold.token ) {
				await rm( hold.writerPath, { force: true } );
			}
			// The mirror goes before the inspection below: inspectState
			// treats a mirror without its claim file as a writer (the
			// crashed-mid-release case), which would otherwise turn this
			// live release into a phantom hold.
			const owner = await readStateFile( hold.ownerPath );
			if ( owner?.record?.token === hold.token ) {
				await rm( hold.ownerPath, { force: true } );
			}
		}
		const state = await inspectState( lockDir, staleMs );
		const firstReader = state.readers[0];
		if ( !state.writer && firstReader ) {
			await writeStateFile( hold.ownerPath, { ...firstReader, mode: "read" } );
		} else if ( !state.writer ) {
			await rm( hold.ownerPath, { force: true } );
		}
		if ( !state.writer && state.readers.length === 0 && state.waiting.length === 0 ) {
			// Still holding the mutex inside the directory being removed;
			// contenders re-create it on demand (every access ensures it).
			await rm( lockDir, { recursive: true, force: true } );
			return;
		}
	} finally {
		if ( mutex ) {
			await mutex.release();
		}
	}
}

/*
================
inspectUnderMutex

Runs decide() with a freshly inspected state while the transition mutex is
held. Returns decide()'s result, or null when the mutex stayed busy past
its spin budget and the caller should treat the round as a wait.
================
*/
async function inspectUnderMutex( lockDir, staleMs, deadline, decide ) {
	const mutex = await acquireStateMutex( lockDir, deadline );
	if ( !mutex ) {
		return null;
	}
	try {
		const state = await inspectState( lockDir, staleMs );
		return await decide( state );
	} finally {
		await mutex.release();
	}
}

/*
================
inspectState

Reads every claim in the state directory and reaps stale ones. Call only
with the mutex held (the read-only status path may skip the mutex and the
reaping). A record is stale when its pid is gone or its heartbeat is older
than staleMs; an unreadable record is stale only once it is older than the
publication grace, so a claim whose metadata is mid-publish is never stolen.
An owner.json without a mode is a legacy exclusive holder and is reported
as the writer.
================
*/
export async function inspectState( lockDir, staleMs, { reap = true } = {} ) {
	await mkdir( lockDir, { recursive: true } );
	/** @type {{ writer: object | undefined, readers: object[], waiting: object[] }} */
	const state = { writer: undefined, readers: [], waiting: [] };

	const writerFile = await readStateFile( path.join( lockDir, "writer.json" ) );
	if ( writerFile ) {
		if ( isStaleRecord( writerFile, staleMs ) ) {
			if ( reap ) await rm( path.join( lockDir, "writer.json" ), { force: true } );
		} else {
			state.writer = writerFile.record;
		}
	}

	state.readers = await inspectClaimDir( path.join( lockDir, "readers" ), staleMs, reap );
	state.waiting = await inspectClaimDir( path.join( lockDir, "waiting" ), staleMs, reap );

	const ownerFile = await readStateFile( path.join( lockDir, "owner.json" ) );
	if ( ownerFile ) {
		const owner = ownerFile.record;
		if ( !owner || owner.mode === undefined || (owner.mode === "write" && !state.writer) ) {
			// A legacy holder (no mode), or a writer mirror whose claim file
			// is gone: either way it excludes everyone while it is live.
			if ( isStaleRecord( ownerFile, staleMs ) ) {
				if ( reap ) await rm( path.join( lockDir, "owner.json" ), { force: true } );
			} else if ( owner ) {
				state.writer = owner;
			}
		}
	}
	return state;
}

/*
================
inspectClaimDir
================
*/
async function inspectClaimDir( dir, staleMs, reap ) {
	let names;
	try {
		names = await readdir( dir );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return [];
		throw error;
	}
	const live = [];
	for ( const name of names ) {
		if ( !name.endsWith( ".json" ) ) continue;
		const file = await readStateFile( path.join( dir, name ) );
		if ( !file ) continue;
		if ( isStaleRecord( file, staleMs ) ) {
			if ( reap ) await rm( path.join( dir, name ), { force: true } );
		} else if ( file.record ) {
			live.push( file.record );
		}
	}
	return live;
}

/*
================
isStaleRecord
================
*/
function isStaleRecord( file, staleMs ) {
	const record = file.record;
	if ( !record ) {
		return Date.now() - file.mtimeMs > OWNER_PUBLICATION_GRACE_MS;
	}
	const pid = Number( record.pid );
	if ( Number.isInteger( pid ) && pid > 0 && !isProcessAlive( pid ) ) {
		return true;
	}
	const heartbeatMs = Date.parse( record.heartbeatAt ?? record.startedAt ?? "" );
	const heartbeatAgeMs = Number.isFinite( heartbeatMs ) ? Date.now() - heartbeatMs : Number.POSITIVE_INFINITY;
	return heartbeatAgeMs > staleMs;
}

/*
================
isDeletePendingClaimError

On Windows a directory another process is removing stays in delete-pending
until its last handle closes, and a mkdir of that name then fails with EPERM
or EACCES. The holder is leaving, so the claim retries; a real permission
fault outlasts the spin and is thrown.
================
*/
export function isDeletePendingClaimError( error, platform = process.platform ) {
	return platform === "win32" && (error?.code === "EPERM" || error?.code === "EACCES");
}

/*
================
acquireStateMutex

mkdir is the claim; the owner record only publishes who holds it, for
stale reaping. Held for single-digit milliseconds per transition, so the
spin is short and gives up to the caller's poll loop rather than camping.
================
*/
async function acquireStateMutex( lockDir, deadline ) {
	const mutexDir = path.join( lockDir, "mutex" );
	const mutexOwnerPath = path.join( mutexDir, "owner.json" );
	const token = `${process.pid}-${Date.now()}-${Math.random().toString( 16 ).slice( 2 )}`;
	const spinUntil = Math.min( deadline, Date.now() + MUTEX_SPIN_MS );

	for ( ;; ) {
		await mkdir( lockDir, { recursive: true } );
		try {
			await mkdir( mutexDir );
		} catch ( error ) {
			if ( isDeletePendingClaimError( error ) && Date.now() < spinUntil ) {
				await sleep( MUTEX_SPIN_SLEEP_MS );
				continue;
			}
			if ( error?.code !== "EEXIST" ) {
				throw error;
			}
			const ownerFile = await readStateFile( mutexOwnerPath );
			if ( ownerFile && isStaleRecord( ownerFile, MUTEX_STALE_MS ) ) {
				await rm( mutexDir, { recursive: true, force: true } );
				continue;
			}
			// An unpublished mutex claim gets only a brief grace: its owner
			// writes the record immediately after the mkdir.
			if ( !ownerFile ) {
				try {
					const metadata = await stat( mutexDir );
					if ( Date.now() - metadata.mtimeMs > 5000 ) {
						await rm( mutexDir, { recursive: true, force: true } );
						continue;
					}
				} catch ( error ) {
					if ( error?.code === "ENOENT" ) continue;
					throw error;
				}
			}
			if ( Date.now() >= spinUntil ) {
				return null;
			}
			await sleep( MUTEX_SPIN_SLEEP_MS );
			continue;
		}

		const record = ownerRecord( {
			name: "state mutex",
			label: "state mutex",
			token,
			lockDir,
			mode: "mutex"
		} );
		await writeStateFile( mutexOwnerPath, record );
		return {
			async release() {
				const current = await readStateFile( mutexOwnerPath );
				if ( current?.record?.token === token ) {
					await rm( mutexDir, { recursive: true, force: true } );
				}
			}
		};
	}
}

/*
================
refreshOwnSummary

Keeps the owner.json reader summary's heartbeat fresh while it names this
reader; another reader's summary is theirs to maintain.
================
*/
async function refreshOwnSummary( ownerPath, token ) {
	const current = await readStateFile( ownerPath );
	const record = current?.record;
	if ( record?.token === token && record.mode === "read" ) {
		await writeStateFile( ownerPath, {
			...record,
			heartbeatAt: new Date().toISOString()
		} );
	}
}

/*
================
readStateFile
================
*/
async function readStateFile( filePath ) {
	let metadata;
	try {
		metadata = await stat( filePath );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	}
	try {
		const text = await readFile( filePath, "utf8" );
		return { record: JSON.parse( text ), mtimeMs: metadata.mtimeMs };
	} catch {
		return { record: undefined, mtimeMs: metadata.mtimeMs };
	}
}

/*
================
writeStateFile

Atomic publish (write-temp-then-rename) so contenders never read a partial
record, exactly as the single-owner protocol published owner.json.
================
*/
async function writeStateFile( filePath, record ) {
	await mkdir( path.dirname( filePath ), { recursive: true } );
	const temporary = `${filePath}.${record.token}.${Date.now()}.tmp`;
	try {
		await writeFile( temporary, `${JSON.stringify( record, null, 2 )}\n`, "utf8" );
		await rename( temporary, filePath );
	} finally {
		await rm( temporary, { force: true } );
	}
}

/*
================
ownerRecord
================
*/
function ownerRecord( { name, label, token, lockDir, mode } ) {
	const now = new Date().toISOString();
	return {
		name,
		label,
		token,
		pid: process.pid,
		ppid: process.ppid,
		user: os.userInfo().username,
		host: os.hostname(),
		cwd: process.cwd(),
		command: process.argv.join( " " ),
		lockDir,
		startedAt: now,
		heartbeatAt: now,
		mode
	};
}

/*
================
readOwner
================
*/
async function readOwner( lockDir ) {
	try {
		const text = await readFile( path.join( lockDir, "owner.json" ), "utf8" );
		return JSON.parse( text );
	} catch {
		return undefined;
	}
}

/*
================
isProcessAlive
================
*/
function isProcessAlive( pid ) {
	if ( pid === process.pid ) return true;
	try {
		process.kill( pid, 0 );
		return true;
	} catch ( error ) {
		return error?.code === "EPERM";
	}
}

/*
================
formatOwner
================
*/
export function formatOwner( owner ) {
	if ( !owner ) {
		return "another process with no owner metadata";
	}
	const pieces = [
		owner.label ?? owner.name ?? "another rebuild",
		owner.pid ? `pid ${owner.pid}` : undefined,
		owner.mode ? `${owner.mode}` : undefined,
		owner.user && owner.host ? `${owner.user}@${owner.host}` : undefined,
		owner.startedAt ? `started ${owner.startedAt}` : undefined
	].filter( Boolean );
	const command = owner.command ? `, command: ${truncate( owner.command, 180 )}` : "";
	return `${pieces.join( ", " )}${command}`;
}

/*
================
normalizeLockName
================
*/
export function normalizeLockName( name ) {
	const normalized = String( name ).trim().toLowerCase().replace( /[^a-z0-9._-]+/g, "-" );
	return normalized || "default";
}

/*
================
normalizeMode
================
*/
export function normalizeMode( mode ) {
	if ( mode === undefined || mode === null ) return "write";
	if ( mode === "read" || mode === "write" ) return mode;
	throw new Error( `[rebuild-lock] unknown lock mode "${mode}" (expected "read" or "write").` );
}

/*
================
rebuildLockDirectory
================
*/
export async function rebuildLockDirectory( name ) {
	let root = locksRoot;
	if ( name === GENERATED_ASSETS_LOCK_NAME ) {
		// The lock lives beside the tree it guards: a worktree that reads the main
		// checkout's build (SRO_GENERATED_ROOT, or a junction) contends with its
		// publisher instead of locking an empty tree of its own.
		let generated = GENERATED_ROOT;
		try {
			generated = await realpath( generated );
		} catch ( error ) {
			if ( error?.code !== "ENOENT" ) throw error;
		}
		root = path.join( path.dirname( generated ), ".state", "locks" );
	}
	return path.join( root, `${name}.lock` );
}

/*
================
relativeLockPath
================
*/
function relativeLockPath( lockDir ) {
	return path.relative( rebuildRoot, lockDir ).replaceAll( "\\", "/" );
}

/*
================
truncate
================
*/
export function truncate( value, maxLength ) {
	return value.length > maxLength ? `${value.slice( 0, maxLength - 3 )}...` : value;
}

/*
================
sleep
================
*/
function sleep( ms ) {
	return new Promise( ( resolve ) => setTimeout( resolve, ms ) );
}

/*
================
numberFromEnv
================
*/
export function numberFromEnv( name, fallback ) {
	const parsed = Number( process.env[name] );
	return Number.isFinite( parsed ) ? parsed : fallback;
}

if ( import.meta.url === pathToFileURL( process.argv[1] ?? "" ).href ) {
	process.exitCode = await runCommandUnderLock( process.argv.slice( 2 ) );
}
