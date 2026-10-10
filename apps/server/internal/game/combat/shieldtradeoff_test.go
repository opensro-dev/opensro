/*
===========================================================================

shieldtradeoff_test.go - shield-only defense penalties in the combat graph

Keep armor, magic defense and unrelated buffs intact. The current shield's
variance/plus and current STR must determine the cut on every snapshot.

===========================================================================
*/
package combat

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"strconv"
	"testing"
)

/*
================
TestShieldTradeoffUsesTheCurrentShieldAndStrength
================
*/
func TestShieldTradeoffUsesTheCurrentShieldAndStrength(t *testing.T) {
	shield := &enterworld.ItemRef{RefObjID: 1, Codename: "SHIELD", TypeIDs: [4]int64{3, 1, 4, 1}, Combat: &enterworld.ItemCombatRef{
		PhysicalDefense:              enterworld.ItemStatRange{Min: 100, Max: 131, PerPlus: 10},
		MagicalDefense:               enterworld.ItemStatRange{Min: 200, Max: 200},
		PhysicalDefenseReinforcement: enterworld.ItemStatRange{Min: .5, Max: .5}}}
	armor := &enterworld.ItemRef{RefObjID: 2, Codename: "ARMOR", TypeIDs: [4]int64{3, 1, 1, 1}, Combat: &enterworld.ItemCombatRef{
		PhysicalDefense: enterworld.ItemStatRange{Min: 80, Max: 80}}}
	refs := itemRefs{shield.Codename: shield, armor.Codename: armor}
	for _, tc := range []struct {
		name           string
		strength, plus int64
		variance       uint64
		shieldBase     float64
	}{
		{"base", 100, 0, 0, 100}, {"plus and variance", 100, 2, 31 << 20, 151}, {"higher STR", 200, 0, 0, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &domain.Character{Level: pointer(1), Strength: pointer(tc.strength), Intellect: pointer(100), MissionInventory: []domain.InventoryRow{
				{Slot: 7, RefObjID: 1, Codename: shield.Codename, TypeFlags: shield.TypeFlags(), Durability: 1, VarianceBits: "0", Plus: tc.plus},
				{Slot: 0, RefObjID: 2, Codename: armor.Codename, TypeFlags: armor.TypeFlags(), Durability: 1, VarianceBits: "0"}}}
			c.MissionInventory[0].VarianceBits = strconv.FormatUint(tc.variance, 10)
			baseWrites := []paramkeeper.Write{{Parameter: 5, Source: 4096, Value: 50}, {Parameter: 5, Channel: paramkeeper.PercentSum, Source: 4097, Value: 20},
				{Parameter: 0x38, Source: 4097, Value: 50}}
			base, _, err := PlayerStatsWithModifiers(c.Snapshot(), Catalogs{Items: refs}, baseWrites, nil)
			if err != nil {
				t.Fatal(err)
			}
			writes := ShieldTradeoffWrites(enterworld.SkillShieldTradeoff{Present: true, DefensePercent: 50, PhysicalAttack: 27})
			for i := range writes {
				writes[i].Source = 4098
			}
			buffed, _, err := PlayerStatsWithModifiers(c.Snapshot(), Catalogs{Items: refs}, append(baseWrites, writes...), nil)
			if err != nil {
				t.Fatal(err)
			}
			defender := Stats{BlockRate: 60}
			if BlockChance(base, defender, 5, false) != 40 || BlockChance(buffed, defender, 5, false) != 40 {
				t.Fatal("shield tradeoff changed block ignore")
			}
			cut := (tc.shieldBase + float64(tc.strength)*.5) * .5
			wantDefense := float32((float64(float32(base.PhysicalDefense/1.2)) - cut) * 1.2)
			if float32(buffed.PhysicalDefense) != wantDefense ||
				float32(buffed.PhysicalAttackMin) != float32(base.PhysicalAttackMin+27) ||
				float32(buffed.PhysicalAttackMax) != float32(base.PhysicalAttackMax+27) ||
				buffed.MagicalDefense != base.MagicalDefense || buffed.MagicalAttackMin != base.MagicalAttackMin ||
				buffed.MagicalAttackMax != base.MagicalAttackMax {
				t.Fatalf("buffed defense/attack %g %g..%g, want %g %g..%g", buffed.PhysicalDefense, buffed.PhysicalAttackMin, buffed.PhysicalAttackMax, wantDefense, base.PhysicalAttackMin+27, base.PhysicalAttackMax+27)
			}
			c.MissionInventory[0].Durability = 0
			broken, _, err := PlayerStatsWithModifiers(c.Snapshot(), Catalogs{Items: refs}, append(baseWrites, writes...), nil)
			if err != nil {
				t.Fatal(err)
			}
			without, _, err := PlayerStatsWithModifiers(c.Snapshot(), Catalogs{Items: refs}, baseWrites, nil)
			if err != nil || broken.PhysicalDefense != without.PhysicalDefense {
				t.Fatal("broken shield still loses defense", err)
			}
		})
	}
}
