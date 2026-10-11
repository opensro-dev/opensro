/*
===========================================================================

effectRemapGuard.test.mjs - a Basic_Original remap is one step

917240 follows a skill's Basic_Original once; the build refuses a table in
which an alias names another alias, which the client would reject whole.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { assertOneStepRemaps } from "../../build/char/parseSkillEffect.mjs";

test("aliases of authored records pass", () => {
	assert.doesNotThrow( () =>
		assertOneStepRemaps( { 3824: { stages: [] }, 3825: { remapOf: 3824 }, 3833: { remapOf: 3824 } } )
	);
});

test("an alias of an alias fails the build", () => {
	assert.throws(
		() => assertOneStepRemaps( { 1: { stages: [] }, 2: { remapOf: 1 }, 3: { remapOf: 2 } } ),
		/skill 3's Basic_Original 2 is itself remapped/
	);
});
