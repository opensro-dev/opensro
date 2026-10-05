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
the tests, the race run and govulncheck. Each step's output is printed as
one block when it finishes; any failure fails the gate. Tests run as one
`go test ./...` with SRO_GO_TEST_PARALLELISM package workers (default: half
the cores), so one slow package no longer stalls a whole shard (measured
2026-09-27: 40s with 8 workers against 61s for eight-package shards at 2).

Go's test cache is on. A cached result is valid only for the inputs it was
produced from, so every input must be visible to Go:
  - the server game-data projection lives inside the module
    (apps/server/.generated), where Go validates the files a test opened;
  - the licensed data outside the module (the retail textdata and the
    published client assets) is named by SRO_LICENSED_DATA_IDENTITY, which
    licensed.RequireGameData reads, so it joins every licensed test's key.
Measured 2026-09-29: the projection outside the module made a fully cached
run cost 105s, because Go resolved symlinks for each of ~89k logged file
operations per package; inside it, a cached run of every package takes 10s.
SRO_GO_TEST_CACHE=off restores -count=1 (flake hunting).
SRO_GO_GATES=label,label runs only the named steps (CI spreads the gate
over parallel runners); an unknown label is an error, never a silent pass.

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
import { mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { publicAssetsRoot, retailTextdataRoot } from "../build/world/paths.mjs";
import { findPass, fingerprintOf, recordPass, snapshotAsync } from "./checkStamps.mjs";

const scriptDirectory = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDirectory, "..", ".." );
const serverRoot = path.join( rebuildRoot, "apps", "server" );
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
const licensedIdentity = licensedDataIdentity();
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
	`=licensed ${licensedIdentity}`,
	`=cache ${testCache} p ${packageParallelism}`,
	toolchain,
	stepEnvironment
];

const testArgs = [ "test", `-p=${packageParallelism}`, ...(testCache ? [] : [ "-count=1" ]), "./..." ];
await wave(
	"all",
	selectSteps( process.env.SRO_GO_GATES, [
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
		{ label: "tests", command: "go", args: testArgs, select: () => true, keys: testKeys },
		{
			label: "race",
			command: "go",
			args: [ "test", "-race", ...(testCache ? [] : [ "-count=1" ]), ...racePackages ],
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
			select: ( file ) => isModuleGo( file ) || file === "apps/server/ops/release/compatibility.json",
			keys: [ toolchain, stepEnvironment ]
		}
	] )
);

console.log(
	`server gates: PASS (${packageParallelism} package workers, test cache ${testCache ? "on" : "off"}, ` +
		`${formatSeconds( performance.now() - startedAt )}s)`
);

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
output is captured and printed as one block when it finishes, so concurrent
steps never interleave. A step fails on a non-zero exit, or when its
accept() predicate rejects the output; a pass records its stamp.
================
*/
async function wave( name, steps ) {
	const waveStarted = performance.now();
	const results = await Promise.all( steps.map( async ( step ) => {
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
		for ( const result of failed ) {
			process.stderr.write( `\n--- ${result.label} FAILED ---\n${result.output}\n` );
		}
		throw new Error( `server ${name}: ${failed.map( ( result ) => result.label ).join( ", " )} failed` );
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
	const declared =
		JSON.parse( readFileSync( path.join( serverRoot, "ops/release/compatibility.json" ), "utf8" ) ).server;
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
	goEnvironmentValue ??= { ...compilerFirstEnvironment(), SRO_LICENSED_DATA_IDENTITY: licensedIdentity };
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
 * @returns {Promise<{ label: string, ok: boolean, output: string, seconds: string }>}
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
		child.on( "close", ( code ) => {
			const ok = code === 0 && (accept === undefined || accept( output ));
			resolve( { label, ok, output, seconds: formatSeconds( performance.now() - stepStarted ) } );
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
licensedDataIdentity

A digest of the licensed data the server's tests read from outside the
module: the stat of every retail textdata file and the published pack
manifest's bytes (which pin every packed asset by sha256). Absent data
contributes its absence, so building it later changes the identity.
================
*/
function licensedDataIdentity() {
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
