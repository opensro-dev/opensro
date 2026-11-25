/*
===========================================================================

highlevelseals_test.go - the optional level 95-110 seal roll (#4393)

Off, the native rare table is untouched (empty above 94). On, each empty
row from 95 to 110 rolls exactly one class, the level's own Star, at the
highest native row's mass; native rows and the ordinary table are shared
unchanged, and the installed catalog is never mutated in place.

===========================================================================
*/
package loot

import (
	"reflect"
	"testing"
)

const (
	nativeLastRareLevel = 94
	firstFilledLevel    = 95
)

/*
================
TestNativeRareTableEndsAt94

The premise the rule rests on: the shipped catalog's rare rows stop at 94.
================
*/
func TestNativeRareTableEndsAt94(t *testing.T) {
	if _, ok := equipment.equipmentGroup(nativeLastRareLevel, true, 0); !ok {
		t.Fatalf("level %d has no native rare row", nativeLastRareLevel)
	}
	for level := firstFilledLevel; level <= highLevelSealLastLevel; level++ {
		if row := equipment.classes[1][level-1]; len(row) != 0 {
			t.Fatalf("level %d already has a native rare row %+v", level, row)
		}
	}
}

/*
================
TestHighLevelSealsOffIsNative
================
*/
func TestHighLevelSealsOffIsNative(t *testing.T) {
	before := equipment
	InitHighLevelSeals(false)
	if !reflect.DeepEqual(before.classes, equipment.classes) {
		t.Fatal("the rule changed the catalog while off")
	}
	if group, ok := EquipmentGroup(100, true, 0); ok {
		t.Fatalf("level 100 rolls rare class %d with the rule off", group)
	}
}

/*
================
TestHighLevelSealsFillOnlyTheEmptyRowsWithTheLevelsStar
================
*/
func TestHighLevelSealsFillOnlyTheEmptyRowsWithTheLevelsStar(t *testing.T) {
	native := equipment
	nativeRare := append([][]classThreshold(nil), native.classes[1]...)
	filled := withHighLevelSeals(native)
	template := native.classes[1][nativeLastRareLevel-1]
	mass := template[len(template)-1].threshold
	if mass == 0 {
		t.Fatal("the native template row carries no mass")
	}
	for level := 1; level <= nativeLastRareLevel; level++ {
		if !reflect.DeepEqual(filled.classes[1][level-1], native.classes[1][level-1]) {
			t.Fatalf("native rare row %d changed", level)
		}
	}
	filledLevels := 0
	for level := firstFilledLevel; level <= highLevelSealLastLevel; level++ {
		star, ok := native.sealGroupAt(uint8(level), sealTierStar)
		if !ok {
			// No ordinary row (102-110 without the cap-110 catalog):
			// the level drops no equipment, so no seal either.
			if row := filled.classes[1][level-1]; len(row) != 0 {
				t.Fatalf("level %d has no ordinary row but rolls rare %+v", level, row)
			}
			continue
		}
		filledLevels++
		want := []classThreshold{{group: star, threshold: mass}}
		if got := filled.classes[1][level-1]; !reflect.DeepEqual(got, want) {
			t.Fatalf("level %d rare row = %+v, want %+v", level, got, want)
		}
		if group, ok := filled.equipmentGroup(uint8(level), true, mass); !ok || group != star {
			t.Fatalf("level %d: a roll at the mass lands on %d (%t), want %d", level, group, ok, star)
		}
		if _, ok := filled.equipmentGroup(uint8(level), true, mass+1); ok {
			t.Fatalf("level %d: a roll past the mass still lands", level)
		}
	}
	// The native catalog's ordinary rows reach 101: 95-101 fill.
	if filledLevels != 101-firstFilledLevel+1 {
		t.Fatalf("%d levels filled, want 95-101", filledLevels)
	}
	if row := filled.classes[1][highLevelSealLastLevel]; len(row) != 0 {
		t.Fatalf("level %d was filled: %+v", highLevelSealLastLevel+1, row)
	}
	if !reflect.DeepEqual(filled.classes[0], native.classes[0]) {
		t.Fatal("the ordinary table changed")
	}
	if !reflect.DeepEqual(native.classes[1], nativeRare) {
		t.Fatal("the installed catalog was mutated in place")
	}
}

/*
================
TestHighLevelSealsCarryOnTheNativeStarPattern

The inference's anchor: at level 94 the native rare row's own top class is
the Star of the level's degree, the class the rule repeats above it.
================
*/
func TestHighLevelSealsCarryOnTheNativeStarPattern(t *testing.T) {
	star, ok := equipment.sealGroupAt(nativeLastRareLevel, sealTierStar)
	if !ok {
		t.Fatal("level 94 has no Star class")
	}
	row := equipment.classes[1][nativeLastRareLevel-1]
	if row[len(row)-1].group != star {
		t.Fatalf("level 94's native rare row ends on class %d, the level's Star is %d", row[len(row)-1].group, star)
	}
}

/*
================
TestHighLevelSealsFromEnv
================
*/
func TestHighLevelSealsFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "maybe": false, "on": true, " ON ": true, "1": true, "true": true} {
		t.Setenv(EnvHighLevelSeals, value)
		if got := HighLevelSealsEnabled(); got != want {
			t.Fatalf("%s=%q: enabled %t, want %t", EnvHighLevelSeals, value, got, want)
		}
	}
}
