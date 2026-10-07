/*
===========================================================================

lockedRun.test.mjs - the shared run lock excludes, queues and always releases

Each test gives the wrapper its own temporary coordination directory and
runs it as a real process, from separate working directories that stand in
for sibling worktrees. The children are tiny node scripts that poll a
release file on a timer, so the tests stay off a busy CPU.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const WRAPPER = fileURLToPath( new URL( "../../coordination/locked-run.mjs", import.meta.url ) );
const EXIT_USAGE = 64, EXIT_HELD = 75, EXIT_PREFLIGHT = 78;

/*
================
coordination

A fresh coordination directory and two "worktrees" to run from.
================
*/
function coordination( t ) {
	const root = mkdtempSync( path.join( os.tmpdir(), "locked-run-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const dir = path.join( root, "coordination" ),
		first = path.join( root, "tree-a" ),
		second = path.join( root, "tree-b" );
	for ( const folder of [ dir, first, second ] ) mkdirSync( folder );
	return { dir, first, second, journal: path.join( dir, "append_only.txt" ) };
}

/*
================
wrapperArgs
================
*/
function wrapperArgs( owner, flags, script ) {
	return [
		WRAPPER,
		"--owner",
		owner,
		"--purpose",
		`${owner} job`,
		"--minutes",
		"1",
		...flags,
		"--",
		process.execPath,
		"-e",
		script
	];
}

/*
================
run

Run the wrapper to completion and return its exit code.
================
*/
function run( dir, cwd, owner, flags, script ) {
	const result = spawnSync( process.execPath, wrapperArgs( owner, flags, script ), {
		cwd,
		env: { ...process.env, SRO_COORDINATION_DIR: dir },
		encoding: "utf8"
	} );
	return result.status;
}

/*
================
start

Start the wrapper in the background; resolves with its exit code.
================
*/
function start( dir, cwd, owner, flags, script ) {
	const child = spawn( process.execPath, wrapperArgs( owner, flags, script ), {
		cwd,
		env: { ...process.env, SRO_COORDINATION_DIR: dir },
		stdio: "ignore"
	} );
	return new Promise( resolve => child.on( "exit", code => resolve( code ) ) );
}

/*
================
waitFor
================
*/
async function waitFor( condition, ms = 10000 ) {
	const deadline = Date.now() + ms;
	while ( !condition() ) {
		if ( Date.now() > deadline ) throw Error( "timed out waiting" );
		await new Promise( resolve => setTimeout( resolve, 25 ) );
	}
}

test("a relative coordination directory is refused, since each worktree would resolve its own", t => {
	const c = coordination( t );
	const marker = path.join( c.first, "ran" );
	const result = spawnSync(
		process.execPath,
		wrapperArgs( "A", [], `require("fs").writeFileSync(${JSON.stringify( marker )}, "ran")` ),
		{ cwd: c.first, env: { ...process.env, SRO_COORDINATION_DIR: "coordination" }, encoding: "utf8" }
	);
	assert.equal( result.status, EXIT_USAGE );
	assert.equal( existsSync( marker ), false, "the child must not start" );
});

test("without a coordination directory nothing runs", () => {
	const marker = path.join( os.tmpdir(), `locked-run-no-dir-${process.pid}` );
	const env = { ...process.env };
	delete env.SRO_COORDINATION_DIR;
	const result = spawnSync(
		process.execPath,
		wrapperArgs( "A", [], `require("fs").writeFileSync(${JSON.stringify( marker )}, "ran")` ),
		{ env, encoding: "utf8" }
	);
	assert.equal( result.status, EXIT_USAGE );
	assert.equal( existsSync( marker ), false, "the child must not start" );
});

test("a held lock refuses a second worktree's run before its child starts", async t => {
	const c = coordination( t );
	const hold = path.join( c.dir, "release-a" ), marker = path.join( c.second, "ran" );
	const first = start(
		c.dir,
		c.first,
		"A",
		[],
		`const h=setInterval(()=>{if(require("fs").existsSync(${JSON.stringify( hold )}))clearInterval(h)},20)`
	);
	await waitFor( () => existsSync( path.join( c.dir, "benchmark.lock" ) ) );
	const status = run( c.dir, c.second, "B", [], `require("fs").writeFileSync(${JSON.stringify( marker )}, "ran")` );
	assert.equal( status, EXIT_HELD );
	assert.equal( existsSync( marker ), false, "the refused child must never start" );
	writeFileSync( hold, "" );
	assert.equal( await first, 0 );
	assert.equal( existsSync( path.join( c.dir, "benchmark.lock" ) ), false, "the holder released the lock" );
	const journal = readFileSync( c.journal, "utf8" );
	assert.match( journal, /\[A\] START A job/ );
	assert.match( journal, /\[A\] END A job .*exit 0.*released/ );
	assert.doesNotMatch( journal, /\[B\] START/ );
});

test("a queued run waits for the holder and then runs", async t => {
	const c = coordination( t );
	const hold = path.join( c.dir, "release-a" ), order = path.join( c.dir, "order" );
	const note = name => `require("fs").appendFileSync(${JSON.stringify( order )}, "${name}")`;
	const first = start(
		c.dir,
		c.first,
		"A",
		[],
		`${note( "a" )};const h=setInterval(()=>{if(require("fs").existsSync(${
			JSON.stringify( hold )
		}))clearInterval(h)},20)`
	);
	await waitFor( () => existsSync( order ) );
	const second = start( c.dir, c.second, "B", [ "--wait", "--estimate", "5" ], note( "b" ) );
	await waitFor( () => existsSync( path.join( c.dir, "benchmark.queue" ) ) );
	writeFileSync( hold, "" );
	assert.equal( await first, 0 );
	assert.equal( await second, 0 );
	assert.equal( readFileSync( order, "utf8" ), "ab", "the waiter ran only after the holder ended" );
	assert.equal( readFileSync( path.join( c.dir, "benchmark.queue" ), "utf8" ), "", "the ticket left the queue" );
});

test("a command that cannot start still releases the lock and records END", t => {
	const c = coordination( t );
	const status = spawnSync(
		process.execPath,
		[
			WRAPPER,
			"--owner",
			"A",
			"--purpose",
			"bad command",
			"--minutes",
			"1",
			"--",
			path.join( c.first, "missing-program" )
		],
		{ cwd: c.first, env: { ...process.env, SRO_COORDINATION_DIR: c.dir }, encoding: "utf8" }
	).status;
	assert.notEqual( status, 0 );
	assert.equal( existsSync( path.join( c.dir, "benchmark.lock" ) ), false, "no lock may be left behind" );
	assert.ok(
		readdirSync( c.dir ).some( name => name.startsWith( "released-a-" ) ),
		"the lock was released, not deleted"
	);
	assert.match( readFileSync( c.journal, "utf8" ), /\[A\] END bad command \(.*failed to start/ );
});

test("an unwritable journal releases the lock and runs nothing", t => {
	const c = coordination( t );
	const marker = path.join( c.first, "ran" );
	const result = spawnSync(
		process.execPath,
		wrapperArgs( "A", [], `require("fs").writeFileSync(${JSON.stringify( marker )}, "ran")` ),
		{
			cwd: c.first,
			// A journal inside a missing directory cannot be appended to.
			env: {
				...process.env,
				SRO_COORDINATION_DIR: c.dir,
				SRO_COORDINATION_JOURNAL: path.join( c.dir, "missing", "log.txt" )
			},
			encoding: "utf8"
		}
	);
	assert.equal( result.status, EXIT_USAGE );
	assert.equal( existsSync( marker ), false, "the child must not start" );
	assert.equal( existsSync( path.join( c.dir, "benchmark.lock" ) ), false, "no lock may be left behind" );
});

test("a failed preflight costs no lock time", t => {
	const c = coordination( t );
	const marker = path.join( c.first, "ran" );
	const status = run(
		c.dir,
		c.first,
		"A",
		[ "--preflight", `"${process.execPath}" -e "process.exit(3)"` ],
		`require("fs").writeFileSync(${JSON.stringify( marker )}, "ran")`
	);
	assert.equal( status, EXIT_PREFLIGHT );
	assert.equal( existsSync( marker ), false );
	assert.equal( existsSync( c.journal ), false, "nothing was journalled or locked" );
});
