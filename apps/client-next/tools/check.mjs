/*
===========================================================================

check.mjs - client verification with explicit source and asset boundaries

`pnpm check` used to run its gates one after another. They are
independent - each reads the sources, the contracts or the generated assets
and writes nothing another gate reads - so they run together here, with the
test run (the longest) started first. Each gate's output is captured; a
failed gate's output is printed whole, and the test runner's timing lines
are printed either way, so a green CI log still carries the per-file
profile (#2929). The
check fails if any gate fails, after all of them have finished. Hosted app-only
builds use --source: types, layout and source tests without retail extraction.
The default remains the complete check, including asset audits. The UI gallery
(tools/ui-gallery) has its own type program, so it cannot rot unseen.

===========================================================================
*/

import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { withHeavyLock } from "../../../scripts/checks/heavyLock.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";
import { checkLane, sourcePart } from "./check-lanes.mjs";
import { publishWarmCaches, seedWarmCaches } from "./warm-cache.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const node = process.execPath;
// its per-file budget failures (printTimingLines, #2929).
const TIMING_PREFIXES = [ "tests:", "tests slowest:", "tests budget:" ];

/*
================
runGate
================
*/
/**
 * @param {string} name
 * @param {string[]} args
 * @returns {Promise<{ name: string, ok: boolean, output: string, seconds: string }>}
 */
function runGate( name, args ) {
	const started = performance.now();
	return new Promise( ( resolve ) => {
		const child = spawn( node, args, { windowsHide: true, cwd: clientRoot } );
		let output = "";
		child.stdout.on( "data", ( chunk ) => {
			output += chunk;
		} );
		child.stderr.on( "data", ( chunk ) => {
			output += chunk;
		} );
		child.on( "error", ( error ) => {
			output += String( error );
		} );
		child.on( "close", ( code ) => {
			const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
			const ok = code === 0;
			process.stdout.write( `${ok ? "[pass ]" : "[FAIL ]"} ${name} in ${seconds}s\n` );
			resolve( { name, ok, output, seconds } );
		} );
	} );
}

/*
================
printTimingLines

The test gate's output is captured, so without this its per-file timings
would only ever surface inside a failed gate's output dump. Print the
runner's timing lines (the run summary, the slowest files, the budget
failures) from every gate that produced them: the client-source CI job's
log then carries the suite profile on green runs too, which is where
docs/agents/client-test-profile.md is measured from (#2929). A failed
gate also gets its whole output below; the repeated lines read as the
summary they are.
================
*/
/**
 * @param {{ name: string, output: string }} result
 */
function printTimingLines( result ) {
	for ( const line of result.output.split( "\n" ) ) {
		if ( TIMING_PREFIXES.some( prefix => line.startsWith( prefix ) ) ) {
			process.stdout.write( `${result.name}: ${line}\n` );
		}
	}
}

/*
================
main
================
*/
async function main() {
	const lane = checkLane( process.argv.slice( 2 ) );
	const gates = lane.gates, sourceOnly = lane.source === true;
	const started = performance.now();
	process.stdout.write(
		`client check (${lane.name}): ${gates.length} gates on ${os.availableParallelism()} cores\n`
	);
	if ( sourceOnly ) {
		process.stdout.write( "Retail audio parity and asset delivery remain required by the full check.\n" );
	}
	// typecheck and typecheck:gallery run tsc directly: seed their
	// buildinfos before the gates start and publish after, as the
	// `typecheck` script and verify-test-types.mjs do for themselves.
	seedWarmCaches();
	const results = await Promise.all( gates.map( ( [name, args] ) => runGate( name, args ) ) );
	publishWarmCaches();
	for ( const result of results ) {
		printTimingLines( result );
	}
	const failed = results.filter( ( result ) => !result.ok );
	for ( const result of failed ) {
		process.stderr.write( `\n--- ${result.name} ---\n${result.output}\n` );
	}
	const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
	if ( failed.length > 0 ) {
		process.stderr.write( `client check: ${failed.length} gate(s) failed in ${seconds}s\n` );
		process.exitCode = 1;
		return;
	}
	process.stdout.write( `client check: PASS (${gates.length} gates, ${seconds}s)\n` );
}

if ( sourcePart( process.argv.slice( 2 ) ) !== undefined ) {
	await main();
} else {
	// The full check is a machine-heavy job: it takes the heavy lock itself
	// (#2766) so two checks on one PC queue instead of running together,
	// whether or not the caller remembered workflow/heavy.py. The --source
	// lane stays unlocked, as it does under heavy.py.
	await withHeavyLock(
		{ owner: "client-check", cmd: "full client check (apps/client-next/tools/check.mjs)" },
		() => withGeneratedAssetsLock( "client verification", main, { mode: "read" } )
	);
}
