/*
===========================================================================

heavyLock.mjs - the machine-wide heavy lock, spoken with workflow/heavy.py

The agents share one PC. A handful of jobs can push it out of memory when
two run at once (the full client check among them), so they take
workflow/state/heavy.lock by exclusive create and wait for each other.
heavy.py wraps agent-run commands, but nothing made an agent remember the
wrapper (#2766): the full client check now takes the same lock itself,
through this module, in the same on-disk protocol heavy.py speaks:

- the lock is one JSON file { owner, pid, since, cmd }, created O_EXCL;
- a holder whose pid is gone is stale and the next waiter reclaims it;
- a live heavy.priority record makes ordinary jobs stand back;
- a lock already held by this process or an ancestor (heavy.py ran the
  check) is not taken again - the ancestor's hold already serializes.

Where no workflow checkout sits beside the repo (CI, a plain clone) there
is no lock and the caller runs at full parallelism. SRO_HEAVY_LOCK points
at another lock file (tests); SRO_HEAVY_LOCK=off disables the lock.

===========================================================================
*/

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { resolveMainCheckout } from "../lib/generatedRoot.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const repoRoot = path.resolve( scriptDir, "..", ".." );
const DEFAULT_POLL_MS = 2000;
const MAX_ANCESTOR_HOPS = 64;

/*
================
heavyLockTarget

Where this machine's heavy lock lives, or null when locking is off:
SRO_HEAVY_LOCK=off, or no workflow state directory beside the main
checkout (CI). An explicit SRO_HEAVY_LOCK path always names a lock.
================
*/
export function heavyLockTarget( env = process.env, baseRoot = repoRoot ) {
	const override = env.SRO_HEAVY_LOCK;
	if ( override === "off" ) {
		return null;
	}
	if ( override ) {
		const lockPath = path.resolve( override );
		return { lockPath, priorityPath: path.join( path.dirname( lockPath ), "heavy.priority" ) };
	}
	// The workflow lives beside the main checkout, not beside a linked
	// worktree: resolve through the worktree's .git file like every other
	// shared-tree resolver, so agents in worktrees lock the same file.
	let mainCheckout = baseRoot;
	try {
		mainCheckout = resolveMainCheckout( baseRoot );
	} catch {
		// An unreadable worktree link names nothing; lock beside the checkout.
	}
	const stateDir = path.join( mainCheckout, "..", "workflow", "state" );
	if ( !fs.existsSync( stateDir ) ) {
		return null;
	}
	return {
		lockPath: path.join( stateDir, "heavy.lock" ),
		priorityPath: path.join( stateDir, "heavy.priority" )
	};
}

/*
================
pidAlive

True while the process exists. Signal 0 probes without delivering; EPERM
means the process is there but owned by someone else.
================
*/
export function pidAlive( pid ) {
	if ( !Number.isInteger( pid ) || pid <= 0 ) {
		return false;
	}
	if ( pid === process.pid ) {
		return true;
	}
	try {
		process.kill( pid, 0 );
		return true;
	} catch ( error ) {
		return error?.code === "EPERM";
	}
}

let parentMapCache = null;

/*
================
parentMap

Every process's parent, read in one pass and cached: one CIM query for
the whole table on Windows (wmic is gone from current builds, and a
PowerShell spawn per level costs seconds on a loaded machine), /proc on
POSIX (comm sits in parens and may itself contain parens; ppid is the
second field after the closing paren). A process's ancestors never
change while it lives, so the cache cannot go stale for its one caller.
================
*/
function parentMap() {
	if ( parentMapCache ) {
		return parentMapCache;
	}
	const map = new Map();
	if ( process.platform === "win32" ) {
		const out = spawnSync(
			"powershell",
			[
				"-NoProfile",
				"-NonInteractive",
				"-Command",
				'Get-CimInstance Win32_Process | ForEach-Object { "$($_.ProcessId) $($_.ParentProcessId)" }'
			],
			{ encoding: "utf8", windowsHide: true, maxBuffer: 16 * 1024 * 1024 }
		);
		for ( const line of String( out.stdout ?? "" ).split( /\r?\n/ ) ) {
			const [pid, parent] = line.trim().split( " " ).map( ( part ) => Number.parseInt( part, 10 ) );
			if ( Number.isInteger( pid ) && pid > 0 ) {
				map.set( pid, Number.isInteger( parent ) && parent > 0 ? parent : 0 );
			}
		}
	} else {
		for ( const entry of fs.readdirSync( "/proc" ) ) {
			const pid = Number.parseInt( entry, 10 );
			if ( !Number.isInteger( pid ) || pid <= 0 ) {
				continue;
			}
			try {
				const stat = fs.readFileSync( `/proc/${pid}/stat`, "utf8" );
				const fields = stat.slice( stat.lastIndexOf( ")" ) + 2 ).split( " " );
				const parent = Number.parseInt( fields[1], 10 );
				map.set( pid, Number.isInteger( parent ) && parent > 0 ? parent : 0 );
			} catch {
				// The process left between the listing and the read.
			}
		}
	}
	parentMapCache = map;
	return map;
}

/*
================
parentPidOf

The parent of any process, or 0 when it cannot be read.
================
*/
export function parentPidOf( pid ) {
	if ( !Number.isInteger( pid ) || pid <= 0 ) {
		return 0;
	}
	return parentMap().get( pid ) ?? 0;
}

/*
================
ownAncestorPids

This process and its ancestors, walked per acquisition over the cached
process table (parentMap pays the OS read; the walk itself is map
lookups). A chain that revisits a pid proves the table was poisoned by
pid reuse (#3206): every parent is older than its child, so a live
chain cannot cycle - what happened is an ancestor died and a younger
process (in the worst case the lock's own holder, spawned under this
process) took its pid. No pid from a cycling chain may be called an
ancestor; the walk answers an empty set, uncached, so each acquisition
judges the table it actually sees.
================
*/
function ownAncestorPids( parentOf ) {
	const set = new Set();
	let current = process.pid;
	for ( let hops = 0; hops < MAX_ANCESTOR_HOPS && current > 0; hops++ ) {
		if ( set.has( current ) ) {
			return new Set();
		}
		set.add( current );
		current = parentOf( current );
	}
	return set;
}

/*
================
isAncestorPid

True when pid is this process or one of its ancestors: the lock it holds
already serializes this run (heavy.py ran the check), so taking the lock
again would wait on ourselves forever. False whenever the chain cycles:
a reused pid once let a waiter take a live holder's lock (#3206).
================
*/
export function isAncestorPid( pid, parentOf = parentPidOf ) {
	return ownAncestorPids( parentOf ).has( pid );
}

/*
================
readRecord
================
*/
function readRecord( filePath ) {
	try {
		return JSON.parse( fs.readFileSync( filePath, "utf8" ) );
	} catch {
		return null;
	}
}

/*
================
clearIfStale

Remove a holder file whose process is gone (or that cannot be read),
exactly as heavy.py reclaims: the exclusive create after it arbitrates
between waiters that cleared at the same time.
================
*/
function clearIfStale( filePath ) {
	const held = readRecord( filePath );
	if ( held === null || !pidAlive( Number( held.pid ) ) ) {
		try {
			fs.unlinkSync( filePath );
		} catch {
			// Another waiter reclaimed first; the create below arbitrates.
		}
	}
}

/*
================
priorityWaiting

The live priority record ordinary jobs stand back for, or null (a stale
one is cleared), mirroring heavy.py's priority_waiting.
================
*/
function priorityWaiting( target ) {
	if ( !fs.existsSync( target.priorityPath ) ) {
		return null;
	}
	clearIfStale( target.priorityPath );
	const held = readRecord( target.priorityPath );
	if ( held === null || Number( held.pid ) === process.pid ) {
		return null;
	}
	return held;
}

/*
================
createExclusive

Create the lock file exclusively with the holder record; false when held.
================
*/
function createExclusive( lockPath, record ) {
	let fd;
	try {
		fd = fs.openSync( lockPath, "wx" );
	} catch ( error ) {
		if ( error?.code === "EEXIST" ) {
			return false;
		}
		throw error;
	}
	try {
		fs.writeFileSync( fd, JSON.stringify( record ) );
	} finally {
		fs.closeSync( fd );
	}
	return true;
}

/*
================
tryTake

One acquisition attempt, heavy.py's order: stand back for a waiting
priority job, try the exclusive create, then clear a stale holder so the
next attempt can take it.
================
*/
function tryTake( target, record ) {
	if ( priorityWaiting( target ) ) {
		return false;
	}
	if ( createExclusive( target.lockPath, record ) ) {
		return true;
	}
	clearIfStale( target.lockPath );
	return false;
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
acquireHeavyLock

Take the heavy lock, waiting (and saying so once) while another job holds
it. Returns a hold with release(), or null when this machine has no lock
(no workflow beside the checkout, or SRO_HEAVY_LOCK=off) and the caller
runs unlocked. A lock held by this process or an ancestor returns a hold
whose release is a no-op: the ancestor owns the lock's lifetime.
================
*/
export async function acquireHeavyLock( options = {} ) {
	const target = options.target ?? heavyLockTarget( options.env ?? process.env );
	if ( !target ) {
		return null;
	}
	const log = options.log ?? (( line ) => console.error( line ));
	const held = readRecord( target.lockPath );
	const heldPid = held ? Number( held.pid ) : 0;
	if ( held && pidAlive( heldPid ) && isAncestorPid( heldPid, options.parentOf ?? parentPidOf ) ) {
		log( `heavy: lock already held by ${held.owner ?? "an ancestor"} (pid ${held.pid}); running under it` );
		return { release() {} };
	}
	const record = {
		owner: options.owner ?? "client-check",
		pid: process.pid,
		since: new Date().toISOString().replace( /\.\d{3}Z$/, "Z" ),
		cmd: options.cmd ?? ""
	};
	const pollMs = options.pollMs ?? DEFAULT_POLL_MS;
	let waited = false;
	while ( !tryTake( target, record ) ) {
		if ( !waited ) {
			const blocker = readRecord( target.lockPath );
			const priority = priorityWaiting( target );
			if ( blocker ) {
				log(
					`heavy: waiting for ${blocker.owner ?? "?"} (${String( blocker.cmd ?? "" ).slice( 0, 80 )}) since ${
						blocker.since ?? "?"
					}`
				);
			} else if ( priority ) {
				log( `heavy: waiting for a priority job (${priority.owner ?? "?"})` );
			} else {
				log( "heavy: waiting for the heavy lock" );
			}
			waited = true;
		}
		await sleep( pollMs );
	}
	if ( waited ) {
		log( `heavy: lock taken by ${record.owner}` );
	}
	let released = false;
	const release = () => {
		if ( released ) {
			return;
		}
		released = true;
		const current = readRecord( target.lockPath );
		if ( current && Number( current.pid ) === process.pid ) {
			try {
				fs.unlinkSync( target.lockPath );
			} catch {
				// Already gone; nothing to release.
			}
		}
	};
	// A graceful exit (including SIGTERM) drops the lock at once; a hard
	// kill leaves it for the next waiter's dead-pid reclaim.
	process.once( "exit", release );
	return {
		release() {
			process.off( "exit", release );
			release();
		}
	};
}

/*
================
withHeavyLock

Run fn under the heavy lock when this machine has one; run it directly
when it does not.
================
*/
export async function withHeavyLock( options, fn ) {
	const hold = await acquireHeavyLock( options );
	if ( !hold ) {
		return fn();
	}
	try {
		return await fn();
	} finally {
		hold.release();
	}
}
