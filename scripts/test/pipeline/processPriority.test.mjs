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
function childPriority( env, before = "" ) {
	const script = `
		const { yieldCpu } = await import( ${JSON.stringify( "file://" + helper.replace( /\\/g, "/" ) )} );
		const { spawnSync } = await import( "node:child_process" );
		const os = await import( "node:os" );
		${before}
		yieldCpu();
		const grandchild = spawnSync( process.execPath, [ "-e", "process.stdout.write( String( require( 'node:os' ).getPriority() ) )" ], { encoding: "utf8" } );
		process.stdout.write( JSON.stringify( { self: os.getPriority(), child: Number( grandchild.stdout ) } ) );
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

test("SRO_CHECK_PRIORITY=normal leaves the inherited priority alone", () => {
	// The runner itself may already run below normal (a background shell);
	// opting out must change nothing, not raise anything.
	const { self } = childPriority( { SRO_CHECK_PRIORITY: "normal" } );
	assert.equal( self, os.getPriority() );
});

test("a check already below normal is never raised", () => {
	const { self } = childPriority(
		{ SRO_CHECK_PRIORITY: "" },
		"os.setPriority( os.constants.priority.PRIORITY_LOW );"
	);
	assert.equal( self, os.constants.priority.PRIORITY_LOW );
});

test("run_if_changed starts its gate below normal", () => {
	// A unique gate name, forced past any stamp; its pass stamp is removed.
	const name = `process-priority-test-${process.pid}`;
	// The wrapper runs its gate through a shell: a script path without
	// spaces and the PATH's node keep the command line plain.
	const scratch = mkdtempSync( path.join( os.tmpdir(), "priority-" ) );
	const gate = path.join( scratch, "gate.cjs" );
	writeFileSync( gate, "process.stdout.write( 'PRIORITY=' + require( 'node:os' ).getPriority() );\n" );
	try {
		const run = spawnSync(
			process.execPath,
			[ wrapper, name, "scripts/lib/processPriority.mjs", "--", "node", gate ],
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
