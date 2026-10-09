/*
===========================================================================

player-info-job.test.mjs - the character window's job block, as 59FFA0

No job, a thief in progress, a European grade title, the grade-7 cap and
an alias, over a synthetic copy table and threshold rows.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { jobExpThresholds, playerInfoJob } = await import(
	"../../src/engine/foundation/gameplay/player-info-job.ts"
);

/**
 * @param {string} key
 * @returns {string}
 */
const copy = key => key === "UIIT_STT_NONE" ? "None" : key === "UIIT_STT_GRADE" ? "Grade" : "[" + key + "]";
const THRESHOLDS = jobExpThresholds( {
	1: { jobExpTrader: 1000, jobExpThief: 2000, jobExpHunter: 3000 },
	2: { jobExpTrader: 5000, jobExpThief: -1, jobExpHunter: 7000 },
	3: { jobExpTrader: 1 }
} );

test("no job publishes the None rows and an empty gauge", () => {
	const job = playerInfoJob( { type: 0, grade: 0, exp: 0, alias: "" }, 0, THRESHOLDS, copy );
	assert.deepEqual( job, { alias: "<None>", icon: null, title: "<None>", grade: "", exp: "0%", fraction: 0 } );
});

test("a thief shows the alias, icon, grade title and experience share", () => {
	const job = playerInfoJob( { type: 2, grade: 1, exp: 500, alias: "Shade" }, 0, THRESHOLDS, copy );
	assert.equal( job.alias, "Shade" );
	assert.equal( job.icon, "com_job_thief" );
	assert.equal( job.title, "[UIIT_STT_CLASS_THIEF_1]" );
	assert.equal( job.grade, "1 Grade" );
	assert.equal( job.exp, "25% (500)" );
	assert.equal( job.fraction, 0.25 );
});

test("a European character reads the EU grade title", () => {
	const job = playerInfoJob( { type: 3, grade: 2, exp: 0, alias: "" }, 1, THRESHOLDS, copy );
	assert.equal( job.title, "[UIIT_STT_CLASS_EU_HUNTER_2]" );
	assert.equal( job.alias, "<None>" );
});

test("grade 7 is full, and a -1 threshold reads unsigned", () => {
	const top = playerInfoJob( { type: 1, grade: 7, exp: 9, alias: "" }, 0, THRESHOLDS, copy );
	assert.equal( top.exp, "100%" );
	assert.equal( top.fraction, 1 );
	const unsigned = playerInfoJob( { type: 2, grade: 2, exp: 85899346, alias: "" }, 0, THRESHOLDS, copy );
	assert.equal( unsigned.exp, "1% (85899346)" );
});

test("rows without all three job columns are left out", () => {
	assert.equal( THRESHOLDS[3], undefined );
	assert.deepEqual( THRESHOLDS[1], [ 1000, 2000, 3000 ] );
});
