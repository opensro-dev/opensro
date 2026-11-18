/*
===========================================================================

spawn_replacement_test.go - the native special-target replace roll

===========================================================================
*/
package monster

import "testing"

/*
================
TestSpawnReplacementHalfOfTheChampions

The winter row (rate 50, grade 1 to 0): a champion becomes the row's
monster when its draw falls in the lower half of 0..32767, keeping its
party nibble; an ordinary spawn never does.
================
*/
func TestSpawnReplacementHalfOfTheChampions(t *testing.T) {
	snowman := MonsterRef{RefObjID: 9001, Codename: "MOB_EV_WINTER_SNOWMAN"}
	rows := []SpawnReplacement{{Ref: snowman, Grade: 1, NewGrade: 0, RatePercent: 50}}
	champion := func(grade uint8, draw uint32) NativeSpawnRoll {
		return NativeSpawnRoll{Nest: NestRow{RefObjID: 7, HasRarityOverride: true, RarityOverride: grade}, ReplaceDraw: draw}
	}
	ref, roll, ok := ApplySpawnReplacement(champion(0x11, 16383), rows)
	if !ok || ref.RefObjID != snowman.RefObjID || roll.Grade() != 0x10 || roll.Nest.RefObjID != 7 {
		t.Fatalf("champion at the half not replaced: ok=%v ref=%d grade=%#x", ok, ref.RefObjID, roll.Grade())
	}
	if _, _, ok := ApplySpawnReplacement(champion(1, 16385), rows); ok {
		t.Fatal("champion above the half replaced")
	}
	if _, _, ok := ApplySpawnReplacement(champion(0, 0), rows); ok {
		t.Fatal("an ordinary spawn replaced")
	}
	if _, _, ok := ApplySpawnReplacement(champion(1, 0), nil); ok {
		t.Fatal("an empty table replaced")
	}
}
