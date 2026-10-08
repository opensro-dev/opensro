/*
===========================================================================

testSuiteMembership.test.mjs - every script test belongs to a task that runs it

The pipeline tests run by directory glob (check:pipeline-tests), but the
asset, CIF, region and world tests run only from the explicit file lists in
scripts/test/suites/. A test missing from those lists never runs anywhere,
which is how six asset tests went unrun. This test runs under the glob, so
it always runs, and fails the moment a test file is left out.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { getSuiteFiles, SUITES } from "../suites/index.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const TEST_ROOT = path.join( REPO_ROOT, "scripts", "test" );
// Run by a directory glob or a discover runner, not by a suite list.
const GLOB_RUN_DIRECTORIES = new Set( [ "pipeline", "python" ] );

/*
================
testFiles

Every *.test.mjs under scripts/test outside the glob-run directories, as a
repository-relative forward-slash path.
================
*/
function testFiles() {
	const found = [];
	for ( const entry of fs.readdirSync( TEST_ROOT, { recursive: true, withFileTypes: true } ) ) {
		if ( !entry.isFile() || !entry.name.endsWith( ".test.mjs" ) ) continue;
		const relative = path.relative( REPO_ROOT, path.join( entry.parentPath, entry.name ) ).split( path.sep ).join(
			"/"
		);
		if ( GLOB_RUN_DIRECTORIES.has( relative.split( "/" )[2] ) ) continue;
		found.push( relative );
	}
	return found.sort();
}

/*
================
listedFiles

Each listed file with the suites that list it.
================
*/
function listedFiles() {
	const owners = new Map();
	for ( const [name, suite] of Object.entries( SUITES ) ) {
		for ( const file of getSuiteFiles( suite ) ) owners.set( file, [ ...(owners.get( file ) ?? []), name ] );
	}
	return owners;
}

test("every suite-run script test is listed by exactly one suite", () => {
	const owners = listedFiles();
	const unlisted = testFiles().filter( file => !owners.has( file ) );
	assert.deepEqual( unlisted, [], "add these to a suite in scripts/test/suites/" );
	const twice = [ ...owners ].filter( ( [, suites] ) => suites.length > 1 ).map( ( [file] ) => file );
	assert.deepEqual( twice, [], "a test listed twice runs twice" );
});

test("every listed test file exists", () => {
	const missing = [ ...listedFiles().keys() ].filter( file => !fs.existsSync( path.join( REPO_ROOT, file ) ) );
	assert.deepEqual( missing, [] );
});
