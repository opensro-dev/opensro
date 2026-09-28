/*
===========================================================================

verify-test-types.mjs - type-check ratchet for the Node-side tests

Runs tsc over tsconfig.tests.json and fails when a test file has type errors
and is not listed in tests/architecture/test-type-debt.txt, the ledger of
files that had errors when the gate was introduced. The ledger only
shrinks: a listed file that is now clean (or gone) must be removed, which
`--update` does. It never adds a file.

Only errors inside tests/ count. The test program loads Node's types, which
changes a few browser types in src/; src/ is checked by `pnpm typecheck`.

===========================================================================
*/

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { root } from "./project.mjs";

const TSC = path.join( root, "node_modules", "typescript", "bin", "tsc" );
const LEDGER = path.join( root, "tests", "architecture", "test-type-debt.txt" );
const LEDGER_HEADER = [
	"# Node-side test files that still have type errors under tsconfig.tests.json.",
	"# This ledger only shrinks: fix a file, then run `node tools/verify-test-types.mjs --update`."
];

/*
================
filesWithErrors

Runs the test program and returns each test file with its error count.
================
*/
function filesWithErrors() {
	const result = spawnSync( process.execPath, [ TSC, "-p", "tsconfig.tests.json", "--pretty", "false" ], {
		windowsHide: true,
		cwd: root,
		encoding: "utf8",
		maxBuffer: 64 * 1024 * 1024
	} );
	if ( result.error ) {
		throw result.error;
	}
	const counts = new Map();
	for ( const line of result.stdout.split( /\r?\n/ ) ) {
		const match = /^(tests\/[^(]+)\(\d+,\d+\): error TS\d+/.exec( line );
		if ( match ) {
			counts.set( match[1], (counts.get( match[1] ) ?? 0) + 1 );
		} else if ( /^error TS\d+/.test( line ) ) {
			throw new Error( `tsc configuration error: ${line}` );
		}
	}
	return counts;
}

/*
================
readLedger
================
*/
function readLedger() {
	return fs.readFileSync( LEDGER, "utf8" )
		.split( /\r?\n/ )
		.map( ( line ) => line.trim() )
		.filter( ( line ) => line.length > 0 && !line.startsWith( "#" ) );
}

/*
================
main
================
*/
function main() {
	const update = process.argv.includes( "--update" );
	const counts = filesWithErrors();
	const ledger = readLedger();
	const listed = new Set( ledger );

	const unlisted = [ ...counts.keys() ].filter( ( file ) => !listed.has( file ) ).sort();
	const cleared = ledger.filter( ( file ) => !counts.has( file ) );

	if ( update ) {
		const kept = ledger.filter( ( file ) => counts.has( file ) ).sort();
		fs.writeFileSync( LEDGER, `${[ ...LEDGER_HEADER, ...kept ].join( "\n" )}\n` );
		console.log( `test-type-debt: ${cleared.length} entries removed, ${kept.length} remain` );
	}

	const problems = [];
	if ( unlisted.length > 0 ) {
		problems.push(
			`${unlisted.length} test file(s) have type errors (run \`pnpm exec tsc -p tsconfig.tests.json\`):\n` +
				unlisted.map( ( file ) => `\t${file} (${counts.get( file )})` ).join( "\n" )
		);
	}
	if ( !update && cleared.length > 0 ) {
		problems.push(
			`${cleared.length} ledger entr(ies) are now clean or gone; run \`node tools/verify-test-types.mjs --update\`:\n` +
				cleared.map( ( file ) => `\t${file}` ).join( "\n" )
		);
	}
	if ( problems.length > 0 ) {
		console.error( problems.join( "\n\n" ) );
		process.exitCode = 1;
		return;
	}
	const errors = [ ...counts.values() ].reduce( ( sum, n ) => sum + n, 0 );
	console.log( `test types: PASS (${counts.size} ledger file(s) carry ${errors} error(s); no new errors)` );
}

main();
