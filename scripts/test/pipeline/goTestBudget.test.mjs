/*
===========================================================================

goTestBudget.test.mjs - CPU contention must not hide real test regressions

Recorded-result fixtures pin the acceptance rule without depending on the
host's speed. The integration case builds the actual timer and runs a Go
test through it, including a failing test and a path containing spaces.

===========================================================================
*/

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { checkTestBudget, goTestOutput, readCPUMeasurements } from "../../checks/goTestBudget.mjs";

const BUDGET_SECONDS = 30;
const FIXTURE_PACKAGE = "example.test/server/package";
const repositoryRoot = fileURLToPath( new URL( "../../../", import.meta.url ) );

/*
================
cpuResult
================
*/
function cpuResult( seconds, { pkg = FIXTURE_PACKAGE, wallSeconds = 90, action = "pass" } = {} ) {
	return [
		{ Action: "output", Package: pkg, Output: `SRO_TEST_CPU_SECONDS=${seconds}\n` },
		{ Action: action, Package: pkg, Elapsed: wallSeconds }
	].map( ( event ) => JSON.stringify( event ) ).join( "\n" ) + "\n";
}

/*
================
checkFixture
================
*/
async function checkFixture( cpuSeconds, { wallSeconds = 90, ...measurement } = {} ) {
	const lines = [];
	const accepted = await checkTestBudget( `ok\t${FIXTURE_PACKAGE}\t${wallSeconds}s\n`, {
		budgetSeconds: BUDGET_SECONDS,
		ledger: [],
		log: ( line ) => lines.push( line ),
		measure: async ( packages ) => {
			assert.deepEqual( packages, [ FIXTURE_PACKAGE ] );
			return { ok: true, output: cpuResult( cpuSeconds, measurement ) };
		}
	} );
	return { accepted, lines };
}

test("a passing package delayed by CPU contention fits the CPU budget", async () => {
	const result = await checkFixture( 12, { wallSeconds: 90 } );
	assert.equal( result.accepted, true );
	assert.match( result.lines.join( "\n" ), /12s CPU/ );
});

test("an expensive passing package still fails on a quiet box", async () => {
	assert.equal( (await checkFixture( 31, { wallSeconds: 32 } )).accepted, false );
});

test("CPU contention does not excuse excessive CPU work", async () => {
	assert.equal( (await checkFixture( 31, { wallSeconds: 120 } )).accepted, false );
});

test("the CPU threshold is inclusive", async () => {
	assert.equal( (await checkFixture( BUDGET_SECONDS )).accepted, true );
	assert.equal( (await checkFixture( BUDGET_SECONDS + 0.001 )).accepted, false );
});

test("fast, cached, untested and ledgered packages need no recheck", async () => {
	const lines = [];
	const accepted = await checkTestBudget(
		[
			"ok example.test/fast 0.500s",
			`ok example.test/boundary ${BUDGET_SECONDS}s`,
			"ok example.test/cached (cached)",
			"? example.test/empty [no test files]",
			"ok example.test/ledger 300s"
		].join( "\n" ),
		{
			budgetSeconds: BUDGET_SECONDS,
			ledger: [ "example.test/ledger" ],
			log: ( line ) => lines.push( line ),
			measure: async () => assert.fail( "unnecessary recheck" )
		}
	);
	assert.equal( accepted, true );
	assert.equal( lines.length, 1 );
	assert.match( lines[0], /in ledger/ );
});

test("concurrent package output is attributed by package, not output order", async () => {
	const first = "example.test/first";
	const second = "example.test/second";
	const accepted = await checkTestBudget( `ok ${first} 90s\nok ${second} 80s\n`, {
		budgetSeconds: BUDGET_SECONDS,
		ledger: [],
		log: () => {},
		measure: async ( packages ) => {
			assert.deepEqual( packages, [ first, second ] );
			return { ok: true, output: cpuResult( 31, { pkg: second } ) + cpuResult( 1, { pkg: first } ) };
		}
	} );
	assert.equal( accepted, false, "the second package's CPU regression must not be lost" );
});

test("missing, malformed, duplicate, cached or failed measurements fail closed", async () => {
	const cached = JSON.stringify( {
		Action: "output",
		Package: FIXTURE_PACKAGE,
		Output: `ok ${FIXTURE_PACKAGE} (cached)\n`
	} );
	for (
		const output of [
			"",
			cpuResult( "" ),
			cpuResult( "NaN" ),
			cpuResult( "Infinity" ),
			cpuResult( -1 ),
			cpuResult( 1 ) + cpuResult( 2 ),
			cpuResult( 1, { pkg: "example.test/wrong" } ),
			cpuResult( 1, { action: "fail" } ),
			cpuResult( 1 ) + cached
		]
	) {
		const accepted = await checkTestBudget( `ok ${FIXTURE_PACKAGE} 90s\n`, {
			budgetSeconds: BUDGET_SECONDS,
			ledger: [],
			log: () => {},
			measure: async () => ({ ok: true, output })
		} );
		assert.equal( accepted, false, output );
	}
});

test("a failed CPU recheck cannot turn the original pass into acceptance", async () => {
	const lines = [];
	const output = cpuResult( 1 ) + JSON.stringify( {
		Action: "output",
		Package: FIXTURE_PACKAGE,
		Output: `FAIL ${FIXTURE_PACKAGE}\n`
	} );
	assert.equal(
		await checkTestBudget( `ok ${FIXTURE_PACKAGE} 90s\n`, {
			budgetSeconds: BUDGET_SECONDS,
			ledger: [],
			log: ( line ) => lines.push( line ),
			measure: async () => ({ ok: false, output })
		} ),
		false
	);
	assert.match( lines.join( "\n" ), /FAIL example.test\/server\/package/ );
	assert.equal( goTestOutput( "go: toolchain failed\n" ), "go: toolchain failed\n" );
});

/*
================
runGo
================
*/
function runGo( args, env = process.env ) {
	const result = spawnSync( "go", args, {
		cwd: path.join( repositoryRoot, "apps", "server" ),
		windowsHide: true,
		encoding: "utf8",
		env,
		timeout: 120_000
	} );
	assert.ifError( result.error );
	return result;
}

// Portable source checks do not require Go; the full source/server gate does.
const goProbe = spawnSync( "go", [ "version" ], { windowsHide: true } );
const goUnavailable = goProbe.error && "code" in goProbe.error && goProbe.error.code === "ENOENT";
test( "the real Go exec timer measures work and preserves a failed test", {
	skip: goUnavailable ? "Go is not installed" : false
}, ( t ) => {
	const directory = mkdtempSync( path.join( os.tmpdir(), "go CPU timer " ) );
	t.after( () => rmSync( directory, { recursive: true, force: true } ) );
	const timer = path.join( directory, process.platform === "win32" ? "timer.exe" : "timer" );
	const build = runGo( [ "build", "-o", timer, path.join( repositoryRoot, "scripts/checks/goTestCPU.go" ) ] );
	assert.equal( build.status, 0, build.stderr );
	for ( const args of [ [], [ path.join( directory, "missing-test-binary" ) ] ] ) {
		const failure = spawnSync( timer, args, { encoding: "utf8", windowsHide: true } );
		assert.equal( failure.status, 1, failure.stderr );
		assert.doesNotMatch( failure.stdout, /SRO_TEST_CPU_SECONDS=/ );
	}
	const fixture = path.join( repositoryRoot, "scripts/test/fixtures/go-test-cpu_test.go" );
	const args = [ "test", "-json", "-count=1", `-exec="${timer}"`, fixture ];
	for ( const mode of [ "work", "fail" ] ) {
		const result = runGo( args, { ...process.env, SRO_CPU_TEST_MODE: mode } );
		assert.equal( result.status, mode === "work" ? 0 : 1, result.stdout + result.stderr );
		const measurements = readCPUMeasurements( result.stdout );
		assert.equal( measurements.size, 1 );
		const [measurement] = measurements.values();
		assert.equal( measurement.passed, mode === "work" );
		assert.equal( measurement.samples.length, 1, result.stdout );
		assert.ok( Number.isFinite( measurement.samples[0] ) );
		assert.ok( measurement.samples[0] >= 0 );
		if ( mode === "work" ) assert.ok( measurement.samples[0] > 0, "CPU work was not measured" );
	}
} );
