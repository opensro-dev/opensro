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
	for ( const row of Object.values( table ) ) {
		if ( row.spineAim ) axes[row.spineAim.axis] = (axes[row.spineAim.axis] ?? 0) + 1;
	}
	assert.deepEqual( axes, { 1: 373, 2: 240, 4: 11 }, "Roll, Yaw and RollR rows of the shipped data" );
	assert.equal( Object.values( table ).find( row => row.animBaseName === "SKILL_CH_SWORD_BASE" )?.spineAim, null );
});
