/*
===========================================================================

check-lanes.test.mjs - the release's asset lane is the complement of CI's source lanes

The release runs only the asset lane, trusting the freeze's green CI for
the source lanes (ops release.py ci_state). That trade is sound only while
the two together run every gate of the full check exactly once and the
runtime tests split on the one asset-backed list: a gate added to neither
would run nowhere, and a gate in both would run twice.

===========================================================================
*/
import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { checkLane, GATES } from "../../tools/check-lanes.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );
const TEST_GATES = new Set( [ "test", "source tests", "source runtime tests", "asset tests" ] );

/*
================
gateNames

A lane's gate names, without the test runs (they split by file, below).
================
*/
function gateNames( lane ) {
	return lane.gates.map( ( [name] ) => name ).filter( ( name ) => !TEST_GATES.has( name ) );
}

/*
================
testTargets

The arguments of a lane's test runs: the list flags and the targets.
================
*/
function testTargets( lane ) {
	return lane.gates.filter( ( [name] ) => TEST_GATES.has( name ) ).flatMap( ( [, args] ) => args.slice( 1 ) );
}

test("the source and asset lanes run every gate of the full check exactly once", () => {
	const source = [
		...gateNames( checkLane( [ "--source=gates" ] ) ),
		...gateNames( checkLane( [ "--source=runtime" ] ) )
	];
	const assets = gateNames( checkLane( [ "--assets" ] ) );
	const full = GATES.map( ( [name] ) => name ).filter( ( name ) => !TEST_GATES.has( name ) );
	assert.deepEqual( [ ...source, ...assets ].sort(), [ ...full ].sort() );
	assert.equal( new Set( [ ...source, ...assets ] ).size, source.length + assets.length, "a gate runs in both" );
});

test("the runtime tests split on one asset-backed list, and the architecture suites on one set", () => {
	const source = testTargets( checkLane( [ "--source=gates" ] ) ).concat(
		testTargets( checkLane( [ "--source=runtime" ] ) )
	);
	const assets = testTargets( checkLane( [ "--assets" ] ) );
	const skip = source.find( ( arg ) => arg.startsWith( "--skip=" ) ),
		only = assets.find( ( arg ) => arg.startsWith( "--only=" ) );
	assert.ok( skip && only, "each side names the list" );
	assert.equal( skip.slice( "--skip=".length ), only.slice( "--only=".length ) );
	assert.ok( source.includes( "tests/runtime" ) && assets.includes( "tests/runtime" ) );
	const architecture = readdirSync( path.join( clientRoot, "tests/architecture" ) )
		.filter( ( name ) => name.endsWith( ".test.mjs" ) )
		.map( ( name ) => `tests/architecture/${name}` );
	const named = [ ...source, ...assets ].filter( ( arg ) => arg.startsWith( "tests/architecture/" ) );
	assert.deepEqual( [ ...named ].sort(), [ ...architecture ].sort() );
});

test("an unknown lane is refused", () => {
	assert.throws( () => checkLane( [ "--assets", "--source" ] ), /Usage/ );
	assert.throws( () => checkLane( [ "--everything" ] ), /Usage/ );
});
