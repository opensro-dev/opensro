/*
===========================================================================

check.mjs - every client gate, run concurrently

`pnpm check` used to run eleven gates one after another. They are
independent - each reads the sources, the contracts or the generated assets
and writes nothing another gate reads - so they run together here, with the
test run (the longest) started first. Each gate's output is captured and
printed as one block when it finishes, so the report stays readable. The
check fails if any gate fails, after all of them have finished.

===========================================================================
*/

import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const node = process.execPath;

// Longest first, so the critical path starts immediately.
const GATES = [
	[ "test", [ "--test", "tests/architecture/*.test.mjs", "tests/runtime/*.test.mjs" ] ],
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
	const started = performance.now();
	process.stdout.write( `client check: ${GATES.length} gates on ${os.availableParallelism()} cores\n` );
	const results = await Promise.all( GATES.map( ( [name, args] ) => runGate( name, args ) ) );
	const failed = results.filter( ( result ) => !result.ok );
	for ( const result of failed ) {
		process.stderr.write( `\n--- ${result.name} ---\n${result.output}\n` );
	}
	const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
	if ( failed.length > 0 ) {
		process.stderr.write( `client check: ${failed.length} gate(s) failed in ${seconds}s\n` );
		process.exit( 1 );
	}
	process.stdout.write( `client check: PASS (${GATES.length} gates, ${seconds}s)\n` );
}

await main();
