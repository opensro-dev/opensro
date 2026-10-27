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
import { test } from "node:test";
import { failingPackages, failureReport, failureSummary } from "../../checks/failureReport.mjs";

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
