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
import {
	aliasBasicOriginals,
	buildEffectRecordTable,
	HWAN_EFFECTS_WITHOUT_SERVICE,
	loadSkillDataRows
} from "../../build/char/parseSkillEffect.mjs";
import { clientV150ResinfoRoot, extractedRoot, retailTextdataRoot } from "../../build/world/paths.mjs";

// The Blue Berserk records stand only while the shard runs the cap-110
// expansion (levelCap.mjs cap110ExpansionEnabled, #4466).
const EXPANSION = { SRO_LEVEL_CAP: "110", SRO_CAP110_ITEMS: "on" };
const NATIVE_CAP = { SRO_LEVEL_CAP: "90", SRO_CAP110_ITEMS: "on" };

/*
================
withEnv
================
*/
function withEnv( values, run ) {
	const saved = Object.fromEntries( Object.keys( values ).map( key => [ key, process.env[key] ] ) );
	Object.assign( process.env, values );
	try {
		return run();
	} finally {
		for ( const [key, value] of Object.entries( saved ) ) {
			if ( value === undefined ) delete process.env[key];
			else process.env[key] = value;
		}
	}
}

/*
================
retailRecordTable
================
*/
function retailRecordTable() {
	return buildEffectRecordTable(
		retailTextdataRoot,
		path.join( retailTextdataRoot, "skilleffect.txt" ),
		path.join( extractedRoot, "Data_extracted", "prim", "snd" ),
		path.join( clientV150ResinfoRoot, "skilleffect.txt" )
	);
}

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

test("the 2nd Hwan body record ships with the cap-110 expansion although its Service column is 0", () => {
	const { namedTable } = withEnv( EXPANSION, retailRecordTable );
	assert.equal( namedTable.SYSTEM_CH_HWAN2MODE?.damageEffectPath, "hiteffect/hit_4_hwan2mode.efp" );
	assert.ok( namedTable.SYSTEM_CH_HWANMODE, "the first Hwan record is unchanged" );
});

test("a native-cap shard keeps the Blue Berserk records out, as the native Service gate does", () => {
	// Their effects (system_hwan2_*.efp) are absent from the v1.150
	// Particles.pk2: admitted here they failed the build's effect closure.
	const { namedTable } = withEnv( NATIVE_CAP, retailRecordTable );
	for ( const name of HWAN_EFFECTS_WITHOUT_SERVICE ) assert.equal( namedTable[name], undefined, name );
	assert.ok( namedTable.SYSTEM_CH_HWANMODE, "the first Hwan record is unchanged" );
});

test("the Niya General spirit trap draws its ground model with the expansion although its Service column is 0", () => {
	const { table, namedTable } = withEnv( EXPANSION, retailRecordTable );
	for (
		const name of [ "SKILL_QNO_CH_HWAN_1_4_04_01", "SKILL_QNO_CH_WHAN_2_1_01_01", "SKILL_QNO_EU_WHAN_2_1_01_01" ]
	) {
		assert.equal( namedTable[name]?.objectResource?.path, "res/etc/capture_trap.bsr", name );
	}
	const trap = loadSkillDataRows( retailTextdataRoot ).find( row => row.levelName === "SKILL_QNO_CH_HWAN_1_4_04_01" );
	assert.equal( table[trap.id]?.objectResource?.path, "res/etc/capture_trap.bsr", "the skill id carries the record" );
});
