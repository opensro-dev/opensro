/*
===========================================================================

processPriority.test.mjs - a check and everything it spawns yield the CPU

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { yieldCpu } from "../../lib/processPriority.mjs";

const helper = fileURLToPath( new URL( "../../lib/processPriority.mjs", import.meta.url ) );
const repository = fileURLToPath( new URL( "../../../", import.meta.url ) );
const wrapper = path.join( repository, "scripts", "checks", "run_if_changed.mjs" );

/*
================
childPriority

Runs a fresh node that calls yieldCpu (or not) and then spawns a
grandchild, and returns both processes' priorities.
================
*/
// Every child starts from normal, so what yieldCpu does is what is seen,
// whatever priority the test runner itself has.
const FROM_NORMAL = "try { os.setPriority( os.constants.priority.PRIORITY_NORMAL ); } catch {}";

function childPriority( env, before = FROM_NORMAL ) {
	const script = `
		const { yieldCpu } = await import( ${JSON.stringify( "file://" + helper.replace( /\\/g, "/" ) )} );
		const { spawnSync } = await import( "node:child_process" );
		const os = await import( "node:os" );
		${before}
		const start = os.getPriority();
		yieldCpu();
		const grandchild = spawnSync( process.execPath, [ "-e", "process.stdout.write( String( require( 'node:os' ).getPriority() ) )" ], { encoding: "utf8" } );
		process.stdout.write( JSON.stringify( { start, self: os.getPriority(), child: Number( grandchild.stdout ) } ) );
	`;
	const run = spawnSync( process.execPath, [ "--input-type=module", "-e", script ], {
		encoding: "utf8",
		env: { ...process.env, ...env }
	} );
	assert.equal( run.status, 0, run.stderr );
	return JSON.parse( run.stdout );
}

test("a check lowers its priority and its children inherit it", () => {
	const { self, child } = childPriority( { SRO_CHECK_PRIORITY: "" } );
	assert.ok( self >= os.constants.priority.PRIORITY_BELOW_NORMAL, `self ${self}` );
	assert.ok( child >= os.constants.priority.PRIORITY_BELOW_NORMAL, `child ${child}` );
});

/*
================
fakePriority

A process priority the test owns (#1651): the live one can be lowered by
other actors on a shared machine mid-test, so the decision tests use this.
================
*/
function fakePriority( start, refuse = false ) {
	const calls = [];
	let current = start;
	return {
		calls,
		constants: os.constants,
		getPriority: () => current,
		setPriority: value => {
			if ( refuse ) throw new Error( "EACCES" );
			calls.push( value );
			current = value;
		}
	};
}

test("SRO_CHECK_PRIORITY=normal leaves the priority alone", () => {
	const priority = fakePriority( os.constants.priority.PRIORITY_NORMAL );
	assert.equal( yieldCpu( { SRO_CHECK_PRIORITY: " Normal " }, priority ), null );
	assert.deepEqual( priority.calls, [] );
});

test("a check at normal lowers itself to below normal", () => {
	const priority = fakePriority( os.constants.priority.PRIORITY_NORMAL );
	assert.equal( yieldCpu( {}, priority ), os.constants.priority.PRIORITY_BELOW_NORMAL );
	assert.deepEqual( priority.calls, [ os.constants.priority.PRIORITY_BELOW_NORMAL ] );
});

test("a check already below normal is never raised", () => {
	const priority = fakePriority( os.constants.priority.PRIORITY_LOW );
	assert.equal( yieldCpu( {}, priority ), os.constants.priority.PRIORITY_LOW );
	assert.deepEqual( priority.calls, [] );
});

test("a platform that refuses the change never fails the check", () => {
	assert.equal( yieldCpu( {}, fakePriority( os.constants.priority.PRIORITY_NORMAL, true ) ), null );
});

test("run_if_changed starts its gate below normal", t => {
	// A runner already below normal (a background shell) would hand the
	// gate a low priority whether or not the wrapper lowers it: launch the
	// wrapper from a parent raised to normal, or skip where that is refused.
	const launcher = spawnSync(
		process.execPath,
		[
			"-e",
			"const os=require('node:os');try{os.setPriority(os.constants.priority.PRIORITY_NORMAL)}catch{};process.stdout.write(String(os.getPriority()))"
		],
		{ encoding: "utf8" }
	);
	if ( Number( launcher.stdout ) !== os.constants.priority.PRIORITY_NORMAL ) {
		t.skip( "this runner cannot start a normal-priority process" );
		return;
	}
	// A unique gate name, forced past any stamp; its pass stamp is removed.
	const name = `process-priority-test-${process.pid}`;
	// The wrapper runs its gate through a shell: a script path without
	// spaces and the PATH's node keep the command line plain.
	const scratch = mkdtempSync( path.join( os.tmpdir(), "priority-" ) );
	const gate = path.join( scratch, "gate.cjs" );
	writeFileSync( gate, "process.stdout.write( 'PRIORITY=' + require( 'node:os' ).getPriority() );\n" );
	try {
		// The launcher raises itself to normal, then runs the wrapper, so the
		// gate's low priority can only come from the wrapper's yieldCpu.
		const launch = "const os=require('node:os'),cp=require('node:child_process');" +
			"os.setPriority(os.constants.priority.PRIORITY_NORMAL);" +
			"const r=cp.spawnSync(process.execPath,JSON.parse(process.argv[1]),{encoding:'utf8',cwd:process.argv[2]});" +
			"process.stdout.write(r.stdout||'');process.stderr.write(r.stderr||'');process.exit(r.status??1)";
		const run = spawnSync(
			process.execPath,
			[
				"-e",
				launch,
				JSON.stringify( [ wrapper, name, "scripts/lib/processPriority.mjs", "--", "node", gate ] ),
				repository
			],
			{ cwd: repository, encoding: "utf8", env: { ...process.env, SRO_CHECK_FORCE: "1", SRO_CHECK_PRIORITY: "" } }
		);
		assert.equal( run.status, 0, run.stderr );
		const printed = Number( /PRIORITY=(-?\d+)/.exec( run.stdout )?.[1] );
		assert.ok( printed >= os.constants.priority.PRIORITY_BELOW_NORMAL, `gate priority ${printed}: ${run.stdout}` );
	} finally {
		rmSync( path.join( repository, ".state", "check-stamps", `${name}.json` ), { force: true } );
		rmSync( scratch, { recursive: true, force: true } );
	}
});
