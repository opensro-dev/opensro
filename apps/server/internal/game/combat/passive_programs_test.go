/*
===========================================================================

passive_programs_test.go - setv-less passive programs on the keeper

The authored Blockade (br), Protection (reat + real) and Chinese sword
passive (br under a shield reqi) rows contribute through learnedPassives
and learnedStatusResistance only while their reqi walk passes (59F0E0).

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
shippedPassive

One authored passive rank by codename, admitted by the production compiler.
================
*/
func shippedPassive(t *testing.T, codename string) enterworld.SkillRow {
	t.Helper()
	row, ok := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t)).SkillByCodename(codename)
	if !ok || !row.PassiveParameters.Pinned {
		t.Fatalf("%s not admitted: %+v", codename, row.PassiveParameters)
	}
	return row
}

/*
================
blockLaneWrites

The block-rate lane writes (0x88..0x8B) among a passive projection.
================
*/
func blockLaneWrites(writes []paramkeeper.Write) []paramkeeper.Write {
	var out []paramkeeper.Write
	for _, w := range writes {
		if w.Parameter >= 0x88 && w.Parameter <= 0x8b {
			out = append(out, w)
		}
	}
	return out
}

/*
================
TestBlockadeRaisesEveryBlockLaneOnlyWithAOneHandSword

SKILL_EU_WARRIOR_SHIELDP_BLOCK_A_08: br 15 9, reqi 6 7. With a one-hand
sword in slot 6 each of the four block-rate lanes gains flat 9 under the
skill's own source (594AC0 at 0x595DFD); a two-hand sword fails the walk
and contributes nothing, and so does a broken sword.
================
*/
func TestBlockadeRaisesEveryBlockLaneOnlyWithAOneHandSword(t *testing.T) {
	row := shippedPassive(t, "SKILL_EU_WARRIOR_SHIELDP_BLOCK_A_08")
	sword := &enterworld.ItemRef{Codename: "EU_SWORD", TypeIDs: [4]int64{3, 1, 6, 7}}
	twoHand := &enterworld.ItemRef{Codename: "EU_TSWORD", TypeIDs: [4]int64{3, 1, 6, 8}}
	items := itemRefs{sword.Codename: sword, twoHand.Codename: twoHand}
	skills := passiveSkills{row.ID: row}
	c := &domain.Character{Skills: []uint32{row.ID}}

	lanes := func(weapon *enterworld.ItemRef, durability int64) []paramkeeper.Write {
		t.Helper()
		c.MissionInventory = []domain.InventoryRow{{Slot: 6, Codename: weapon.Codename, TypeFlags: weapon.TypeFlags(), Durability: durability}}
		writes, _, err := learnedPassives(c, skills, items, uint8(weapon.TypeIDs[3]))
		if err != nil {
			t.Fatal(err)
		}
		return blockLaneWrites(writes)
	}
	got := lanes(sword, 10)
	if len(got) != 4 {
		t.Fatalf("one-hand sword: block writes %+v", got)
	}
	for i, w := range got {
		if w.Parameter != uint16(0x88+i) || w.Channel != paramkeeper.Flat || w.Value != 9 || w.Source <= 2048 {
			t.Fatalf("lane %d write %+v", i, w)
		}
	}
	if got := lanes(twoHand, 10); len(got) != 0 {
		t.Fatalf("two-hand sword carried Blockade: %+v", got)
	}
	if got := lanes(sword, 0); len(got) != 0 {
		t.Fatalf("broken sword carried Blockade: %+v", got)
	}
}

/*
================
TestChineseSwordPassiveFollowsTheShield

SKILL_CH_SWORD_PASSIVE_A_01 is the same br program (15 2) under reqi 4 1,
the secondary-slot walk for a Chinese shield (TID4 1) in slot 7: the same
compiler admits it, and it contributes only while the shield is worn.
================
*/
func TestChineseSwordPassiveFollowsTheShield(t *testing.T) {
	row := shippedPassive(t, "SKILL_CH_SWORD_PASSIVE_A_01")
	shield := &enterworld.ItemRef{Codename: "CH_SHIELD", TypeIDs: [4]int64{3, 1, 4, 1}}
	items := itemRefs{shield.Codename: shield}
	skills := passiveSkills{row.ID: row}
	c := &domain.Character{Skills: []uint32{row.ID}}
	if writes, _, _ := learnedPassives(c, skills, items, 0); len(blockLaneWrites(writes)) != 0 {
		t.Fatalf("contributed without a shield: %+v", writes)
	}
	c.MissionInventory = []domain.InventoryRow{{Slot: 7, Codename: shield.Codename, TypeFlags: shield.TypeFlags(), Durability: 10}}
	writes, _, err := learnedPassives(c, skills, items, 0)
	if got := blockLaneWrites(writes); err != nil || len(got) != 4 || got[0].Value != 2 {
		t.Fatalf("shield worn: writes %+v err %v", writes, err)
	}
}

/*
================
TestProtectionResistsOnlyWithADualAxe

SKILL_EU_WARRIOR_DUALP_ABNORMAL_A_07: reat 63 100, real 0x17FAFC0 50 9,
reqi 6 9. With a dual axe the six flat status reductions 0x91..0x96 gain
100 (595542..59568F) and every masked status's bucket holds flat 50 at
grade 9 (59DF20); without it, nothing.
================
*/
func TestProtectionResistsOnlyWithADualAxe(t *testing.T) {
	row := shippedPassive(t, "SKILL_EU_WARRIOR_DUALP_ABNORMAL_A_07")
	axe := &enterworld.ItemRef{Codename: "EU_AXE", TypeIDs: [4]int64{3, 1, 6, 9}}
	items := itemRefs{axe.Codename: axe}
	skills := passiveSkills{row.ID: row}
	c := &domain.Character{Skills: []uint32{row.ID}}

	check := func(equipped bool) {
		t.Helper()
		writes, _, err := learnedPassives(c, skills, items, 9)
		if err != nil {
			t.Fatal(err)
		}
		reduction := 0
		for _, w := range writes {
			if w.Parameter >= 0x91 && w.Parameter <= 0x96 && w.Channel == paramkeeper.Flat && w.Value == 100 {
				reduction++
			}
		}
		if (reduction == 6) != equipped || (len(writes) == 0) == equipped {
			t.Fatalf("equipped %v: writes %+v", equipped, writes)
		}
		resist := learnedStatusResistance(c, skills, items)
		filed := 0
		for _, source := range abnormal.Sources {
			if source.Resist < 0 {
				continue
			}
			want := abnormal.Resistance{}
			if equipped && 0x17FAFC0&source.Status.Bit() != 0 {
				want = abnormal.Resistance{Flat: 50, Grade: 9}
				filed++
			}
			if resist[source.Resist] != want {
				t.Fatalf("equipped %v: bucket %d = %+v, want %+v", equipped, source.Resist, resist[source.Resist], want)
			}
		}
		if equipped && filed == 0 {
			t.Fatal("no status of the real mask has a resistance bucket")
		}
	}
	check(false)
	c.MissionInventory = []domain.InventoryRow{{Slot: 6, Codename: axe.Codename, TypeFlags: axe.TypeFlags(), Durability: 10}}
	check(true)
	c.MissionInventory[0].Durability = 0
	check(false)
}

/*
================
TestChineseFlatRatePassivesRaiseTheirParameters

SKILL_CH_SPEAR_PASSIVE_A_09 (Cheolsam Force) is hpi 2826 0 with no reqi:
59F0E0 installs it through 594AC0, whose hpi case (595481) adds flat 2826
to maximum HP. The Cold Force mpi, Pacheon hr and Lightning er passives
are the same flat/percent blocks on Params 4, 11 and 9.
================
*/
func TestChineseFlatRatePassivesRaiseTheirParameters(t *testing.T) {
	for _, tc := range []struct {
		codename  string
		parameter uint16
		flat      float32
	}{
		{"SKILL_CH_SPEAR_PASSIVE_A_09", 3, 2826},
		{"SKILL_CH_WATER_PASSIVE_A_09", 4, 0},
		{"SKILL_CH_BOW_PASSIVE_A_09", 11, 0},
		{"SKILL_CH_LIGHTNING_PASSIVE_A_09", 9, 0},
	} {
		row := shippedPassive(t, tc.codename)
		c := &domain.Character{Skills: []uint32{row.ID}}
		writes, _, err := learnedPassives(c, passiveSkills{row.ID: row}, itemRefs{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		var flat, percent int
		for _, w := range writes {
			if w.Parameter != tc.parameter || w.Source <= 2048 {
				t.Fatalf("%s: stray write %+v", tc.codename, w)
			}
			switch w.Channel {
			case paramkeeper.Flat:
				flat++
				if tc.flat != 0 && w.Value != tc.flat || w.Value <= 0 {
					t.Fatalf("%s: flat %+v", tc.codename, w)
				}
			case paramkeeper.PercentSum:
				percent++
			}
		}
		if flat != 1 || percent != 1 {
			t.Fatalf("%s: writes %+v", tc.codename, writes)
		}
	}
}
