/*
===========================================================================

Go Server Verification Gate

Module hygiene (`go mod tidy -diff` for the module and the lint tool module,
gofmt, `go vet`, golangci-lint), every package's tests, the race detector on
the concurrency-owning packages, and govulncheck (pinned in go.mod as a
tool). golangci-lint is pinned in its own tool module,
tools/golangci-lint/go.mod, so the repository's Go toolchain builds it.

Speed. Each step declares the inputs it reads; a step whose inputs match
a recorded pass (checkStamps.mjs) reports "up to date" without running, so
an edit re-runs only the steps it can affect (a test-only edit never
re-runs govulncheck). SRO_CHECK_FORCE=1 runs every step. The steps that do
run are independent, so they all run concurrently: hygiene,
the tests, the race run and govulncheck. Each step's output is captured;
a failure prints one report block per failed step - its exit status, the
packages its output names as failed, then the full output - and the thrown
error names the same, so a log that keeps only the error line still says
which step failed, how it ended and which packages failed. Tests run as one
`go test ./...` with SRO_GO_TEST_PARALLELISM package workers (default: half
the cores), so one slow package no longer stalls a whole shard (measured
2026-09-27: 40s with 8 workers against 61s for eight-package shards at 2).

Go's test cache is on. A cached result is valid only for the inputs it was
produced from, so every input must be visible to Go:
  - the server game-data projection lives inside the module
    (apps/server/.generated), where Go validates the files a test opened;
  - the game data outside the module (the retail textdata and the
    published client assets) is named by SRO_GAME_DATA_IDENTITY, which
    clientdata.RequireGameData reads, so it joins every game-data test's key.
Measured 2026-09-29: the projection outside the module made a fully cached
run cost 105s, because Go resolved symlinks for each of ~89k logged file
operations per package; inside it, a cached run of every package takes 10s.
SRO_GO_TEST_CACHE=off restores -count=1 (flake hunting).
Tiers. The everyday gate runs the tests with -short: whole-catalogue and
brute-force evidence sweeps (marked tier.Long, internal/testsupport/tier)
skip there, and every package's tests must burn at most
TEST_BUDGET_SECONDS of CPU (user + system). The tests run through a
supervisor (apps/server/tools/testcpu, via `go test -exec`) that records
each test binary's CPU, because a busy box inflates a package's wall
clock, not the CPU its tests burn: contention alone no longer fails the
budget (opensro#2582). The wall clock is still printed, and a package
with no CPU record keeps the wall verdict, so a lost measurement can
never wave a slow package through. A package over the CPU budget fails
the gate unless the shrinking
ledger scripts/checks/go-test-budget.txt names it. `--long`
(check:server:long, and every release freeze) runs only the test steps,
without -short, so the sweeps run; neither tier needs a hosted CI.
SRO_GO_GATES=label,label runs only the named steps (CI spreads the gate
over parallel runners); an unknown label is an error, never a silent pass.
Changed scope. `--changed` (check:server:changed) is the pre-push form: the
Go packages the branch changes since its merge base with origin/main,
committed or not. It runs gofmt, a whole-module `go build`, vet and
golangci-lint on those packages, their tests plus internal/gates (the
architecture boundaries any package change can break), the race detector on
the ones in the race list, and tidy only when go.mod or go.sum changed. It
leaves govulncheck and the release contract to the full gate, and never
reads or writes a pass stamp: a scoped pass is no evidence for the full one.
It is not a heavy run, so it needs no machine-wide slot.

On Windows every Go command runs with the C compiler's own directory first
on PATH. Git for Windows ships older copies of the MinGW runtime DLLs in
Git\mingw64\bin, and a Git-launched shell (Git Bash, the pre-push hook) puts
that directory first. gcc's cc1 then loads the wrong DLLs and dies silently,
Go's probe of the external linker fails, and Go falls back to legacy link
flags that leave ASLR on - which ThreadSanitizer cannot run under
("failed to allocate ... error code: 87"). Pinning the compiler's directory
makes the race step independent of how the gate was launched.

===========================================================================
*/

import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { publicAssetsRoot, retailTextdataRoot } from "../build/world/paths.mjs";
import { findPass, fingerprintOf, recordPass, snapshotAsync } from "./checkStamps.mjs";
import { failureSummary, persistFailureReports, printFailureReports } from "./failureReport.mjs";
import { overBudgetPackages, parseTestRuns } from "./goTestBudget.mjs";

const scriptDirectory = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDirectory, "..", ".." );
const serverRoot = path.join( rebuildRoot, "apps", "server" );
const modulePath = /^module\s+(\S+)/m.exec( readFileSync( path.join( serverRoot, "go.mod" ), "utf8" ) )[1];
const packageParallelism = positiveInteger(
	process.env.SRO_GO_TEST_PARALLELISM,
	Math.max( 2, Math.floor( os.availableParallelism() / 2 ) )
);
const testCache = process.env.SRO_GO_TEST_CACHE !== "off";
const startedAt = performance.now();
const lintModfile = "tools/golangci-lint/go.mod";
const STEP_ENVIRONMENT = [
	"SRO_REQUIRE_GAME_DATA",
	"SRO_SERVER_GAME_DATA_ROOT",
	"SRO_SERVER_GAME_DATA_MANIFEST_DIGEST",
	"SRO_SERVER_GAME_DATA_CACHE_ROOT",
	"GOFLAGS",
	"GOOS",
	"GOARCH",
	"CGO_ENABLED",
	"GOEXPERIMENT"
];
// TEST_BUDGET_SECONDS is the most CPU (user + system seconds) one
// package's everyday tests may burn.
const TEST_BUDGET_SECONDS = 30;
// The `go test -exec` supervisor that records each test binary's CPU
// (prepareTestSupervisor builds it). It is named for this gate process
// and found through PATH, because go test splits the -exec value on
// spaces: the binary lives under the spaced checkout, so only its bare
// name may appear in the argument.
const supervisorFileName = `sro-testcpu-${process.pid}${process.platform === "win32" ? ".exe" : ""}`;
const supervisorDirectory = path.join( rebuildRoot, ".state", "test-cpu-bin" );
const supervisorPath = path.join( supervisorDirectory, supervisorFileName );
// This process's CPU records (the supervisor writes one JSON per
// package there; see tools/testcpu).
const testCpuDirectory = path.join( rebuildRoot, ".state", "test-cpu", String( process.pid ) );
const VULNERABILITY_DATABASE_INDEX = "https://vuln.go.dev/index/db.json";
// A database update is noticed within this long; govulncheck itself always
// scans against the live database. The key is kept under .state.
const VULNERABILITY_DATABASE_KEY_MS = 15 * 60 * 1000;
const vulnerabilityDatabaseKeyPath = path.join( rebuildRoot, ".state", "check-stamps", "vulndb-index.json" );
// Built on first use by goEnvironment().
let goEnvironmentValue;
const racePackages = [
	"./internal/agent/api",
	"./internal/agent/server",
	"./internal/security/auth",
	"./internal/transport/worldsession",
	"./internal/cluster/shard",
	"./internal/data/store",
	"./internal/transport"
];

// Everything a step can read under the module, in one snapshot; each step
// digests its share. The snapshot, the toolchain query and the database
// key are independent and run concurrently.
const [moduleFiles, goVersion, vulnerabilityDatabase] = await Promise.all( [
	snapshotAsync( [ "apps/server" ] ),
	commandOutput( "go", [ "version" ] ),
	vulnerabilityDatabaseModified()
] );
const gameIdentity = gameDataIdentity();
const toolchain = `=${goVersion}`;
// Environment that changes what a step checks: a pass under one setting is
// no evidence for another (SRO_REQUIRE_GAME_DATA=1 turns skips into failures).
const stepEnvironment = "=env " +
	JSON.stringify( STEP_ENVIRONMENT.map( ( name ) => [ name, process.env[name] ?? null ] ) );

// Step file selections, as repository-relative paths.
const isModuleGo = ( file ) => file.endsWith( ".go" ) || file === "apps/server/go.mod" || file === "apps/server/go.sum";
const isLintTool = ( file ) => file.startsWith( "apps/server/tools/golangci-lint/" );
const isFormatted = ( file ) =>
	file.endsWith( ".go" ) && (file.startsWith( "apps/server/cmd/" ) || file.startsWith( "apps/server/internal/" ));
const isScannedGo = ( file ) =>
	(file.endsWith( ".go" ) && !file.endsWith( "_test.go" )) || file === "apps/server/go.mod" ||
	file === "apps/server/go.sum";
const testKeys = [
	"!apps/server/.generated/game-data/1.150/server/manifest.json",
	`=game-data ${gameIdentity}`,
	`=cache ${testCache} p ${packageParallelism}`,
	toolchain,
	stepEnvironment
];

// The long tier runs the evidence sweeps the everyday -short run skips.
const longTier = process.argv.includes( "--long" ) || process.env.SRO_GO_LONG_TESTS === "1";
const tierArgs = longTier ? [] : [ "-short" ];
testKeys.push( `=tier ${longTier ? "long" : "short"}` );
const testArgs = [
	"test",
	`-p=${packageParallelism}`,
	`-exec=${supervisorFileName}`,
	...tierArgs,
	...(testCache ? [] : [ "-count=1" ]),
	"./..."
];
const budgetLedger = readBudgetLedger();
const changedScope = process.argv.includes( "--changed" ) || process.env.SRO_GO_SCOPE === "changed";
if ( changedScope && longTier ) throw new Error( "--changed and --long do not combine: the long tier is a full run" );
const steps = selectSteps(
	longTier ? "tests (long tier),race" : process.env.SRO_GO_GATES,
	scopeSteps( [
		{
			label: "tidy",
			command: "go",
			args: [ "mod", "tidy", "-diff" ],
			select: isModuleGo,
			keys: [ toolchain, stepEnvironment ]
		},
		{
			label: "tidy (lint tool)",
			command: "go",
			args: [ "-C", path.dirname( lintModfile ), "mod", "tidy", "-diff" ],
			select: isLintTool,
			keys: [ toolchain, stepEnvironment ]
		},
		{
			label: "gofmt",
			command: "gofmt",
			args: [ "-l", "cmd", "internal" ],
			accept: ( output ) => output.trim().length === 0,
			select: isFormatted,
			keys: [ toolchain, stepEnvironment ]
		},
		{
			label: "vet",
			command: "go",
			args: [ "vet", "./..." ],
			select: isModuleGo,
			keys: [ toolchain, stepEnvironment ]
		},
		{
			label: "golangci-lint",
			command: "go",
			args: [ "tool", `-modfile=${lintModfile}`, "golangci-lint", "run", "./..." ],
			select: ( file ) => isModuleGo( file ) || isLintTool( file ) || file === "apps/server/.golangci.yml",
			keys: [ toolchain, stepEnvironment ]
		},
		{
			label: longTier ? "tests (long tier)" : "tests",
			command: "go",
			args: testArgs,
			accept: longTier ? undefined : acceptTestBudget,
			select: () => true,
			// The budget key carries the rule, not just the number: bump
			// it when the rule changes, or a pass recorded under the old
			// rule would bless the new one.
			keys: [ ...testKeys, `=budget cpu ${TEST_BUDGET_SECONDS} ${budgetLedger.join( "," )}` ]
		},
		{
			label: "race",
			command: "go",
			args: [ "test", "-race", ...tierArgs, ...(testCache ? [] : [ "-count=1" ]), ...racePackages ],
			select: () => true,
			keys: testKeys
		},
		{
			// Only production code is scanned; the result also moves with the
			// vulnerability database, so its modification time is a key.
			label: "govulncheck",
			command: "go",
			args: [ "tool", "govulncheck", "./..." ],
			select: isScannedGo,
			keys: [ `=vulndb ${vulnerabilityDatabase}`, toolchain, stepEnvironment ]
		},
		{
			// The compiled release protocol and schema must be what
			// compatibility.json declares; release preparation checks the same.
			label: "release contract",
			command: "go",
			args: [ "run", "./cmd/operations/sro-release-contract" ],
			accept: matchesDeclaredServerContract,
			select: ( file ) => isModuleGo( file ) || file === "apps/server/config/compatibility.json",
			keys: [ toolchain, stepEnvironment ]
		}
	] )
);

const runsTestStep = steps.some( ( step ) => step.label.startsWith( "tests" ) );
const budgetApplies = steps.some( ( step ) => step.label === "tests" );
if ( runsTestStep ) {
	prepareTestSupervisor();
}

// The test step runs alone first: its per-package CPU times are what the
// budget measures, and racing it against the race step (most of the gate's
// time) would smear even those. Every wave still runs, so one failure never
// hides another; the first failure is thrown at the end.
const isTestStep = ( step ) => step.label.startsWith( "tests" );
let firstFailure = null;
for (
	const [name, group] of [
		[ "tests", steps.filter( isTestStep ) ],
		[ longTier ? "long tier" : "all", steps.filter( ( step ) => !isTestStep( step ) ) ]
	]
) {
	if ( group.length === 0 ) continue;
	try {
		await wave( name, group );
	} catch ( error ) {
		firstFailure ??= error;
	}
}
if ( firstFailure ) throw firstFailure;

console.log(
	`server gates: PASS (${packageParallelism} package workers, test cache ${testCache ? "on" : "off"}, ` +
		`${formatSeconds( performance.now() - startedAt )}s)`
);

/*
================
prepareTestSupervisor

Builds the -exec supervisor (apps/server/tools/testcpu) this process's
test runs resolve through PATH, and gives this process a fresh CPU
record directory when the budget applies. Supervisors older gate
processes left behind are retired past an hour; one a live gate is
executing cannot be removed on Windows, and that is fine - it is
skipped and left for the next run.
================
*/
function prepareTestSupervisor() {
	mkdirSync( supervisorDirectory, { recursive: true } );
	if ( budgetApplies ) {
		rmSync( testCpuDirectory, { recursive: true, force: true } );
		mkdirSync( testCpuDirectory, { recursive: true } );
	}
	for ( const name of readdirSync( supervisorDirectory ) ) {
		if ( name === supervisorFileName || !name.startsWith( "sro-testcpu-" ) ) {
			continue;
		}
		try {
			if ( Date.now() - statSync( path.join( supervisorDirectory, name ) ).mtimeMs > 60 * 60 * 1000 ) {
				rmSync( path.join( supervisorDirectory, name ), { force: true } );
			}
		} catch {
			// Still executing, or already gone.
		}
	}
	const built = spawnSync( "go", [ "build", "-o", supervisorPath, "./tools/testcpu" ], {
		windowsHide: true,
		cwd: serverRoot,
		env: { ...goEnvironment(), CGO_ENABLED: "0" },
		encoding: "utf8"
	} );
	if ( built.status !== 0 ) {
		throw new Error( `building the test supervisor (apps/server/tools/testcpu): ${built.stderr || built.stdout}` );
	}
}

/*
================
acceptTestBudget

Accepts the everyday test output when every package that ran burned at
most TEST_BUDGET_SECONDS of CPU (readTestCpuSeconds collects what the
supervisor recorded), or is still named in the budget ledger. Cached
packages ("(cached)") cost nothing and are not timed. The wall clock
go test prints no longer decides (opensro#2582) but is still printed,
and a package with no CPU record keeps the wall verdict. Prints every
package over budget, ledger or not, so the ledger can only shrink,
and every package the wall alone would have convicted, so a busy box
leaves evidence instead of a rejection.
================
*/
async function acceptTestBudget( output ) {
	const { over, forgiven, offenders } = overBudgetPackages(
		parseTestRuns( output ),
		readTestCpuSeconds(),
		budgetLedger,
		TEST_BUDGET_SECONDS
	);
	for ( const entry of forgiven ) {
		console.log(
			`test budget: ${entry.pkg} took ${entry.wallSeconds}s wall but ` +
				`${formatSeconds( entry.cpuSeconds * 1000 )}s CPU (budget ${TEST_BUDGET_SECONDS}s CPU); ` +
				"the wall clock is not counted on a busy box"
		);
	}
	for ( const entry of over ) {
		const measured = entry.cpuSeconds === null ?
			`${entry.wallSeconds}s wall (no CPU record; the wall verdict stands)` :
			`${formatSeconds( entry.cpuSeconds * 1000 )}s CPU (${entry.wallSeconds}s wall)`;
		console.log(
			`test budget: ${entry.pkg} took ${measured} (budget ${TEST_BUDGET_SECONDS}s CPU)` +
				`${entry.listed ? ", in ledger" : ""}`
		);
	}
	if ( offenders.length ) {
		console.log(
			`test budget: ${offenders.length} package(s) over ${TEST_BUDGET_SECONDS}s CPU; make them faster or move ` +
				"evidence sweeps to tier.Long (internal/testsupport/tier)"
		);
	}
	return offenders.length === 0;
}

/*
================
readTestCpuSeconds

The CPU seconds (user + system) the supervisor recorded per package
this run, by import path. A torn or unreadable record counts as no
record: the package keeps the wall verdict.
================
*/
function readTestCpuSeconds() {
	const cpu = new Map();
	let names = [];
	try {
		names = readdirSync( testCpuDirectory );
	} catch {
		return cpu;
	}
	for ( const name of names ) {
		if ( !name.endsWith( ".json" ) ) {
			continue;
		}
		try {
			const entry = JSON.parse( readFileSync( path.join( testCpuDirectory, name ), "utf8" ) );
			if ( !Number.isFinite( entry.userMs ) || !Number.isFinite( entry.sysMs ) ) {
				continue;
			}
			const relative = path.relative( serverRoot, entry.dir ).split( path.sep ).join( "/" );
			cpu.set(
				relative === "" ? modulePath : `${modulePath}/${relative}`,
				(entry.userMs + entry.sysMs) / 1000
			);
		} catch {
			// No usable record: the wall verdict stands for that package.
		}
	}
	return cpu;
}

/*
================
readBudgetLedger

The packages still allowed over the budget, one import path per line; the
ledger only shrinks.
================
*/
function readBudgetLedger() {
	const file = path.join( rebuildRoot, "scripts", "checks", "go-test-budget.txt" );
	let text = "";
	try {
		text = readFileSync( file, "utf8" );
	} catch {
		return [];
	}
	return text.split( "\n" ).map( ( line ) => line.replace( /#.*/, "" ).trim() ).filter( Boolean );
}

/*
================
changedGoPackages

The module's packages (as ./dir) that hold a .go file changed since the
merge base with origin/main, committed, staged, unstaged or untracked, and
whether go.mod, go.sum or the lint tool module changed.
================
*/
function changedGoPackages() {
	const git = ( args ) => {
		const result = spawnSync( "git", args, { cwd: rebuildRoot, encoding: "utf8" } );
		if ( result.status !== 0 ) throw new Error( `git ${args.join( " " )}: ${result.stderr.trim()}` );
		return result.stdout.split( "\n" ).map( ( line ) => line.trim() ).filter( Boolean );
	};
	const [base] = git( [ "merge-base", "HEAD", "origin/main" ] );
	const files = new Set( [
		...git( [ "diff", "--name-only", base, "--", "apps/server" ] ),
		...git( [ "ls-files", "--others", "--exclude-standard", "--", "apps/server" ] )
	] );
	const packages = new Set();
	for ( const file of files ) {
		if ( !file.endsWith( ".go" ) ) continue;
		try {
			statSync( path.join( rebuildRoot, file ) );
		} catch {
			continue; // deleted: its package's other files still count if changed
		}
		packages.add( "./" + path.posix.dirname( file.slice( "apps/server/".length ) ) );
	}
	return {
		base,
		packages: [ ...packages ].sort(),
		goMod: files.has( "apps/server/go.mod" ) || files.has( "apps/server/go.sum" ),
		lintTool: [ ...files ].some( isLintTool )
	};
}

/*
================
scopeSteps

The full step list, or under --changed its pre-push form (see the header):
the package-wide steps narrowed to the changed packages, a whole-module
build added first, and the steps the scope leaves to the full gate dropped.
================
*/
function scopeSteps( steps ) {
	if ( !changedScope ) return steps;
	const changed = changedGoPackages();
	console.log(
		`server gates: changed scope since ${changed.base.slice( 0, 9 )}: ${changed.packages.length} packages`
	);
	if ( changed.packages.length === 0 ) return [];
	const testPackages = [ ...new Set( [ ...changed.packages, "./internal/gates" ] ) ];
	const raced = racePackages.filter( ( pkg ) => changed.packages.includes( pkg ) );
	const scoped = [ { label: "build", command: "go", args: [ "build", "./..." ], select: () => true, keys: [] } ];
	for ( const step of steps ) {
		switch ( step.label ) {
			case "tidy":
				if ( changed.goMod ) scoped.push( step );
				break;
			case "tidy (lint tool)":
				if ( changed.lintTool ) scoped.push( step );
				break;
			case "vet":
				scoped.push( { ...step, args: [ "vet", ...changed.packages ] } );
				break;
			case "golangci-lint":
				// Serialized: another session's lint run holds the cache lock.
				scoped.push( {
					...step,
					args: [ ...step.args.slice( 0, -1 ), "--allow-serial-runners", ...changed.packages ]
				} );
				break;
			case "tests":
				scoped.push( { ...step, args: [ ...step.args.slice( 0, -1 ), ...testPackages ] } );
				break;
			case "race":
				if ( raced.length ) {
					scoped.push( { ...step, args: [ ...step.args.slice( 0, -racePackages.length ), ...raced ] } );
				}
				break;
			case "gofmt":
				scoped.push( step );
				break;
			default:
				break; // govulncheck and the release contract: the full gate's
		}
	}
	return scoped;
}

/*
================
selectSteps

The steps a comma-separated label list names, in gate order; every step
when the list is empty. A label no step has is a configuration error.
================
*/
function selectSteps( list, steps ) {
	const wanted = (list ?? "").split( "," ).map( ( label ) => label.trim() ).filter( Boolean );
	if ( !wanted.length ) return steps;
	const unknown = wanted.filter( ( label ) => !steps.some( ( step ) => step.label === label ) );
	if ( unknown.length ) throw new Error( `SRO_GO_GATES names unknown steps: ${unknown.join( ", " )}` );
	return steps.filter( ( step ) => wanted.includes( step.label ) );
}

/*
================
wave

Runs the steps concurrently. A step whose inputs match a recorded pass
(checkStamps.mjs) reports "up to date" instead of running. Each step's
output is captured, so concurrent steps never interleave; a failure prints
one failureReport block per failed step. A step fails on a non-zero exit,
or when its accept() predicate rejects the output; a pass records its
stamp.
================
*/
async function wave( name, steps ) {
	const waveStarted = performance.now();
	const results = await Promise.all( steps.map( async ( step ) => {
		if ( changedScope ) return runStep( step.label, step.command, step.args, step.accept );
		const stampName = `server-${step.label.replace( /[^a-z0-9]+/gi, "-" )}`;
		const key = fingerprintOf( moduleFiles, step.select, step.keys );
		if ( findPass( stampName, key ) ) {
			return { label: step.label, ok: true, output: "", seconds: "up to date" };
		}
		const result = await runStep( step.label, step.command, step.args, step.accept );
		if ( result.ok ) recordPass( stampName, key );
		return result;
	} ) );
	const failed = results.filter( ( result ) => !result.ok );
	if ( failed.length > 0 ) {
		// Persisted before anything can lose them: a captured log that
		// truncates keeps no step name to attribute, the artifact does.
		const artifactDir = path.join( rebuildRoot, ".state", "gate-failures", name );
		const written = await persistFailureReports( failed, artifactDir );
		// Drained before the throw, or a pipe keeps only the report's head.
		await printFailureReports( failed );
		if ( written.length > 0 ) {
			process.stderr.write( `failure reports also written to ${artifactDir}\n` );
		}
		throw new Error(
			`server ${name}: ${failed.map( ( result ) => failureSummary( result ) ).join( ", " )}`
		);
	}
	console.log(
		`server ${name}: ${
			results.map( ( result ) =>
				`${result.label} ${result.seconds}${result.seconds === "up to date" ? "" : "s"}`
			)
				.join( ", " )
		} ` +
			`(wave ${formatSeconds( performance.now() - waveStarted )}s)`
	);
}

/*
================
matchesDeclaredServerContract

The sro-release-contract report equals the server declaration field for
field; anything else (extra, missing or different) is a failed step.
================
*/
function matchesDeclaredServerContract( output ) {
	const declared = JSON.parse( readFileSync( path.join( serverRoot, "config/compatibility.json" ), "utf8" ) ).server;
	// The step's output also carries Go's own progress ("go: downloading ..."
	// on a cold module cache): the contract is the one JSON line it prints.
	const line = output.split( /\r?\n/ ).reverse().find( ( row ) => row.trim().startsWith( "{" ) );
	let compiled;
	try {
		compiled = JSON.parse( line ?? "" );
	} catch {
		return false;
	}
	const keys = Object.keys( declared ).sort();
	return JSON.stringify( keys ) === JSON.stringify( Object.keys( compiled ).sort() ) &&
		keys.every( ( key ) => compiled[key] === declared[key] );
}

/*
================
vulnerabilityDatabaseModified

The Go vulnerability database's modification time, re-read from the index
at most every VULNERABILITY_DATABASE_KEY_MS. When it cannot be read the key
is unique, so govulncheck runs (and reports the network failure itself)
rather than being skipped on a stale verdict.
================
*/
async function vulnerabilityDatabaseModified() {
	try {
		const kept = JSON.parse( readFileSync( vulnerabilityDatabaseKeyPath, "utf8" ) );
		if ( Date.now() - kept.readAt < VULNERABILITY_DATABASE_KEY_MS && typeof kept.modified === "string" ) {
			return kept.modified;
		}
	} catch {
		// No kept key yet: read the index.
	}
	try {
		const response = await fetch( VULNERABILITY_DATABASE_INDEX, { signal: AbortSignal.timeout( 5000 ) } );
		if ( !response.ok ) throw new Error( `HTTP ${response.status}` );
		const index = await response.json();
		if ( typeof index.modified !== "string" ) throw new Error( "no modified field" );
		mkdirSync( path.dirname( vulnerabilityDatabaseKeyPath ), { recursive: true } );
		writeFileSync(
			vulnerabilityDatabaseKeyPath,
			JSON.stringify( { modified: index.modified, readAt: Date.now() } )
		);
		return index.modified;
	} catch ( error ) {
		return `unavailable ${Date.now()} ${error}`;
	}
}

/*
================
commandOutput

A command's trimmed stdout, without blocking the event loop.
================
*/
function commandOutput( command, args ) {
	return new Promise( ( resolve ) => {
		const child = spawn( command, args, { windowsHide: true, cwd: serverRoot } );
		let output = "";
		child.stdout.on( "data", ( chunk ) => {
			output += chunk;
		} );
		child.on( "error", ( error ) => resolve( `error ${error}` ) );
		child.on( "close", () => resolve( output.trim() ) );
	} );
}

/*
================
goEnvironment

The environment every Go step runs with, built on first use: when every
step is up to date, no compiler lookup runs.
================
*/
function goEnvironment() {
	if ( goEnvironmentValue === undefined ) {
		const env = { ...compilerFirstEnvironment(), SRO_GAME_DATA_IDENTITY: gameIdentity };
		// The -exec supervisor's directory goes first, so the bare name
		// in -exec resolves to the binary prepareTestSupervisor built.
		const pathKey = Object.keys( env ).find( ( key ) => key.toUpperCase() === "PATH" ) ?? "PATH";
		env[pathKey] = `${supervisorDirectory}${path.delimiter}${env[pathKey] ?? ""}`;
		if ( budgetApplies ) {
			env.SRO_GO_TEST_CPU_DIR = testCpuDirectory;
		}
		goEnvironmentValue = env;
	}
	return goEnvironmentValue;
}

/*
================
runStep
================
*/
/**
 * @param {string} label
 * @param {string} command
 * @param {string[]} args
 * @param {((output: string) => boolean) | undefined} accept
 * @returns {Promise<{ label: string, ok: boolean, code: number | null, signal: string | null, output: string, seconds: string }>}
 */
function runStep( label, command, args, accept ) {
	const stepStarted = performance.now();
	return new Promise( ( resolve ) => {
		const child = spawn( command, args, { windowsHide: true, cwd: serverRoot, env: goEnvironment() } );
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
		child.on( "close", async ( code, signal ) => {
			const ok = code === 0 && (accept === undefined || (await accept( output )));
			resolve( { label, ok, code, signal, output, seconds: formatSeconds( performance.now() - stepStarted ) } );
		} );
	} );
}

/*
================
compilerFirstEnvironment

Returns the process environment with the directory of Go's C compiler
(`go env CC`, resolved on PATH) moved to the front of PATH on Windows, so
the compiler loads its own runtime DLLs. Elsewhere, or when the compiler
cannot be found, the environment is returned unchanged and Go reports the
missing compiler itself.
================
*/
function compilerFirstEnvironment() {
	if ( process.platform !== "win32" ) {
		return process.env;
	}
	const compiler =
		spawnSync( "go", [ "env", "CC" ], { windowsHide: true, cwd: serverRoot, encoding: "utf8" } ).stdout?.trim() ||
		"gcc";
	const located = spawnSync( "where.exe", [ compiler ], { windowsHide: true, encoding: "utf8" } );
	const compilerPath = located.status === 0 ? located.stdout.split( /\r?\n/ )[0].trim() : "";
	if ( compilerPath.length === 0 ) {
		return process.env;
	}

	const pathKey = Object.keys( process.env ).find( ( key ) => key.toUpperCase() === "PATH" ) ?? "PATH";
	return {
		...process.env,
		[pathKey]: `${path.dirname( compilerPath )}${path.delimiter}${process.env[pathKey] ?? ""}`
	};
}

/*
================
gameDataIdentity

A digest of the game data the server's tests read from outside the
module: the stat of every retail textdata file and the published pack
manifest's bytes (which pin every packed asset by sha256). Absent data
contributes its absence, so building it later changes the identity.
================
*/
function gameDataIdentity() {
	const hash = createHash( "sha256" );
	try {
		for ( const name of readdirSync( retailTextdataRoot ).sort() ) {
			const info = statSync( path.join( retailTextdataRoot, name ) );
			hash.update( `${name}|${info.size}|${info.mtimeMs}\n` );
		}
	} catch ( error ) {
		hash.update( `textdata ${error.code ?? error}\n` );
	}
	try {
		hash.update( readFileSync( path.join( publicAssetsRoot, "packs", "manifest.json" ) ) );
	} catch ( error ) {
		hash.update( `packs ${error.code ?? error}\n` );
	}
	return hash.digest( "hex" );
}

/*
================
positiveInteger
================
*/
/**
 * @param {string | undefined} value
 * @param {number} fallback
 */
function positiveInteger( value, fallback ) {
	const parsed = Number( value );
	return Number.isInteger( parsed ) && parsed > 0 ? parsed : fallback;
}

/*
================
formatSeconds
================
*/
/** @param {number} elapsedMs */
function formatSeconds( elapsedMs ) {
	return (elapsedMs / 1000).toFixed( 1 );
}
