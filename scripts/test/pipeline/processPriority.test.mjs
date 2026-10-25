/*
===========================================================================

processPriority.test.mjs - a check and everything it spawns yield the CPU

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import os from "node:os";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const helper = fileURLToPath( new URL( "../../lib/processPriority.mjs", import.meta.url ) );

/*
================
childPriority

Runs a fresh node that calls yieldCpu (or not) and then spawns a
grandchild, and returns both processes' priorities.
================
*/
function childPriority( env ) {
	const script = `
		const { yieldCpu } = await import( ${JSON.stringify( "file://" + helper.replace( /\\/g, "/" ) )} );
		const { spawnSync } = await import( "node:child_process" );
		const os = await import( "node:os" );
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
