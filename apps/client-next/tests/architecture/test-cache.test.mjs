/*
===========================================================================

test-cache.test.mjs - the test runner reuses a pass only while its inputs hold

Drives tools/run-tests.mjs on a two-file fixture suite under temp/ (the
recorder ignores the OS temporary directory, so the fixture must live in the
checkout): a pass is reused, an edit to a module one test imports reruns
exactly that test, a failure is never reused, and a test process cannot
grow past the runner's heap ceiling or its resident memory ceiling, nor
outlive its runner.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { root } from "../../tools/project.mjs";

const runner = path.join( root, "tools/run-tests.mjs" );

/*
================
runSuite

Runs the fixture suite; returns its exit code and summary line.
================
*/
function runSuite( suite, environment = {} ) {
	// A top-level run: without node --test's child context, which would turn
	// the runner's own reporting into the parent's protocol.
	const { NODE_TEST_CONTEXT, ...env } = process.env;
	const result = spawnSync( process.execPath, [ runner, suite ], {
		cwd: root,
		encoding: "utf8",
		env: { ...env, ...environment, SRO_TEST_CACHE_DIR: path.join( suite, ".cache" ), SRO_CHECK_FORCE: "" }
	} );
	const summary = result.stdout.split( "\n" ).find( line => line.startsWith( "tests:" ) ) ?? "";
	const counts = /(\d+) reused unchanged, (\d+) run, (\d+) failed/.exec( summary );
	assert.ok(
		counts,
		`runner summary missing (status ${result.status}, signal ${result.signal}, ${result.error}):\n` +
			`${result.stdout}\n${result.stderr}`
	);
	return {
		status: result.status,
		reused: Number( counts[1] ),
		ran: Number( counts[2] ),
		failed: Number( counts[3] )
	};
}

test("a pass is reused until an input it read changes; a failure is never reused", () => {
	const suite = path.join( root, "temp", `test-cache-fixture-${process.pid}` );
	fs.mkdirSync( suite, { recursive: true } );
	try {
		const dependency = path.join( suite, "value.mjs" );
		fs.writeFileSync( dependency, "export const value = 1;\n" );
		fs.writeFileSync(
			path.join( suite, "reads.test.mjs" ),
			'import { test } from "node:test";\nimport assert from "node:assert/strict";\n' +
				'import { value } from "./value.mjs";\ntest( "reads", () => assert.equal( value, 1 ) );\n'
		);
		fs.writeFileSync(
			path.join( suite, "alone.test.mjs" ),
			'import { test } from "node:test";\ntest( "alone", () => {} );\n'
		);

		assert.deepEqual( runSuite( suite ), { status: 0, reused: 0, ran: 2, failed: 0 } );
		assert.deepEqual(
			runSuite( suite ),
			{ status: 0, reused: 2, ran: 0, failed: 0 },
			"unchanged inputs reuse both"
		);

		assert.deepEqual(
			runSuite( suite, {
				SRO_REBUILD_LOCK_NAME: "generated-assets",
				SRO_REBUILD_LOCK_TOKEN: "next-verification-owner",
				SRO_REBUILD_LOCK_DIR: path.join( suite, "owner.lock" )
			} ),
			{ status: 0, reused: 2, ran: 0, failed: 0 },
			"a different verification lock owner preserves unchanged test passes"
		);

		fs.appendFileSync( dependency, "// edited\n" );
		assert.deepEqual( runSuite( suite ), { status: 0, reused: 1, ran: 1, failed: 0 }, "the importer reruns" );

		fs.writeFileSync( dependency, "export const value = 2;\n" );
		assert.deepEqual( runSuite( suite ), { status: 1, reused: 1, ran: 1, failed: 1 } );
		assert.deepEqual( runSuite( suite ), { status: 1, reused: 1, ran: 1, failed: 1 }, "a failure reruns" );
	} finally {
		fs.rmSync( suite, { recursive: true, force: true } );
	}
});

test("a test process stops at the runner's heap ceiling instead of growing on", () => {
	const suite = path.join( root, "temp", `test-heap-fixture-${process.pid}` );
	fs.mkdirSync( suite, { recursive: true } );
	try {
		// About 512 MB of live arrays: it passes without a ceiling, so the
		// fixture stays bounded even if the ceiling were ever removed.
		fs.writeFileSync(
			path.join( suite, "grows.test.mjs" ),
			'import { test } from "node:test";\nimport assert from "node:assert/strict";\n' +
				'test( "grows", () => {\n\tconst keep = [];\n' +
				"\tfor ( let i = 0; i < 64; i++ ) keep.push( new Array( 1 << 20 ).fill( i ) );\n" +
				"\tassert.equal( keep.length, 64 );\n} );\n"
		);
		assert.deepEqual(
			runSuite( suite, { SRO_TEST_HEAP_MB: "64" } ),
			{ status: 1, reused: 0, ran: 1, failed: 1 },
			"a test past the ceiling fails instead of allocating on"
		);
	} finally {
		fs.rmSync( suite, { recursive: true, force: true } );
	}
});

test("a test process is killed at the resident memory ceiling, typed arrays included", () => {
	const suite = path.join( root, "temp", `test-rss-fixture-${process.pid}` );
	fs.mkdirSync( suite, { recursive: true } );
	try {
		// 64 typed arrays of 8 MB, outside the JS heap ceiling's reach, built
		// slowly enough for the watchdog to look. About 512 MB at most, so it
		// stays bounded (and passes) if the watchdog were ever removed.
		fs.writeFileSync(
			path.join( suite, "grows.test.mjs" ),
			`import { test } from "node:test";
import assert from "node:assert/strict";
test( "grows", () => {
	const keep = [];
	for ( let i = 0; i < 64; i++ ) {
		keep.push( new Float32Array( 2 << 20 ).fill( i ) );
		const until = Date.now() + 25;
		while ( Date.now() < until );
	}
	assert.equal( keep.length, 64 );
} );
`
		);
		assert.deepEqual(
			runSuite( suite, { SRO_TEST_RSS_MB: "256" } ),
			{ status: 1, reused: 0, ran: 1, failed: 1 },
			"a test past the resident memory ceiling is killed instead of allocating on"
		);
	} finally {
		fs.rmSync( suite, { recursive: true, force: true } );
	}
});

test("a test process whose runner is gone exits", () => {
	const watchdog = path.join( root, "tools/lib/test-process-watchdog.mjs" );
	// A stand-in runner, killed below. The orphan spins in a synchronous loop
	// for at most ORPHAN_LIMIT_MS, so it ends on its own if the watchdog
	// were ever removed.
	const ORPHAN_LIMIT_MS = 20000;
	const runnerStandIn = spawn( process.execPath, [ "-e", "setTimeout( () => {}, 60000 );" ], { stdio: "ignore" } );
	try {
		const started = Date.now();
		const orphan = spawnSync(
			process.execPath,
			[
				"--import",
				pathToFileURL( watchdog ).href,
				"-e",
				// The runner dies first; the orphan then spins where no timer of its
				// own could fire.
				`process.kill( ${runnerStandIn.pid} ); const until = Date.now() + ${ORPHAN_LIMIT_MS};` +
				"while ( Date.now() < until );"
			],
			{
				encoding: "utf8",
				env: { ...process.env, SRO_TEST_RUNNER_PID: String( runnerStandIn.pid ), SRO_TEST_RSS_MB: "" }
			}
		);
		const elapsed = Date.now() - started;
		assert.match( orphan.stderr, /its test runner \(pid \d+\) is gone/ );
		assert.ok( elapsed < ORPHAN_LIMIT_MS / 2, `the orphan ran ${elapsed} ms after its runner died` );
	} finally {
		runnerStandIn.kill();
	}
});

test("only the test's own temporary files are excluded from its inputs", async () => {
	const os = await import( "node:os" );
	const { scratchPath } = await import( "../../tools/lib/test-input-recorder.mjs" );
	const temp = path.resolve( os.tmpdir() );
	assert.equal( scratchPath( path.join( temp, "fixture", "a.bin" ) ), true );
	// A sibling sharing the prefix is not inside the temporary directory.
	assert.equal( scratchPath( temp + "-sibling" + path.sep + "a.bin" ), false );
	// The checkout's files are inputs wherever the checkout lives.
	assert.equal( scratchPath( path.join( root, "src", "bootstrap.ts" ) ), false );
	assert.equal( scratchPath( path.join( root, "temp", "fixture.mjs" ) ), false );
});
