/*
===========================================================================

failureReport.test.mjs - a failed gate step names its packages

Captured `go test` outputs, driven through the report the Go server gate
prints (scripts/checks/failureReport.mjs). The load-bearing fixture is
the first: a failing test binary ends its own output with a bare FAIL
line before `go test` prints "FAIL <pkg> <time>", and the bare line must
not be read as a package - or swallow the name that follows it.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { test } from "node:test";
import { failingPackages, failureReport, failureSummary, runsGoTest } from "../../checks/failureReport.mjs";

// The report under test, as the child below imports it.
const FAILURE_REPORT_URL = new URL( "../../checks/failureReport.mjs", import.meta.url ).href;

// Larger than a Linux pipe buffer, so an undrained write is visibly cut.
const LARGE_OUTPUT_LINES = 20000;

// The go test verdict that ends the large output, the part a cut loses.
const FAIL_LINE = [ "FAIL", "opensro.online/server/internal/game/action", "43.880s" ].join( String.fromCharCode( 9 ) );

// What `go test` prints when one package's test fails and another passes:
// the failing binary's own output (ending in its bare FAIL), its exit
// status, the package verdict, and the run's closing bare FAIL.
const TEST_FAILURE_OUTPUT = [
	"ok  \topensro.online/server/internal/agent/api\t0.812s",
	"--- FAIL: TestLeaky (0.02s)",
	"\trace_test.go:17: boom",
	"FAIL",
	"exit status 1",
	"FAIL\topensro.online/server/internal/transport\t0.456s",
	"ok  \topensro.online/server/internal/cluster/shard\t1.020s",
	"FAIL",
	""
].join( "\n" );

// What `go test` prints when a package does not compile.
const BUILD_FAILURE_OUTPUT = [
	"# opensro.online/server/internal/security/auth",
	"internal/security/auth/base64.go:10:1: syntax error: non-declaration statement outside function body",
	"FAIL\topensro.online/server/internal/security/auth [build failed]",
	"ok  \topensro.online/server/internal/agent/api\t0.812s",
	"FAIL",
	""
].join( "\n" );

// Every package ok, yet the command exited non-zero: the killed
// command (or a toolchain-level error), which names no package at all.
const ALL_OK_OUTPUT = [
	"ok  \topensro.online/server/internal/agent/api\t0.812s",
	"ok  \topensro.online/server/internal/transport\t1.105s",
	""
].join( "\n" );

test("a test failure names its package, not the bare FAIL line", () => {
	assert.deepEqual( failingPackages( TEST_FAILURE_OUTPUT ), [ "opensro.online/server/internal/transport" ] );
	const result = { label: "race", code: 1, signal: null, output: TEST_FAILURE_OUTPUT };
	assert.equal(
		failureSummary( result ),
		"race failed (exit 1: opensro.online/server/internal/transport)"
	);
	assert.ok(
		failureReport( result ).startsWith( "--- race failed (exit 1: opensro.online/server/internal/transport) ---\n" )
	);
});

test("a build failure names its package", () => {
	assert.deepEqual( failingPackages( BUILD_FAILURE_OUTPUT ), [ "opensro.online/server/internal/security/auth" ] );
	const result = { label: "race", code: 1, signal: null, output: BUILD_FAILURE_OUTPUT };
	assert.equal(
		failureSummary( result ),
		"race failed (exit 1: opensro.online/server/internal/security/auth)"
	);
});

test("an all-ok output with a non-zero exit names no package and says so", () => {
	assert.deepEqual( failingPackages( ALL_OK_OUTPUT ), [] );
	const result = { label: "race", code: 1, signal: null, output: ALL_OK_OUTPUT };
	assert.equal( failureSummary( result ), "race failed (exit 1)" );
	const report = failureReport( result );
	assert.ok( report.startsWith( "--- race failed (exit 1) ---\n" ) );
	assert.ok( report.includes( "the output names no failing package" ) );
	assert.ok( report.endsWith( ALL_OK_OUTPUT ) );
});

test("an exit-0 rejection says the acceptance check rejected it", () => {
	const result = { label: "gofmt", code: 0, signal: null, output: "internal/game/action/monsterunique.go\n" };
	assert.equal(
		failureSummary( result ),
		"gofmt failed (exit 0, rejected by the step's acceptance check)"
	);
	assert.ok( !failureReport( result ).includes( "names no failing package" ) );
});

// A lint finding as golangci-lint prints it: a file and line, no package
// verdict. The step did run and report; it was not killed.
const LINT_FAILURE_OUTPUT = [
	"internal/data/store/lock_timing_test.go:43:3: use of `time.Sleep` forbidden (forbidigo)",
	"1 issues:",
	"* forbidigo: 1",
	""
].join( "\n" );

test("a failed lint step is not reported as a command that never reported", () => {
	const result = { label: "golangci-lint", code: 1, signal: null, output: LINT_FAILURE_OUTPUT };
	const report = failureReport( result );
	assert.ok( report.startsWith( "--- golangci-lint failed (exit 1) ---\n" ) );
	assert.ok( !report.includes( "names no failing package" ), "the note is for go test steps only" );
	assert.ok( report.endsWith( LINT_FAILURE_OUTPUT ) );
	for ( const label of [ "tests", "tests (long tier)", "race" ] ) assert.ok( runsGoTest( label ), label );
	for ( const label of [ "golangci-lint", "vet", "gofmt", "build" ] ) assert.ok( !runsGoTest( label ), label );
});

/*
================
stderrOfThrowingGate

Run a child that does what the gate does on failure: print the report of
one step whose large output ends in its FAIL line, then throw. Resolves
with everything the parent read from the child's stderr pipe.
================
*/
function stderrOfThrowingGate() {
	const child = `
		import { printFailureReports } from ${JSON.stringify( FAILURE_REPORT_URL )};
		const lines = Array.from( { length: ${LARGE_OUTPUT_LINES} }, ( _, i ) => "line " + i + " " + "x".repeat( 60 ) );
		lines.push( ${JSON.stringify( FAIL_LINE )} );
		const output = lines.join( String.fromCharCode( 10 ) );
		await printFailureReports( [ { label: "tests", code: 1, signal: null, output } ] );
		throw new Error( "server tests: tests failed" );
	`;
	return new Promise( ( resolve, reject ) => {
		const proc = spawn( process.execPath, [ "--input-type=module", "-e", child ], {
			stdio: [ "ignore", "ignore", "pipe" ]
		} );
		let stderr = "";
		proc.stderr.setEncoding( "utf8" );
		proc.stderr.on( "data", ( chunk ) => stderr += chunk );
		proc.on( "error", reject );
		proc.on( "close", ( code ) => resolve( { code, stderr } ) );
	} );
}

test("a gate that throws after its report still delivers the report's FAIL line through a pipe", async () => {
	const { code, stderr } = await stderrOfThrowingGate();
	assert.notEqual( code, 0, "the gate's throw still fails the process" );
	assert.ok( stderr.includes( FAIL_LINE ), `the report was cut: ${stderr.length} bytes arrived` );
	assert.ok( stderr.includes( "server tests: tests failed" ), "the thrown error follows the report" );
});
