/*
===========================================================================

uniquelevel_test.go - the level a unique's equipment tables roll at

The owner's rule is the highest level the installed catalog fills: the
native cap, or the expansion cap once the cap-110 catalog carries gear
there.

===========================================================================
*/
package loot

import "testing"

/*
================
TestUniqueDropLevelFollowsTheInstalledCatalog

Native content ends at 90; a cap-110 catalog with a filled level-110 class
row lifts the drop level to 110.
================
*/
func TestUniqueDropLevelFollowsTheInstalledCatalog(t *testing.T) {
	before, beforeEquipment := cap110Installed, equipment
	t.Cleanup(func() {
		cap110Installed, equipment = before, beforeEquipment
	})

	cap110Installed = false
	if got := UniqueDropLevel(); got != nativeMonsterLevelCap {
		t.Fatalf("native catalog drop level %d, want %d", got, nativeMonsterLevelCap)
	}

	cap110Installed = true
	equipment = topLevelEquipmentCatalog(beforeEquipment, expansionMonsterLevelCap, true)
	if got := UniqueDropLevel(); got != expansionMonsterLevelCap {
		t.Fatalf("cap-110 catalog drop level %d, want %d", got, expansionMonsterLevelCap)
	}
}

/*
================
TestUniqueDropLevelKeepsNativeWhenTheTopRowIsEmpty

A cap-110 catalog whose level-110 class row holds nothing must not leave a
unique kill with no equipment: the native level stays in force.
================
*/
func TestUniqueDropLevelKeepsNativeWhenTheTopRowIsEmpty(t *testing.T) {
	before, beforeEquipment := cap110Installed, equipment
	t.Cleanup(func() {
		cap110Installed, equipment = before, beforeEquipment
	})

	cap110Installed = true
	equipment = topLevelEquipmentCatalog(beforeEquipment, expansionMonsterLevelCap, false)
	if got := UniqueDropLevel(); got != nativeMonsterLevelCap {
		t.Fatalf("empty top row drop level %d, want the native %d", got, nativeMonsterLevelCap)
	}
}

/*
================
TestUniqueInTopLootBand

The band is ten levels under the drop level and follows it: levels 80 to
90 count on the native catalog, only 100 to 110 once the cap-110 catalog
lifts the top.
================
*/
func TestUniqueInTopLootBand(t *testing.T) {
	before, beforeEquipment := cap110Installed, equipment
	t.Cleanup(func() {
		cap110Installed, equipment = before, beforeEquipment
	})

	cap110Installed = false
	for level, want := range map[uint8]bool{20: false, 79: false, 80: true, 90: true, 94: true} {
		if got := UniqueInTopLootBand(level); got != want {
			t.Fatalf("native catalog: level %d in band = %v, want %v", level, got, want)
		}
	}

	cap110Installed = true
	equipment = topLevelEquipmentCatalog(beforeEquipment, expansionMonsterLevelCap, true)
	for level, want := range map[uint8]bool{90: false, 99: false, 100: true, 110: true} {
		if got := UniqueInTopLootBand(level); got != want {
			t.Fatalf("cap-110 catalog: level %d in band = %v, want %v", level, got, want)
		}
	}
}

/*
================
topLevelEquipmentCatalog

A copy of the catalog whose normal class row at the given level is either
filled with the highest class the catalog already uses (a cap-110 file that
carries gear) or emptied (a cap-110 file that does not).
================
*/
func topLevelEquipmentCatalog(source equipmentCatalog, level uint8, filled bool) equipmentCatalog {
	out := equipmentCatalog{buckets: source.buckets, classes: [2][][]classThreshold{
		append([][]classThreshold(nil), source.classes[0]...),
		append([][]classThreshold(nil), source.classes[1]...),
	}, widths: source.widths}
	if !filled {
		out.classes[0][level-1] = nil
		return out
	}
	out.classes[0][level-1] = []classThreshold{{group: source.widths[0] - 1, threshold: 1_000_000}}
	return out
}
