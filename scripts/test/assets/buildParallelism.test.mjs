/*
===========================================================================

buildParallelism.test.mjs - SRO_BUILD_JOBS sizes the asset build

The default leaves one core free, an explicit value wins, and anything but
a positive integer is refused instead of silently falling back.

===========================================================================
*/
import assert from "node:assert/strict";
import { availableParallelism } from "node:os";
import test from "node:test";
import { buildJobs } from "../../build/shared/buildParallelism.mjs";

test("the default is every core but one, never below one", () => {
	assert.equal( buildJobs( {} ), Math.max( 1, availableParallelism() - 1 ) );
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "" } ), Math.max( 1, availableParallelism() - 1 ) );
});

test("an explicit job count wins", () => {
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "1" } ), 1 );
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "48" } ), 48 );
});

test("a job count that is not a positive integer is refused", () => {
	for ( const raw of [ "0", "-2", "1.5", "many" ] ) {
		assert.throws( () => buildJobs( { SRO_BUILD_JOBS: raw } ), /SRO_BUILD_JOBS must be a positive integer/ );
	}
});

test("the module sizes libuv's thread pool for the build", () => {
	assert.ok( Number( process.env.UV_THREADPOOL_SIZE ) >= 4 );
});
