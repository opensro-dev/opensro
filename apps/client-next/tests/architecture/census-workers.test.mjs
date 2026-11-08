/*
===========================================================================

census-workers.test.mjs - the action census worker cap (#2766)

Each census scene worker holds 300-500 MB; one per scene on a shared PC
ran eight at once. censusWorkerCount is the one place that decides how
many run: SRO_CENSUS_JOBS when it names a positive count (CI raises it),
else the shared-PC default.
===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import { DEFAULT_CENSUS_WORKERS, censusWorkerCount } from "../../tools/censusWorkers.mjs";

/*
================
the default caps the fan-out
================
*/
test("the default caps the fan-out", () => {
	assert.equal( censusWorkerCount( {} ), DEFAULT_CENSUS_WORKERS );
	assert.equal( DEFAULT_CENSUS_WORKERS, 4 );
});

/*
================
SRO_CENSUS_JOBS raises or lowers the cap
================
*/
test("SRO_CENSUS_JOBS raises or lowers the cap", () => {
	assert.equal( censusWorkerCount( { SRO_CENSUS_JOBS: "8" } ), 8 );
	assert.equal( censusWorkerCount( { SRO_CENSUS_JOBS: "1" } ), 1 );
});

/*
================
a count that names nothing usable falls back
================
*/
test("a count that names nothing usable falls back", () => {
	assert.equal( censusWorkerCount( { SRO_CENSUS_JOBS: "0" } ), DEFAULT_CENSUS_WORKERS );
	assert.equal( censusWorkerCount( { SRO_CENSUS_JOBS: "-3" } ), DEFAULT_CENSUS_WORKERS );
	assert.equal( censusWorkerCount( { SRO_CENSUS_JOBS: "many" } ), DEFAULT_CENSUS_WORKERS );
});
