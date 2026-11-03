/*
===========================================================================

skillSpineAim.test.mjs - the Spine_Base aim in the skill effect records

skilleffect column 23 is the rotation axis 91DAE0 stores at record+0xBC
(none, Roll, Yaw, Pitch, RollR, YawR, PitchR). A nonzero axis arms
CIDecoSkill's aim; 916AA0's last AT_MOV_* stage supplies its height
bindings and its last stage releases it.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";
import { buildEffectRecordTable } from "../../build/char/parseSkillEffect.mjs";
import { isQinShiTombSkillEffectName, qinshiTombEnabled } from "../../build/shared/qinshiTomb.mjs";
import { clientV150ResinfoRoot, extractedRoot, retailTextdataRoot } from "../../build/world/paths.mjs";

/*
================
records
================
*/
function records() {
	return buildEffectRecordTable(
		retailTextdataRoot,
		path.join( retailTextdataRoot, "skilleffect.txt" ),
		path.join( extractedRoot, "Data_extracted", "prim", "snd" ),
		path.join( clientV150ResinfoRoot, "skilleffect.txt" )
	).table;
}

// The Qin-Shi tomb family's spine-aim records (record name:axis), counted
// apart from the shipped data: parseSkillAniSet admits the family's
// Service=0 skillaniset2 rows only under SRO_QINSHI_TOMB
// (parseSkillEffect.mjs:268-275), so these records exist
// only in a tomb bake. Named one by one so a drift in the overlay or the
// admission says which record moved.
const TOMB_SPINE_AIM_ROWS = [
	"MSKILL_TQ_BLACKSNAKE_ATTACK01:2",
	"MSKILL_TQ_BLACKSNAKE_ATTACK02:2",
	"MSKILL_TQ_BLACKSNAKE_ATTACK03:2",
	"MSKILL_TQ_FLAMEMASTER_ATTACK01:1",
	"MSKILL_TQ_SNAKESLAVE_ATTACK01:2",
	"MSKILL_TQ_SNAKESLAVE_ATTACK03:2",
	"MSKILL_TQ_SNAKESLAVE_CLON2_ATTACK01:2",
	"MSKILL_TQ_SNAKESLAVE_CLON2_ATTACK03:2",
	"MSKILL_TQ_SNAKESLAVE_CLON_ATTACK01:2",
	"MSKILL_TQ_SNAKESLAVE_CLON_ATTACK02:2",
	"MSKILL_TQ_SNAKESLAVE_CLON_ATTACK03:2",
	"MSKILL_TQ_TOMBARCHER_ATTACK01:4",
	"MSKILL_TQ_TOMBARCHER_ATTACK06:1",
	"MSKILL_TQ_TOMBARCHER_CLON2_ATTACK01:4",
	"MSKILL_TQ_TOMBARCHER_CLON2_ATTACK04:1",
	"MSKILL_TQ_TOMBARCHER_CLON_ATTACK01:4",
	"MSKILL_TQ_TOMBARCHER_CLON_ATTACK05:1",
	"MSKILL_TQ_TOMBGENERAL1_ATTACK01:2",
	"MSKILL_TQ_TOMBGENERAL1_ATTACK02:2",
	"MSKILL_TQ_TOMBGENERAL2_ATTACK01:2",
	"MSKILL_TQ_TOMBGENERAL2_ATTACK02:2",
	"MSKILL_TQ_TOMBGENERAL3_ATTACK01:2",
	"MSKILL_TQ_TOMBGENERAL3_ATTACK02:2",
	"MSKILL_TQ_TOMBGENERAL4_ATTACK01:2",
	"MSKILL_TQ_TOMBGENERAL4_ATTACK02:2"
];

test("bows aim their spine from the right hand at the target, released at the shot", () => {
	const table = records();
	const bow = Object.values( table ).find( row => row.animBaseName === "SKILL_CH_BOW_BASE" );
	assert.deepEqual( bow?.spineAim, {
		axis: 1,
		start: { bone: "Bip01 R Hand", offsetY: 0, addHeight: false },
		target: { bone: null, offsetY: 10, addHeight: false },
		release: { phase: "SHOT", event: 1 }
	} );
	const axes = {};
	const tombRows = [];
	for ( const row of Object.values( table ) ) {
		if ( !row.spineAim ) continue;
		if ( isQinShiTombSkillEffectName( row.animBaseName ) ) {
			tombRows.push( `${row.animBaseName}:${row.spineAim.axis}` );
			continue;
		}
		axes[row.spineAim.axis] = (axes[row.spineAim.axis] ?? 0) + 1;
	}
	// Rows with a record of their own: PSKILL_P_WOLF_01_ATTACK01_SMALL draws
	// its Basic_Original's (917240), a { remapOf } alias with no spine aim.
	assert.deepEqual( axes, { 1: 373, 2: 239, 4: 11 }, "Roll, Yaw and RollR rows of the shipped data" );
	// The tomb term follows the posture both ways: with SRO_QINSHI_TOMB off
	// the admission never happens and the family contributes no rows.
	assert.deepEqual(
		tombRows.sort(),
		qinshiTombEnabled() ? TOMB_SPINE_AIM_ROWS : [],
		"the tomb family's spine-aim rows changed; review the SRO_QINSHI_TOMB admission"
	);
	assert.equal( Object.values( table ).find( row => row.animBaseName === "SKILL_CH_SWORD_BASE" )?.spineAim, null );
});
