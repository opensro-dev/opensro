/*
===========================================================================

practice-box.test.mjs - the native training confirmation's mastery face

CIFSkillPracticeBox_ConfigureRecord (5DE040) names the next mastery level
and the current level's SP cost; a mastery at level 0 costs nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { masteryPractice, PRACTICE_MASTERY, PRACTICE_SKILL } = await import(
	"../../src/engine/foundation/ui/practice-box.ts"
);

test("the mastery face names the next level and the current level's cost", () => {
	const costs = { 1: 12, 2: 30 };
	assert.deepEqual( masteryPractice( 0, costs ), { nextLevel: 1, cost: 0 } );
	assert.deepEqual( masteryPractice( 1, costs ), { nextLevel: 2, cost: 12 } );
	assert.deepEqual( masteryPractice( 2, costs ), { nextLevel: 3, cost: 30 } );
	assert.equal( masteryPractice( 3, costs ), null, "a missing price is never invented" );
	assert.notEqual( PRACTICE_SKILL, PRACTICE_MASTERY );
});
