/*
===========================================================================

list-asset-backed-tests.mjs - which runtime test files need the built assets

	node tools/list-asset-backed-tests.mjs

CI's source lane (tools/check.mjs --source) has no extracted client and no
.generated tree, so it runs every runtime test file except the ones that read
them. It used to run a hand-picked 17 of 766 instead, so a regression in any
of the other ~560 asset-free files was found at release time, after merge
(ops #335 row 1: four re-freezes in one day).

The test cache already knows what each file read: every passing file's
record names each input. A file is asset-backed when any input lies outside
the checkout (the extracted client, the shared .generated) or in a tree only
a workstation has (.generated, .state, temp, .tools inside the checkout).
This tool reads the shared records and writes the list to
tests/runtime/asset-backed-tests.json. Run it after a full local
`node tools/run-tests.mjs tests/runtime` when you add or change what a test
reads; a file without a record (one that starts a program) keeps its
previous verdict. A new asset-backed file missing from the list fails CI's
source lane at once, so the list cannot hide a regression; a stale entry only
keeps an asset-free file out of CI, as before.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { recordsPath } from "./lib/test-cache-files.mjs";
import { CHECKOUT_PREFIX } from "./lib/test-cache-ids.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const repoRoot = path.resolve( clientRoot, "../.." );
const RUNTIME_DIR = path.join( clientRoot, "tests", "runtime" );
export const LIST_PATH = path.join( RUNTIME_DIR, "asset-backed-tests.json" );
// Trees inside the checkout that only a workstation has.
const LOCAL_TREE = /(^|[\\/])(\.generated|\.state|temp|\.tools)[\\/]/i;

/*
================
assetBacked

True when one of a record's inputs is something CI's source lane lacks. An
input id is "<kind>\t<target>", the target checkout-relative ("~/...") or
absolute (test-cache-ids.mjs).
================
*/
export function assetBacked( record ) {
	return Object.keys( record.inputs ?? {} ).some( ( id ) => {
		const target = id.slice( id.indexOf( "\t" ) + 1 );
		return !target.startsWith( CHECKOUT_PREFIX ) || LOCAL_TREE.test( target.slice( CHECKOUT_PREFIX.length ) );
	} );
}

/*
================
classify

The asset-backed runtime files, by file name: each current file with a record
is judged by it, one without keeps its previous verdict.
================
*/
export function classify( { records, files, previous } ) {
	const byName = new Map();
	for ( const [file, record] of Object.entries( records ) ) {
		const parts = file.split( /[\\/]/ );
		if ( parts.at( -2 ) === "runtime" ) byName.set( parts.at( -1 ), record );
	}
	const before = new Set( previous );
	return files.filter( ( name ) => byName.has( name ) ? assetBacked( byName.get( name ) ) : before.has( name ) )
		.sort();
}

/*
================
readList

The committed list, or none when it is absent.
================
*/
export function readList( file = LIST_PATH ) {
	try {
		return JSON.parse( fs.readFileSync( file, "utf8" ) ).tests;
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return [];
		throw error;
	}
}

/*
================
main
================
*/
function main() {
	const index = JSON.parse( fs.readFileSync( recordsPath( repoRoot ), "utf8" ) );
	const files = fs.readdirSync( RUNTIME_DIR ).filter( ( name ) => name.endsWith( ".test.mjs" ) );
	const tests = classify( { records: index.records ?? {}, files, previous: readList() } );
	const body = { generatedBy: "apps/client-next/tools/list-asset-backed-tests.mjs", tests };
	fs.writeFileSync( LIST_PATH, JSON.stringify( body, null, "\t" ) + "\n" );
	console.log(
		`asset-backed: ${tests.length} of ${files.length} runtime files -> ${path.relative( repoRoot, LIST_PATH )}`
	);
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
