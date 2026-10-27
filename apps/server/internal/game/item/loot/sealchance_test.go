/*
===========================================================================

sealchance_test.go - the pity increment composes the planner's odds

===========================================================================
*/
package loot

import (
	"math"
	"testing"
)

// nativeSettings is a native-rate composition: one pass, one equipment
// roll per pass, no cap, no band, no Sun widening, no thinning.
func nativeSettings() SealChanceSettings {
	settings := SealChanceSettings{
		RareChance:    0.001,
		PassRate:      1,
		EquipmentRate: 1,
		AssignedRate:  1,
	}
	for degree := range settings.SealKeep {
		for tier := range settings.SealKeep[degree] {
			settings.SealKeep[degree][tier] = 1
		}
	}
	for category := range settings.CategoryRates {
		settings.CategoryRates[category] = 1
	}
	return settings
}

/*
================
TestSealKillChanceNativeShape

At native rates a level-85 monster owes degree-9 Star more than Moon
and Moon more than Sun, a level-1 monster owes no degree-9 debt at all,
and the nine degrees together stay under the rare chance itself.
================
*/
func TestSealKillChanceNativeShape(t *testing.T) {
	settings := nativeSettings()
	at85 := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, settings)
	star, moon, sun := at85[8][0], at85[8][1], at85[8][2]
	if !(star > moon && moon > sun && sun > 0) {
		t.Fatalf("degree-9 chances at 85: star %g moon %g sun %g, want star > moon > sun > 0", star, moon, sun)
	}
	total := 0.0
	for degree := range at85 {
		for tier := range at85[degree] {
			total += at85[degree][tier]
		}
	}
	if total > settings.RareChance || total <= 0 {
		t.Fatalf("all-seal chance %g outside (0, rare chance %g]", total, settings.RareChance)
	}
	at1 := SealKillChance(1, 0, "MOB_CH_MANGNYANG", 0, settings)
	if at1[8] != [sealTiers]float64{} {
		t.Fatalf("degree-9 chances at level 1: %v, want none", at1[8])
	}
	// The cache answers the same for the same inputs.
	if again := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, settings); again != at85 {
		t.Fatal("cached composition differs from the first")
	}
}

/*
================
TestSealKillChanceScalesWithPassesAndKeep

Ten equipment passes owe more than one (less than ten times more), a
keep probability of a third thins its class to a third of the unthinned
kill chance, and a cap far below the expected items thins every class.
================
*/
func TestSealKillChanceScalesWithPassesAndKeep(t *testing.T) {
	base := nativeSettings()
	one := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, base)[8][0]

	more := base
	more.EquipmentRate = 10
	ten := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, more)[8][0]
	if !(ten > one && ten < 10*one) {
		t.Fatalf("ten passes %g, want between one pass %g and ten times it", ten, one)
	}

	thinned := base
	thinned.SealKeep[8][0] = 1.0 / 3
	third := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, thinned)[8][0]
	if math.Abs(third-one/3) > one*0.01 {
		t.Fatalf("keep 1/3 gives %g, want about %g", third, one/3)
	}

	wide := base
	wide.PassRate = 50
	wide.EquipmentRate = 50
	for category := range wide.CategoryRates {
		wide.CategoryRates[category] = 50
	}
	uncapped := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, wide)[8][0]
	capped := wide
	capped.DropCap = 1
	if got := SealKillChance(85, 0, "MOB_CH_MANGNYANG", 0, capped)[8][0]; !(got > 0 && got < uncapped) {
		t.Fatalf("hard cap gives %g, want below the same-rate uncapped %g", got, uncapped)
	}
}

/*
================
TestPickEquipmentOfClassStaysInClass

The forced pick always names an item of the bucket it was asked for,
at either end of the weight range.
================
*/
func TestPickEquipmentOfClassStaysInClass(t *testing.T) {
	for _, group := range []int{24, 25, 26} {
		bucket := equipment.buckets[equipmentKey{0, group, true}]
		if bucket == nil {
			continue
		}
		members := map[string]bool{}
		for _, ref := range bucket.refs {
			members[ref.Codename] = true
		}
		for _, roll := range []uint32{0, 1, 32767} {
			got, ok := PickEquipmentOfClass(0, group, func() (uint32, error) { return roll, nil })
			if !ok || !members[got] {
				t.Fatalf("group %d roll %d: %q/%v, want a bucket member", group, roll, got, ok)
			}
		}
	}
	if _, ok := PickEquipmentOfClass(0, 26, nil); ok {
		t.Fatal("nil roll picked an item")
	}
	if _, ok := PickEquipmentOfClass(9, 26, func() (uint32, error) { return 0, nil }); ok {
		t.Fatal("country 9 picked an item")
	}
}
