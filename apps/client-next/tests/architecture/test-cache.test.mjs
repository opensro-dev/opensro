/*
===========================================================================

test-cache.test.mjs - the test runner reuses a pass only while its inputs hold

Drives tools/run-tests.mjs on a two-file fixture suite under temp/ (the
recorder ignores the OS temporary directory, so the fixture must live in the
checkout): a pass is reused, an edit to a module one test imports reruns
exactly that test, and a failure is never reused.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const runner = path.join( root, "tools/run-tests.mjs" );

/*
================
runSuite

Runs the fixture suite; returns its exit code and summary line.
================
*/
function runSuite( suite ) {
	// A top-level run: without node --test's child context, which would turn
	// the runner's own reporting into the parent's protocol.
	const { NODE_TEST_CONTEXT, ...env } = process.env;
	const result = spawnSync( process.execPath, [ runner, suite ], {
		cwd: root,
		encoding: "utf8",
		env: { ...env, SRO_TEST_CACHE_DIR: path.join( suite, ".cache" ), SRO_CHECK_FORCE: "" }
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

		fs.appendFileSync( dependency, "// edited\n" );
		assert.deepEqual( runSuite( suite ), { status: 0, reused: 1, ran: 1, failed: 0 }, "the importer reruns" );

		fs.writeFileSync( dependency, "export const value = 2;\n" );
		assert.deepEqual( runSuite( suite ), { status: 1, reused: 1, ran: 1, failed: 1 } );
		assert.deepEqual( runSuite( suite ), { status: 1, reused: 1, ran: 1, failed: 1 }, "a failure reruns" );
	} finally {
		fs.rmSync( suite, { recursive: true, force: true } );
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
