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
sourceGates

The audio parity audit consumes the extracted native sound and animation tables.
Keep that file in the full asset lane; do not silently skip missing files inside
its tests. Every other architecture suite runs from a clean source checkout.
================
*/
function sourceGates() {
	const architecture = readdirSync( path.join( clientRoot, "tests/architecture" ) )
		.filter( ( name ) => name.endsWith( ".test.mjs" ) && !ASSET_ARCHITECTURE_TESTS.has( name ) )
		.sort()
		.map( ( name ) => `tests/architecture/${name}` );
	const tests = [
		"tools/run-tests.mjs",
		...architecture,
		"tests/runtime/lifecycle.test.mjs",
		"tests/runtime/device-recovery.test.mjs",
		"tests/runtime/renderer-diagnostics.test.mjs",
		"tests/runtime/input-network.test.mjs",
		"tests/runtime/beta-release.test.mjs",
		"tests/runtime/runtime-text-admission.test.mjs",
		"tests/runtime/persistent-recovery.test.mjs",
		"tests/runtime/persistent-settlement.test.mjs",
		"tests/runtime/application-release.test.mjs",
		"tests/runtime/cos-item-use.test.mjs",
		"tests/runtime/cos-hud.test.mjs",
		"tests/runtime/release-smoke.test.mjs",
		"tests/runtime/terrain-frustum-cull.test.mjs",
		// The asset transport's recovery and resume (#3354) mock fetch and
		// time; they read no generated file.
		"tests/runtime/asset-download-recovery.test.mjs",
		"tests/runtime/asset-download-resume.test.mjs",
		// The chat tabs' filing rule (#3370) reads no generated file.
		"tests/runtime/chat-tabs.test.mjs"
	];
	return [ [ "source tests", tests ], ...GATES.filter( ( [name] ) => SOURCE_GATE_NAMES.has( name ) ) ];
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
	if ( args.length > 1 || (args.length === 1 && args[0] !== SOURCE_FLAG) ) {
		throw new Error( "Usage: node tools/check.mjs [--source]" );
	}
	const sourceOnly = args[0] === SOURCE_FLAG;
	const gates = sourceOnly ? sourceGates() : GATES;
	const started = performance.now();
	process.stdout.write(
		`client check (${
			sourceOnly ? "source" : "full"
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

if ( process.argv.includes( SOURCE_FLAG ) ) {
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
