/*
===========================================================================

Go Server Verification Gate

Module hygiene (`go mod tidy -diff` for the module and the lint tool module,
gofmt, `go vet`, golangci-lint), every package's tests, the race detector on
the concurrency-owning packages, and govulncheck (pinned in go.mod as a
tool). golangci-lint is pinned in its own tool module,
tools/golangci-lint/go.mod, so the repository's Go toolchain builds it.

Speed. The steps are independent, so they all run concurrently: hygiene,
the tests, the race run and govulncheck. Each step's output is printed as
one block when it finishes; any failure fails the gate. Tests run as one
`go test ./...` with SRO_GO_TEST_PARALLELISM package workers (default: half
the cores), so one slow package no longer stalls a whole shard (measured
2026-09-27: 40s with 8 workers against 61s for eight-package shards at 2).

Go's test cache stays off (-count=1). The integration tests read the large
game-data projection, and validating a cached result re-hashes every file a
test opened: a fully cached re-run measured 74s, slower than running the
tests. SRO_GO_TEST_CACHE=on enables it for trees where that is not true.

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
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDirectory, "..", ".." );
const serverRoot = path.join( rebuildRoot, "apps", "server" );
const packageParallelism = positiveInteger(
	process.env.SRO_GO_TEST_PARALLELISM,
	Math.max( 2, Math.floor( os.availableParallelism() / 2 ) )
);
const testCache = process.env.SRO_GO_TEST_CACHE === "on";
const startedAt = performance.now();
const lintModfile = "tools/golangci-lint/go.mod";
const goEnvironment = compilerFirstEnvironment();
const racePackages = [
	"./internal/agent/api",
	"./internal/agent/server",
	"./internal/security/auth",
	"./internal/transport/worldsession",
	"./internal/cluster/shard",
	"./internal/data/store",
	"./internal/transport"
];

const testArgs = [ "test", `-p=${packageParallelism}`, ...(testCache ? [] : [ "-count=1" ]), "./..." ];
await wave( "all", [
	[ "tidy", "go", [ "mod", "tidy", "-diff" ] ],
	[ "tidy (lint tool)", "go", [ "-C", path.dirname( lintModfile ), "mod", "tidy", "-diff" ] ],
	[ "gofmt", "gofmt", [ "-l", "cmd", "internal" ], ( output ) => output.trim().length === 0 ],
	[ "vet", "go", [ "vet", "./..." ] ],
	[ "golangci-lint", "go", [ "tool", `-modfile=${lintModfile}`, "golangci-lint", "run", "./..." ] ],
	[ "tests", "go", testArgs ],
	[ "race", "go", [ "test", "-race", ...(testCache ? [] : [ "-count=1" ]), ...racePackages ] ],
	[ "govulncheck", "go", [ "tool", "govulncheck", "./..." ] ]
] );

console.log(
	`server gates: PASS (${packageParallelism} package workers, test cache ${testCache ? "on" : "off"}, ` +
		`${formatSeconds( performance.now() - startedAt )}s)`
);

/*
================
wave

Runs the steps concurrently. Each step's output is captured and printed as
one block when it finishes, so concurrent steps never interleave. A step
fails on a non-zero exit, or when its accept() predicate rejects the output.
================
*/
/**
 * @param {string} name
 * @param {Array<[string, string, string[], ((output: string) => boolean)?]>} steps
 */
async function wave( name, steps ) {
	const waveStarted = performance.now();
	const results = await Promise.all(
		steps.map( ( [label, command, args, accept] ) => runStep( label, command, args, accept ) )
	);
	const failed = results.filter( ( result ) => !result.ok );
	if ( failed.length > 0 ) {
		for ( const result of failed ) {
			process.stderr.write( `\n--- ${result.label} FAILED ---\n${result.output}\n` );
		}
		throw new Error( `server ${name}: ${failed.map( ( result ) => result.label ).join( ", " )} failed` );
	}
	console.log(
		`server ${name}: ${results.map( ( result ) => `${result.label} ${result.seconds}s` ).join( ", " )} ` +
			`(wave ${formatSeconds( performance.now() - waveStarted )}s)`
	);
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
		const child = spawn( command, args, { windowsHide: true, cwd: serverRoot, env: goEnvironment } );
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
