/*
===========================================================================

buildParallelism.test.mjs - SRO_BUILD_JOBS sizes the asset build

The default leaves one core free and runs no more jobs than fit in the
memory budget, an explicit value wins, and anything but a positive integer
is refused instead of silently falling back.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { buildJobs, JOB_MEMORY_MB } from "../../build/shared/buildParallelism.mjs";

const PLENTY = { cores: 16, freeMB: 64 * 1024 };

test("with memory to spare the default is every core but one, never below one", () => {
	assert.equal( buildJobs( {}, PLENTY ), 15 );
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "" }, PLENTY ), 15 );
	assert.equal( buildJobs( {}, { cores: 1, freeMB: 64 * 1024 } ), 1 );
});

test("the default runs no more jobs than half the free memory holds", () => {
	// A 4 GB machine with 2 GB free: 1 GB budget, four jobs, not fifteen.
	assert.equal( buildJobs( {}, { cores: 16, freeMB: 2048 } ), Math.floor( 1024 / JOB_MEMORY_MB ) );
	// Under one job's worth free still builds, one job at a time.
	assert.equal( buildJobs( {}, { cores: 16, freeMB: 100 } ), 1 );
});

test("an explicit memory budget replaces the free-memory one", () => {
	assert.equal( buildJobs( { SRO_BUILD_MEMORY_MB: String( 3 * JOB_MEMORY_MB ) }, PLENTY ), 3 );
	assert.throws( () => buildJobs( { SRO_BUILD_MEMORY_MB: "lots" }, PLENTY ), /SRO_BUILD_MEMORY_MB must be/ );
});

test("an explicit job count wins over both limits", () => {
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "1" }, PLENTY ), 1 );
	assert.equal( buildJobs( { SRO_BUILD_JOBS: "48" }, { cores: 2, freeMB: 100 } ), 48 );
});

test("a job count that is not a positive integer is refused", () => {
	for ( const raw of [ "0", "-2", "1.5", "many" ] ) {
		assert.throws( () => buildJobs( { SRO_BUILD_JOBS: raw } ), /SRO_BUILD_JOBS must be a positive integer/ );
	}
});

test("the module fixes one job count for the run and sizes libuv's pool", () => {
	assert.ok( Number( process.env.SRO_BUILD_JOBS ) >= 1 );
	assert.ok( Number( process.env.UV_THREADPOOL_SIZE ) >= 4 );
});
