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
import { readdirSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { withHeavyLock } from "../../../scripts/checks/heavyLock.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";
import { LIST_PATH as ASSET_BACKED_LIST } from "./list-asset-backed-tests.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const node = process.execPath;
const SOURCE_FLAG = "--source";
// The test runner's timing lines: its run summary, its slowest files and
// its per-file budget failures (printTimingLines, #2929).
const TIMING_PREFIXES = [ "tests:", "tests slowest:", "tests budget:" ];
const ASSET_ARCHITECTURE_TESTS = new Set( [ "audio-parity-audit.test.mjs" ] );
const SOURCE_GATE_NAMES = new Set( [
	"verify:test-types",
	"typecheck",
	"typecheck:gallery",
	"verify:layout",
	"verify:dependencies",
	"verify:ui-census"
] );

// Longest first, so the critical path starts immediately.
const GATES = [
	// Reuses each file's pass while every input it read is unchanged.
	[ "test", [ "tools/run-tests.mjs", "tests/architecture", "tests/runtime" ] ],
	// The joint-longest gate: keys its own inputs (ui.ts's import closure,
	// the tool, the committed census, the built assets) and reuses the
	// last pass while they are unchanged (ui-action-census-gate.mjs).
	[ "verify:ui-action-census", [ "tools/ui-action-census-gate.mjs" ] ],
	[ "verify:test-types", [ "tools/verify-test-types.mjs" ] ],
	[ "typecheck", [
		path.join( clientRoot, "node_modules", "typescript", "bin", "tsc" ),
		"--noEmit",
		"-p",
		"tsconfig.json"
	] ],
	[ "typecheck:gallery", [
		path.join( clientRoot, "node_modules", "typescript", "bin", "tsc" ),
		"--noEmit",
		"-p",
		"tools/ui-gallery/tsconfig.json"
	] ],
	[ "verify:execution", [ "tools/generate-execution-map.mjs" ] ],
	[ "verify:ownership", [ "tools/verify-ownership.mjs" ] ],
	[ "verify:dependencies", [ "tools/verify-dependencies.mjs" ] ],
	[ "verify:allocations", [ "--expose-gc", "tools/perf/bench/ui-allocation-gauge.mjs" ] ],
	[ "verify:ui-census", [ "tools/ui-census.mjs" ] ],
	[ "verify:capabilities", [ "tools/verify-capabilities.mjs" ] ],
	[ "verify:layout", [ "tools/verify-layout.mjs" ] ],
	[ "verify:retail-panels", [ "tools/audit-retail-panels.mjs" ] ],
	[ "verify:delivery", [ "../../scripts/checks/check_asset_delivery.mjs" ] ],
	[ "verify:item-sounds", [ "tools/generate-item-sounds.mjs" ] ],
	[ "audit:audio", [ "tools/audit-audio-parity.mjs" ] ]
];

/*
================
sourcePart

The source lane's part an argument names: --source runs both, CI runs
--source=gates and --source=runtime as two jobs on two runners, so the
runtime files' ~600 processes never share a two-core runner with the
typechecks (together they took the job from 87-132 s to 220-230 s and
pushed execution.test.mjs past its cap; ops #335 row 1). Undefined for
anything else.
================
*/
const SOURCE_PARTS = {
	all: ( gates, runtime ) => [ ...gates, ...runtime ],
	gates: ( gates ) => gates,
	runtime: ( _gates, runtime ) => runtime
};

function sourcePart( args ) {
	if ( args[0] === SOURCE_FLAG ) return "all";
	const named = args[0]?.startsWith( SOURCE_FLAG + "=" ) ? args[0].slice( SOURCE_FLAG.length + 1 ) : undefined;
	return named !== undefined && Object.hasOwn( SOURCE_PARTS, named ) && named !== "all" ? named : undefined;
}

/*
================
sourceGates

The audio parity audit consumes the extracted native sound and animation tables.
Keep that file in the full asset lane; do not silently skip missing files inside
its tests. Every other architecture suite runs from a clean source checkout.
================
*/
function sourceGates( part ) {
	const architecture = readdirSync( path.join( clientRoot, "tests/architecture" ) )
		.filter( ( name ) => name.endsWith( ".test.mjs" ) && !ASSET_ARCHITECTURE_TESTS.has( name ) )
		.sort()
		.map( ( name ) => `tests/architecture/${name}` );
	// Every runtime file but the asset-backed ones (ops #335 row 1: the lane
	// ran a hand-picked 17 of 766, so the rest regressed unseen until a
	// release build). The list is derived from what each file read
	// (tools/list-asset-backed-tests.mjs).
	const skip = "--skip=" + path.relative( clientRoot, ASSET_BACKED_LIST ).replaceAll( "\\", "/" );
	const gates = [
		[ "source tests", [ "tools/run-tests.mjs", ...architecture ] ],
		...GATES.filter( ( [name] ) => SOURCE_GATE_NAMES.has( name ) )
	];
	const runtime = [ [ "source runtime tests", [ "tools/run-tests.mjs", skip, "tests/runtime" ] ] ];
	return SOURCE_PARTS[part]( gates, runtime );
}

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
	const args = process.argv.slice( 2 );
	const part = sourcePart( args );
	if ( args.length > 1 || (args.length === 1 && part === undefined) ) {
		throw new Error( `Usage: node tools/check.mjs [--source[=gates|runtime]]` );
	}
	const sourceOnly = part !== undefined;
	const gates = sourceOnly ? sourceGates( part ) : GATES;
	const started = performance.now();
	process.stdout.write(
		`client check (${
			sourceOnly ? "source " + part : "full"
		}): ${gates.length} gates on ${os.availableParallelism()} cores\n`
	);
	if ( sourceOnly ) {
		process.stdout.write( "Retail audio parity and asset delivery remain required by the full check.\n" );
	}
	const results = await Promise.all( gates.map( ( [name, args] ) => runGate( name, args ) ) );
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
