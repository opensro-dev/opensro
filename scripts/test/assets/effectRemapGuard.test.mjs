/*
===========================================================================

effectRemapGuard.test.mjs - a Basic_Original remap is one step

917240 follows a skill's Basic_Original once; the build aliases such rows
to their original's record and refuses a table in which an alias names
another alias, which the client would reject whole.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";
import { aliasBasicOriginals, buildEffectRecordTable, loadSkillDataRows } from "../../build/char/parseSkillEffect.mjs";
import { clientV150ResinfoRoot, extractedRoot, retailTextdataRoot } from "../../build/world/paths.mjs";

test("a row whose Basic_Original has a record draws through it", () => {
	const table = { 1: { stages: [] } };
	const remapped = aliasBasicOriginals( table, [
		{ id: 1, originalId: 0 },
		{ id: 2, originalId: 1 },
		{ id: 3, originalId: 9 }
	] );
	assert.equal( remapped, 1 );
	assert.deepEqual( table, { 1: { stages: [] }, 2: { remapOf: 1 } } );
});

test("an alias of an alias fails the build", () => {
	// 2 has a record of its own and a Basic_Original, so it becomes an
	// alias; 3 names 2 and would need a second step.
	const table = { 1: { stages: [] }, 2: { stages: [] } };
	assert.throws(
		() =>
			aliasBasicOriginals( table, [
				{ id: 2, originalId: 1 },
				{ id: 3, originalId: 2 }
			] ),
		/skill 3's Basic_Original 2 is itself remapped/
	);
});

test("the shipped table aliases a pet's levelled attack to its original, one step", () => {
	const { table } = buildEffectRecordTable(
		retailTextdataRoot,
		path.join( retailTextdataRoot, "skilleffect.txt" ),
		path.join( extractedRoot, "Data_extracted", "prim", "snd" ),
		path.join( clientV150ResinfoRoot, "skilleffect.txt" )
	);
	const wolf = loadSkillDataRows( retailTextdataRoot ).find( row => row.levelName === "PSKILL_P_WOLF_002_ATTACK_01" );
	assert.ok( wolf?.originalId, "the levelled wolf attack names a Basic_Original" );
	assert.deepEqual( table[wolf.id], { remapOf: wolf.originalId } );
	assert.equal( table[wolf.originalId]?.remapOf, undefined, "the original is a record, not an alias" );
});
