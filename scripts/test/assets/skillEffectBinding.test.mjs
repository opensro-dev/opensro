/*
===========================================================================

skillEffectBinding.test.mjs - skilleffectset bone tokens decode as sub_91e720

A stage row names its start and target anchors with one token each. The
native parser reads a marker off the token into the binding's +0x08 / +0x09
bytes; the name a socket lookup sees never carries that marker.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { SkillEffectSet_ParseRow } from "../../build/char/native/skillEffectSetParseRow.ts";

/*
================
row

A 28-column skilleffectset row with the given start (19) and target (21)
bone tokens; every other column holds a neutral authored value.
================
*/
function row( start, target ) {
	const columns = Array.from( { length: 28 }, () => "none" );
	columns[0] = "-";
	columns[1] = "SKILL_FIXTURE";
	columns[2] = "SHOT";
	columns[12] = "0,0";
	columns[13] = "AT_ONE_FOLLOW";
	columns[14] = "MOV_NONE,0,0,0";
	columns[15] = "0,0,0";
	columns[16] = "0,0,0,0,0,0";
	columns[19] = start;
	columns[20] = "1,2,3";
	columns[21] = target;
	columns[22] = "4,5,6";
	columns[24] = "0";
	return columns;
}

/*
================
bindings
================
*/
function bindings( record ) {
	return {
		start: [ record.startBone, record.startKeepRotation, record.startAddHeight ],
		target: [ record.targetBone, record.targetKeepRotation, record.targetAddHeight ]
	};
}

test("each token arm writes the binding sub_91e720 writes", () => {
	// 0x91f3b2 arms over a record zeroed at 0x91eaca.
	assert.deepEqual( bindings( SkillEffectSet_ParseRow( row( "none", "none" ) ) ), {
		start: [ null, false, false ],
		target: [ null, false, false ]
	} );
	assert.deepEqual( bindings( SkillEffectSet_ParseRow( row( "Bip01 R Hand", "Bip01" ) ) ), {
		start: [ "Bip01 R Hand", true, false ],
		target: [ "Bip01", true, false ]
	} );
	assert.deepEqual( bindings( SkillEffectSet_ParseRow( row( "@Bip01 R Finger2", "none" ) ) ), {
		start: [ "Bip01 R Finger2", false, false ],
		target: [ null, false, false ]
	} );
	assert.deepEqual( bindings( SkillEffectSet_ParseRow( row( "*", "none" ) ) ), {
		start: [ null, true, true ],
		target: [ null, false, false ]
	} );
});

test("a target '*' writes the start binding as the native arm does", () => {
	// 0x91f526..0x91f535 store esp+0x90 / esp+0x98 (start) and esp+0xb1.
	assert.deepEqual( bindings( SkillEffectSet_ParseRow( row( "Bip01 Head", "*" ) ) ), {
		start: [ null, true, false ],
		target: [ null, false, true ]
	} );
});

test("offsets stay the authored native vectors", () => {
	const record = SkillEffectSet_ParseRow( row( "@Bip01", "*" ) );
	assert.deepEqual( record.startOffset, [ 1, 2, 3 ] );
	assert.deepEqual( record.targetOffset, [ 4, 5, 6 ] );
});
