/*
===========================================================================

job-rank.test.mjs - the job guilds' rank windows, as 647E50 and 646D10

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { jobContributionSelf, jobRankPage, jobRankPages } = await import(
	"../../src/engine/foundation/gameplay/job-rank.ts"
);

/**
 * @param {string} key
 * @returns {string}
 */
const copy = key => key === "UIIT_STT_JOBGUILD_MYCONTRIBUTE2" ? "[%s] so far" : "[" + key + "]";
const THRESHOLDS = { 2: /** @type {const} */ ([ 1000, 2000, 4000 ]) };

test("an activity page shows rank, alias, grade, the grade's share and its title", () => {
	const rows = Array.from( { length: 12 }, ( _, i ) => ({ rank: i + 1, alias: "A" + i, grade: 2, value: 1000 }) );
	const page = jobRankPage( { job: 2, kind: 0, rows }, 0, THRESHOLDS, copy );
	assert.equal( page.title, "[UIIT_STT_JOBGUILD_THIEF_MENU_JOBRANK]" );
	assert.equal( page.pages, 2 );
	assert.equal( page.slots.length, 10 );
	assert.deepEqual( page.slots[0], { 10: "1", 11: "A0", 12: "2", 13: "50%", 14: "[UIIT_STT_CLASS_THIEF_2]" } );
	assert.equal( jobRankPage( { job: 2, kind: 0, rows }, 1, THRESHOLDS, copy ).slots.length, 2 );
	assert.equal( jobRankPages( 0 ), 1 );
});

test("a contribution page groups the amount and the window's own entry follows 646FC0", () => {
	const page = jobRankPage(
		{ job: 3, kind: 1, rows: [ { rank: 1, alias: "Watch", grade: 4, value: 1234567 } ] },
		0,
		THRESHOLDS,
		copy
	);
	assert.equal( page.title, "[UIIT_STT_JOBGUILD_HUNTER_MENU_CONTRIBUTERANK]" );
	assert.deepEqual( page.slots[0], { 10: "1", 11: "Watch", 12: "4", 13: "1,234,567" } );
	const hunter = { type: 3, grade: 4, exp: 10, alias: "Watch", contribution: 77 };
	assert.deepEqual( jobContributionSelf( hunter, 3, copy ), {
		note: "[Watch] so far",
		label: "[UIIT_STT_CONTRIBUTE]",
		alias: "Watch",
		grade: "4",
		amount: "77"
	} );
	assert.equal( jobContributionSelf( hunter, 1, copy ), null, "another guild's window shows no own entry" );
	assert.equal( jobContributionSelf( { ...hunter, exp: 0 }, 3, copy ), null, "no job experience, no entry" );
});
