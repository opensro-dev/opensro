/*
===========================================================================

heavyLock.test.mjs - the machine-wide heavy lock the client check takes

Real child processes hold a lock in a temp directory while this process
contends for it: the second acquirer waits, a dead holder's lock is
reclaimed, a killed holder's lock is reclaimed, a live priority record
makes an ordinary acquirer stand back, and a lock held by this very
process (heavy.py ran us) is never taken twice.
===========================================================================
*/

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";

import { acquireHeavyLock, heavyLockTarget, isAncestorPid, pidAlive } from "../../checks/heavyLock.mjs";

const MODULE_URL = pathToFileURL( path.resolve( import.meta.dirname, "../../checks/heavyLock.mjs" ) ).href;
// A child that takes the named lock, says so, and releases on stdin data.
const HOLDER_SOURCE = `
import { acquireHeavyLock } from ${JSON.stringify( MODULE_URL )};
const target = { lockPath: process.env.LOCK_PATH, priorityPath: process.env.PRIORITY_PATH };
const hold = await acquireHeavyLock( { target, owner: "holder", pollMs: 25, log: () => {} } );
console.log( "ACQUIRED" );
process.stdin.once( "data", () => {
	hold.release();
	console.log( "RELEASED" );
} );
`;

/*
================
tempTarget

A lock target under a fresh temp directory, removed with the test.
================
*/
async function tempTarget( t ) {
	const dir = await mkdtemp( path.join( os.tmpdir(), "heavy-lock-" ) );
	t.after( () => rm( dir, { recursive: true, force: true } ) );
	return {
		dir,
		target: {
			lockPath: path.join( dir, "heavy.lock" ),
			priorityPath: path.join( dir, "heavy.priority" )
		}
	};
}

/*
================
spawnHolder

Spawn the holder child and resolve once it prints ACQUIRED.
================
*/
async function spawnHolder( t, dir, target ) {
	const driver = path.join( dir, "holder.mjs" );
	await writeFile( driver, HOLDER_SOURCE );
	const child = spawn( process.execPath, [ driver ], {
		env: { ...process.env, LOCK_PATH: target.lockPath, PRIORITY_PATH: target.priorityPath },
		stdio: [ "pipe", "pipe", "inherit" ],
		windowsHide: true
	} );
	t.after( () => {
		if ( child.exitCode === null ) child.kill();
	} );
	await new Promise( ( resolve, reject ) => {
		child.stdout.on( "data", ( chunk ) => {
			if ( String( chunk ).includes( "ACQUIRED" ) ) resolve();
		} );
		child.on( "exit", () => reject( new Error( "holder exited before acquiring" ) ) );
	} );
	return child;
}

/*
================
deadPid

A pid that named a process until a moment ago.
================
*/
async function deadPid() {
	const child = spawn( process.execPath, [ "-e", "" ], { stdio: "ignore", windowsHide: true } );
	await new Promise( ( resolve ) => child.on( "exit", resolve ) );
	return child.pid;
}

/*
================
waitFor
================
*/
async function waitFor( predicate, timeoutMs = 5000 ) {
	const deadline = Date.now() + timeoutMs;
	while ( Date.now() < deadline ) {
		if ( predicate() ) return;
		await new Promise( ( resolve ) => setTimeout( resolve, 10 ) );
	}
	throw new Error( "waitFor timed out" );
}

test("a second acquirer waits for the holder, then takes the lock", async ( t ) => {
	const { dir, target } = await tempTarget( t );
	const holder = await spawnHolder( t, dir, target );
	const lines = [];
	let acquired = false;
	const pending = acquireHeavyLock( { target, owner: "waiter", pollMs: 25, log: ( line ) => lines.push( line ) } )
		.then( ( hold ) => {
			acquired = true;
			return hold;
		} );
	await new Promise( ( resolve ) => setTimeout( resolve, 300 ) );
	assert.equal( acquired, false, "the waiter took a held lock" );
	assert.ok( lines.some( ( line ) => line.includes( "waiting for holder" ) ), "the wait was not reported" );
	holder.stdin.write( "go\n" );
	const hold = await pending;
	assert.equal( acquired, true );
	assert.equal( JSON.parse( fs.readFileSync( target.lockPath, "utf8" ) ).owner, "waiter" );
	hold.release();
	assert.equal( fs.existsSync( target.lockPath ), false, "release left the lock behind" );
});

test("a lock whose pid is dead is reclaimed", async ( t ) => {
	const { target } = await tempTarget( t );
	const ghost = await deadPid();
	assert.equal( pidAlive( ghost ), false );
	await writeFile( target.lockPath, JSON.stringify( { owner: "ghost", pid: ghost, since: "?", cmd: "gone" } ) );
	const hold = await acquireHeavyLock( { target, pollMs: 25, log: () => {} } );
	assert.equal( JSON.parse( fs.readFileSync( target.lockPath, "utf8" ) ).pid, process.pid );
	hold.release();
});

test("a killed holder leaves a lock the next acquirer reclaims", async ( t ) => {
	const { dir, target } = await tempTarget( t );
	const holder = await spawnHolder( t, dir, target );
	holder.kill( "SIGKILL" );
	await new Promise( ( resolve ) => holder.on( "exit", resolve ) );
	assert.equal( fs.existsSync( target.lockPath ), true, "the hard kill should leave the lock file" );
	const hold = await acquireHeavyLock( { target, pollMs: 25, log: () => {} } );
	hold.release();
	assert.equal( fs.existsSync( target.lockPath ), false );
});

test("a live priority record makes an ordinary acquirer stand back", async ( t ) => {
	const { target } = await tempTarget( t );
	const sleeper = spawn( process.execPath, [ "-e", "setTimeout( () => {}, 60000 )" ], {
		stdio: "ignore",
		windowsHide: true
	} );
	t.after( () => {
		if ( sleeper.exitCode === null ) sleeper.kill();
	} );
	await writeFile(
		target.priorityPath,
		JSON.stringify( { owner: "priority job", pid: sleeper.pid, since: "?", cmd: "release" } )
	);
	const lines = [];
	let acquired = false;
	const pending = acquireHeavyLock( { target, owner: "ordinary", pollMs: 25, log: ( line ) => lines.push( line ) } )
		.then( ( hold ) => {
			acquired = true;
			return hold;
		} );
	await new Promise( ( resolve ) => setTimeout( resolve, 300 ) );
	assert.equal( acquired, false, "the ordinary job jumped the priority lane" );
	assert.ok( lines.some( ( line ) => line.includes( "priority" ) ), "the stand-back was not reported" );
	sleeper.kill();
	const hold = await pending;
	hold.release();
});

test("a lock held by this process is not taken again", async ( t ) => {
	const { target } = await tempTarget( t );
	await writeFile(
		target.lockPath,
		JSON.stringify( { owner: "ancestor", pid: process.pid, since: "?", cmd: "pnpm check" } )
	);
	const hold = await acquireHeavyLock( { target, pollMs: 25, log: () => {} } );
	hold.release();
	assert.equal( fs.existsSync( target.lockPath ), true, "the ancestor's lock was released from under it" );
	await rm( target.lockPath, { force: true } );
});

test("isAncestorPid walks the parent chain", () => {
	const chain = new Map( [ [ process.pid, 111 ], [ 111, 123 ], [ 123, 0 ] ] );
	const parentOf = ( pid ) => chain.get( pid ) ?? 0;
	assert.equal( isAncestorPid( process.pid, parentOf ), true );
	assert.equal( isAncestorPid( 123, parentOf ), true );
	assert.equal( isAncestorPid( 999, parentOf ), false );
});

test("isAncestorPid trusts no chain that cycles (a reused pid)", () => {
	// An ancestor died and its pid now names the holder, a child of this
	// process: the chain from here runs 111 -> holder -> back to here.
	// A live chain never revisits a pid (every parent is older than its
	// child), so the cycle proves the table was poisoned by pid reuse
	// and no pid on it may be called an ancestor (#3206).
	const holderPid = 4242;
	const chain = new Map( [ [ process.pid, 111 ], [ 111, holderPid ], [ holderPid, process.pid ] ] );
	const parentOf = ( pid ) => chain.get( pid ) ?? 0;
	assert.equal( isAncestorPid( holderPid, parentOf ), false );
	assert.equal( isAncestorPid( 111, parentOf ), false );
});

test("a holder whose pid a dead ancestor's pid was reused for is waited on (#3206)", async ( t ) => {
	const { target } = await tempTarget( t );
	const sleeper = spawn( process.execPath, [ "-e", "setTimeout( () => {}, 60000 )" ], {
		stdio: "ignore",
		windowsHide: true
	} );
	t.after( () => {
		if ( sleeper.exitCode === null ) sleeper.kill();
	} );
	assert.ok( sleeper.pid );
	// The record names a live stranger, but the parent table - poisoned
	// by the reuse above - calls it this process's ancestor.
	await writeFile(
		target.lockPath,
		JSON.stringify( { owner: "holder", pid: sleeper.pid, since: "?", cmd: "heavy" } )
	);
	const chain = new Map( [ [ process.pid, 111 ], [ 111, sleeper.pid ], [ sleeper.pid, process.pid ] ] );
	const parentOf = ( pid ) => chain.get( pid ) ?? 0;
	let acquired = false;
	const pending = acquireHeavyLock( { target, owner: "waiter", pollMs: 25, log: () => {}, parentOf } )
		.then( ( hold ) => {
			acquired = true;
			return hold;
		} );
	await new Promise( ( resolve ) => setTimeout( resolve, 300 ) );
	assert.equal( acquired, false, "the waiter took a live stranger for an ancestor and took its lock" );
	sleeper.kill();
	const hold = await pending;
	assert.equal( acquired, true, "the dead holder's lock was not reclaimed" );
	hold.release();
});

test("heavyLockTarget follows the workflow beside the checkout", async ( t ) => {
	const { dir } = await tempTarget( t );
	const checkout = path.join( dir, "rebuild" );
	await mkdir( checkout, { recursive: true } );
	assert.equal( heavyLockTarget( {}, checkout ), null, "no workflow beside the checkout should mean no lock" );
	await mkdir( path.join( dir, "workflow", "state" ), { recursive: true } );
	const target = heavyLockTarget( {}, checkout );
	assert.equal( target.lockPath, path.join( dir, "workflow", "state", "heavy.lock" ) );
	assert.equal( target.priorityPath, path.join( dir, "workflow", "state", "heavy.priority" ) );
	const override = path.join( dir, "elsewhere", "mine.lock" );
	assert.equal( heavyLockTarget( { SRO_HEAVY_LOCK: override }, checkout ).lockPath, override );
	assert.equal( heavyLockTarget( { SRO_HEAVY_LOCK: "off" }, checkout ), null );
});

test("a linked worktree locks beside the main checkout, not beside itself", async ( t ) => {
	const { dir } = await tempTarget( t );
	const hub = path.join( dir, "hub" );
	const worktree = path.join( dir, "worktrees", "wt" );
	await mkdir( worktree, { recursive: true } );
	await mkdir( path.join( dir, "workflow", "state" ), { recursive: true } );
	await writeFile( path.join( worktree, ".git" ), `gitdir: ${path.join( hub, ".git", "worktrees", "wt" )}\n` );
	const target = heavyLockTarget( {}, worktree );
	assert.equal( target.lockPath, path.join( dir, "workflow", "state", "heavy.lock" ) );
});
