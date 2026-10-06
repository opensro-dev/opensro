/*
===========================================================================

rebuildLock.mjs - serialize generated asset publication and verification

Node and Python publishers share physical-tree lock ownership, including
worktrees whose generated directory is a junction into another checkout.

===========================================================================
*/

import { execFile, spawn } from "node:child_process";
import { rmSync } from "node:fs";
import { mkdir, readFile, realpath, rename, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const locksRoot = path.join( rebuildRoot, ".state", "locks" );

const LOCK_NAME_ENV = "SRO_REBUILD_LOCK_NAME";
const LOCK_TOKEN_ENV = "SRO_REBUILD_LOCK_TOKEN";
const LOCK_DIR_ENV = "SRO_REBUILD_LOCK_DIR";
const DEFAULT_POLL_MS = 5000;
const DEFAULT_STALE_MS = 12 * 60 * 60 * 1000;
const HEARTBEAT_MS = 10000;
const OWNER_PUBLICATION_GRACE_MS = 60000;
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
================
*/
export async function withRebuildLock( options, fn ) {
	const name = normalizeLockName( options.name ?? GENERATED_ASSETS_LOCK_NAME );
	const label = options.label ?? name;
	const lockDir = await rebuildLockDirectory( name );
	if (
		process.env[LOCK_NAME_ENV] === name && process.env[LOCK_TOKEN_ENV] &&
		process.env[LOCK_DIR_ENV] === lockDir
	) {
		return fn();
	}

	const timeoutMs = numberFromEnv( "SRO_REBUILD_LOCK_TIMEOUT_MS", options.timeoutMs ?? 0 );
	const pollMs = Math.max( 250, numberFromEnv( "SRO_REBUILD_LOCK_POLL_MS", options.pollMs ?? DEFAULT_POLL_MS ) );
	const staleMs = Math.max(
		60000,
		numberFromEnv( "SRO_REBUILD_LOCK_STALE_MS", options.staleMs ?? DEFAULT_STALE_MS )
	);
	const lock = await acquireRebuildLock( { name, label, timeoutMs, pollMs, staleMs, quiet: options.quiet === true } );
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
		timeoutMs: options.timeoutMs ?? 0,
		pollMs: Math.max( 250, options.pollMs ?? DEFAULT_POLL_MS ),
		staleMs: Math.max( 60000, options.staleMs ?? DEFAULT_STALE_MS ),
		quiet: options.quiet === true
	} );
	const cleanupOnExit = () => {
		try {
			rmSync( lock.lockDir, { recursive: true, force: true } );
		} catch {
			// The next contender validates owner liveness and removes stale state.
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
async function acquireRebuildLock( { name, label, timeoutMs, pollMs, staleMs, quiet = false } ) {
	const lockDir = await rebuildLockDirectory( name );
	const ownerPath = path.join( lockDir, "owner.json" );
	const token = `${process.pid}-${Date.now()}-${Math.random().toString( 16 ).slice( 2 )}`;
	const startedAt = Date.now();
	let nextNoticeAt = 0;

	await mkdir( path.dirname( lockDir ), { recursive: true } );

	for ( ;; ) {
		try {
			await mkdir( lockDir );
			const owner = ownerRecord( { name, label, token, lockDir } );
			await writeOwner( ownerPath, owner );
			const heartbeat = setInterval( () => {
				writeOwner( ownerPath, { ...owner, heartbeatAt: new Date().toISOString() } ).catch( () => {} );
			}, HEARTBEAT_MS );
			heartbeat.unref?.();

			process.env[LOCK_NAME_ENV] = name;
			process.env[LOCK_TOKEN_ENV] = token;
			process.env[LOCK_DIR_ENV] = lockDir;
			if ( !quiet ) {
				console.error(
					`[rebuild-lock] ${label}: acquired ${relativeLockPath( lockDir )} (pid ${process.pid}).`
				);
			}

			return {
				lockDir,
				env: {
					[LOCK_NAME_ENV]: name,
					[LOCK_TOKEN_ENV]: token,
					[LOCK_DIR_ENV]: lockDir
				},
				async release() {
					clearInterval( heartbeat );
					if ( process.env[LOCK_TOKEN_ENV] === token ) {
						delete process.env[LOCK_NAME_ENV];
						delete process.env[LOCK_TOKEN_ENV];
						delete process.env[LOCK_DIR_ENV];
					}
					await rm( lockDir, { recursive: true, force: true } );
					if ( !quiet ) {
						console.error( `[rebuild-lock] ${label}: released ${relativeLockPath( lockDir )}.` );
					}
				}
			};
		} catch ( error ) {
			if ( error?.code !== "EEXIST" ) {
				throw error;
			}
		}

		const owner = await readOwner( lockDir );
		if ( await removeStaleLockIfNeeded( lockDir, owner, staleMs, label, quiet ) ) {
			continue;
		}

		const now = Date.now();
		if ( now >= nextNoticeAt ) {
			if ( !quiet ) {
				console.error(
					`[rebuild-lock] ${label}: waiting for ${formatOwner( owner )} (${relativeLockPath( lockDir )}).`
				);
			}
			nextNoticeAt = now + Math.max( pollMs, 30000 );
		}

		if ( timeoutMs > 0 && now - startedAt >= timeoutMs ) {
			throw new Error( `[rebuild-lock] ${label}: timed out waiting for ${formatOwner( owner )}.` );
		}

		await sleep( pollMs );
	}
}

/*
================
ownerRecord
================
*/
function ownerRecord( { name, label, token, lockDir } ) {
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
		heartbeatAt: now
	};
}

/*
================
writeOwner
================
*/
async function writeOwner( ownerPath, owner ) {
	// Readers must never mistake a truncated heartbeat for an abandoned lock.
	const temporary = `${ownerPath}.${owner.token}.${Date.now()}.tmp`;
	try {
		await writeFile( temporary, `${JSON.stringify( owner, null, 2 )}\n`, "utf8" );
		await rename( temporary, ownerPath );
	} finally {
		await rm( temporary, { force: true } );
	}
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
removeStaleLockIfNeeded
================
*/
async function removeStaleLockIfNeeded( lockDir, owner, staleMs, label, quiet = false ) {
	// mkdir claims the lock before owner.json is published. A partial heartbeat
	// read has the same shape; neither permits stealing a fresh lock.
	if ( !owner ) {
		try {
			const metadata = await stat( lockDir );
			if ( Date.now() - metadata.mtimeMs < OWNER_PUBLICATION_GRACE_MS ) return false;
		} catch ( error ) {
			if ( error?.code === "ENOENT" ) return true;
			throw error;
		}
	}
	const pid = Number( owner?.pid );
	const heartbeatMs = Date.parse( owner?.heartbeatAt ?? owner?.startedAt ?? "" );
	const heartbeatAgeMs = Number.isFinite( heartbeatMs ) ? Date.now() - heartbeatMs : Number.POSITIVE_INFINITY;
	const processIsAlive = Number.isInteger( pid ) && pid > 0 ? isProcessAlive( pid ) : false;

	if ( processIsAlive && heartbeatAgeMs <= staleMs ) {
		return false;
	}

	if ( !processIsAlive || heartbeatAgeMs > staleMs ) {
		const reason = processIsAlive ? `stale heartbeat from pid ${pid}` : `exited pid ${pid || "(unknown)"}`;
		if ( !quiet ) {
			console.error( `[rebuild-lock] ${label}: removing stale lock (${reason}).` );
		}
		await rm( lockDir, { recursive: true, force: true } );
		return true;
	}

	return false;
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
function formatOwner( owner ) {
	if ( !owner ) {
		return "another process with no owner metadata";
	}
	const pieces = [
		owner.label ?? owner.name ?? "another rebuild",
		owner.pid ? `pid ${owner.pid}` : undefined,
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
function normalizeLockName( name ) {
	const normalized = String( name ).trim().toLowerCase().replace( /[^a-z0-9._-]+/g, "-" );
	return normalized || "default";
}

/*
================
rebuildLockDirectory
================
*/
async function rebuildLockDirectory( name ) {
	let root = locksRoot;
	if ( name === GENERATED_ASSETS_LOCK_NAME ) {
		// Junction aliases must contend with the publisher in the owning checkout.
		// Keep the existing location for ordinary checkouts and Python publishers.
		let generated = path.join( rebuildRoot, ".generated" );
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
function truncate( value, maxLength ) {
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
function numberFromEnv( name, fallback ) {
	const parsed = Number( process.env[name] );
	return Number.isFinite( parsed ) ? parsed : fallback;
}

/*
================
runCommandUnderLock
================
*/
async function runCommandUnderLock( args ) {
	const options = parseCliArgs( args );
	if ( options.status ) {
		const owner = await readRebuildLock( options.name );
		if ( owner ) {
			console.log( `${options.name} is locked by ${formatOwner( owner )}.` );
		} else {
			console.log( `${options.name} is not locked.` );
		}
		const relatedProcesses = await findRelatedGeneratedAssetProcesses();
		if ( relatedProcesses.length > 0 ) {
			console.log( "Related generated-asset process(es) are currently running:" );
			for ( const processInfo of relatedProcesses ) {
				console.log( `- pid ${processInfo.pid}: ${truncate( processInfo.commandLine, 220 )}` );
			}
		}
		return 0;
	}

	if ( options.command.length === 0 ) {
		console.error(
			'Usage: node scripts/rebuildLock.mjs [--name generated-assets] [--label "resource build"] -- <command> [args...]'
		);
		return 2;
	}

	return withRebuildLock(
		{ name: options.name, label: options.label, timeoutMs: options.timeoutMs },
		async () => spawnCommand( options.command )
	);
}

/*
================
parseCliArgs
================
*/
function parseCliArgs( args ) {
	/** @type {{ name: string, label: string, timeoutMs: number | undefined, status: boolean, command: string[] }} */
	const options = {
		name: GENERATED_ASSETS_LOCK_NAME,
		label: GENERATED_ASSETS_LOCK_LABEL,
		timeoutMs: undefined,
		status: false,
		command: []
	};

	for ( let index = 0; index < args.length; index += 1 ) {
		const arg = args[index];
		if ( arg === "--" ) {
			options.command = args.slice( index + 1 );
			break;
		}
		if ( arg === "--status" ) {
			options.status = true;
			continue;
		}
		if ( arg === "--name" ) {
			options.name = args[index + 1] ?? options.name;
			index += 1;
			continue;
		}
		if ( arg === "--label" ) {
			options.label = args[index + 1] ?? options.label;
			index += 1;
			continue;
		}
		if ( arg === "--timeout-ms" ) {
			options.timeoutMs = Number( args[index + 1] );
			index += 1;
			continue;
		}
		options.command = args.slice( index );
		break;
	}

	options.name = normalizeLockName( options.name );
	return options;
}

/*
================
spawnCommand
================
*/
function spawnCommand( command ) {
	return new Promise( ( resolve, reject ) => {
		const child = spawn( command[0], command.slice( 1 ), {
			env: process.env,
			stdio: "inherit",
			windowsHide: true
		} );
		child.once( "error", reject );
		child.once( "exit", ( code, signal ) => {
			if ( signal ) {
				console.error( `[rebuild-lock] child exited from signal ${signal}.` );
				resolve( 1 );
				return;
			}
			resolve( code ?? 0 );
		} );
	} );
}

/*
================
findRelatedGeneratedAssetProcesses
================
*/
async function findRelatedGeneratedAssetProcesses() {
	if ( process.platform !== "win32" ) {
		return [];
	}

	const command = [
		"$matches = Get-CimInstance Win32_Process |",
		"Where-Object { $_.CommandLine -match 'build_sro_resources|rebuild_asset_packs_from_public|jsonAssetCompression|convert_images\\.py' } |",
		"Select-Object ProcessId,ParentProcessId,CommandLine;",
		"$matches | ConvertTo-Json -Compress"
	].join( " " );

	try {
		const { stdout } = await execFileCapture( "powershell", [ "-NoProfile", "-Command", command ], {
			timeoutMs: 3000
		} );
		const parsed = stdout.trim() ? JSON.parse( stdout ) : [];
		const rows = Array.isArray( parsed ) ? parsed : [ parsed ];
		return rows
			.map( ( row ) => ({
				pid: Number( row.ProcessId ),
				parentPid: Number( row.ParentProcessId ),
				commandLine: String( row.CommandLine ?? "" )
			}) )
			.filter( ( row ) => Number.isInteger( row.pid ) && row.pid !== process.pid && row.commandLine )
			.filter( ( row ) => !/Get-CimInstance Win32_Process|rebuildLock\.mjs --status/i.test( row.commandLine ) );
	} catch {
		return [];
	}
}

/*
================
execFileCapture
================
*/
function execFileCapture( file, args, { timeoutMs } ) {
	return new Promise( ( resolve, reject ) => {
		const child = execFile( file, args, { windowsHide: true, timeout: timeoutMs }, ( error, stdout, stderr ) => {
			if ( error ) {
				error.stderr = stderr;
				reject( error );
				return;
			}
			resolve( { stdout, stderr } );
		} );
		child.stdin?.end();
	} );
}

if ( import.meta.url === pathToFileURL( process.argv[1] ?? "" ).href ) {
	const exitCode = await runCommandUnderLock( process.argv.slice( 2 ) );
	process.exitCode = exitCode;
}
