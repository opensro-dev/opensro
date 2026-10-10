/*
===========================================================================

check.mjs - client verification with explicit source and asset boundaries

`pnpm check` used to run eleven gates one after another. They are
independent - each reads the sources, the contracts or the generated assets
and writes nothing another gate reads - so they run together here, with the
test run (the longest) started first. Each gate's output is captured and
printed as one block when it finishes, so the report stays readable. The
check fails if any gate fails, after all of them have finished. Hosted app-only
builds use --source: types, layout and source tests without retail extraction.
The default remains the complete eleven-gate check, including asset audits.

===========================================================================
*/

import { spawn } from "node:child_process";
import { readdirSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const node = process.execPath;
const SOURCE_FLAG = "--source";
const ASSET_ARCHITECTURE_TESTS = new Set( [ "audio-parity-audit.test.mjs" ] );
const SOURCE_GATE_NAMES = new Set( [ "verify:test-types", "typecheck", "verify:layout" ] );

// Longest first, so the critical path starts immediately.
const GATES = [
	// Reuses each file's pass while every input it read is unchanged.
	[ "test", [ "tools/run-tests.mjs", "tests/architecture", "tests/runtime" ] ],
	[ "verify:test-types", [ "tools/verify-test-types.mjs" ] ],
	[ "typecheck", [
		path.join( clientRoot, "node_modules", "typescript", "bin", "tsc" ),
		"--noEmit",
		"-p",
		"tsconfig.json"
	] ],
	[ "verify:execution", [ "tools/generate-execution-map.mjs" ] ],
	[ "verify:ownership", [ "tools/verify-ownership.mjs" ] ],
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
		"tests/runtime/beta-release.test.mjs",
		"tests/runtime/runtime-text-admission.test.mjs",
		"tests/runtime/application-release.test.mjs",
		"tests/runtime/cos-item-use.test.mjs",
		"tests/runtime/cos-hud.test.mjs",
		"tests/runtime/release-smoke.test.mjs"
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
	await withGeneratedAssetsLock( "client verification", main );
}
