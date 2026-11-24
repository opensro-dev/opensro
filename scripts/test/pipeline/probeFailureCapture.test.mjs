/*
===========================================================================

probeFailureCapture.test.mjs - a failed browser test keeps what it saw

Runs fixtures/probe-capture/stuck.fixture.mjs under node --test in a
scratch directory: the passing test must leave nothing, a stuck test its
picture plus the runtime's account and the requests it still waited on,
whether its finally closed the browser or the page was left open.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const FIXTURE = fileURLToPath( new URL( "../fixtures/probe-capture/stuck.fixture.mjs", import.meta.url ) );
const PNG = Buffer.from( [ 0x89, 0x50, 0x4e, 0x47 ] );

test("only the stuck tests leave a capture, naming what they waited on", async t => {
	const scratch = await mkdtemp( join( tmpdir(), "probe-capture-" ) );
	t.after( () => rm( scratch, { recursive: true, force: true } ) );
	const run = spawnSync( process.execPath, [ "--test", "--test-reporter=tap", FIXTURE ], {
		cwd: scratch,
		encoding: "utf8",
		// A run of its own, not a subtest reporting to this runner: node:test
		// reads the key's presence, so it is removed, not emptied.
		env: Object.fromEntries( Object.entries( process.env ).filter( ( [key] ) => key !== "NODE_TEST_CONTEXT" ) )
	} );
	assert.equal( run.status, 1, "the fixture's stuck tests fail" );
	assert.match( run.stdout, /stuck capture: .*stuck-1\.png/ );

	const directory = join( scratch, "temp", "artifacts", "stuck", "stuck-fixture" );
	const files = (await readdir( directory )).sort();
	assert.deepEqual( files, [
		"stuck-1.png",
		"stuck-1.txt",
		"stuck-with-the-page-left-open-1.png",
		"stuck-with-the-page-left-open-1.txt"
	] );
	assert.deepEqual( await readFile( join( directory, "stuck-1.png" ) ), PNG );
	const closed = await readFile( join( directory, "stuck-1.txt" ), "utf8" );
	assert.match( closed, /in flight: 1\n +\d+ ms GET http:\/\/probe\/src\/main\.ts\n/ );
	assert.doesNotMatch( closed, /title\/servers/, "an answered request is not listed" );
	assert.match( closed, /status: signed-out/ );
	const open = await readFile( join( directory, "stuck-with-the-page-left-open-1.txt" ), "utf8" );
	assert.match( open, /GET http:\/\/probe\/src\/held\.ts/ );
});
