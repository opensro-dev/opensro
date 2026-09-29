package combat

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

/*
==================
TestFaithWorksOnlyInARobeWithAStaff

SKILL_EU_CLERIC_PRAISEP_HEAL_A_01: setv HLRU 2, reat 32 20, real
0x1618600 50 1, reqi 9 0 + reqi 6 15 under reqn. Without the robe and
staff it adds nothing (59F0E0); with them HLRU is 2, parameter 0x96
(reat bit 5) gains 20 and every curse-line status bucket holds flat 50
at grade 1 - and no weakening-line bucket does.
==================
*/
func TestFaithWorksOnlyInARobeWithAStaff(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	row, ok := enterworld.NewTextdataSkills(dir).SkillByID(10094)
	if !ok || !row.PassiveParameters.Pinned || !row.Reqi.Present || !row.Reqi.All {
		t.Fatalf("Faith not admitted: %+v", row.PassiveParameters)
	}
	skills := passiveSkills{row.ID: row}
	robe := &enterworld.ItemRef{Codename: "ROBE_HEAD", TypeIDs: [4]int64{3, 1, 9, 1}}
	staff := &enterworld.ItemRef{Codename: "STAFF", TypeIDs: [4]int64{3, 1, 6, 15}}
	items := itemRefs{robe.Codename: robe, staff.Codename: staff}
	c := &domain.Character{Skills: []uint32{row.ID}}

	check := func(equipped bool) {
		t.Helper()
		writes, power, err := learnedPassives(c, skills, items, 15)
		if err != nil {
			t.Fatal(err)
		}
		resist := learnedStatusResistance(c, skills, items)
		reat := 0
		for _, w := range writes {
			if w.Parameter == 0x96 && w.Value == 20 {
				reat++
			}
		}
		if got := power[enterworld.ParameterHealRecoveryUp]; (got == 2) != equipped || (reat == 1) != equipped {
			t.Fatalf("equipped %v: HLRU %d, reat writes %d", equipped, got, reat)
		}
		for _, source := range abnormal.Sources {
			if source.Resist < 0 {
				continue
			}
			curse := 0x1618600&source.Status.Bit() != 0
			want := abnormal.Resistance{}
			if equipped && curse {
				want = abnormal.Resistance{Flat: 50, Grade: 1}
			}
			if resist[source.Resist] != want {
				t.Fatalf("equipped %v: bucket %d = %+v, want %+v", equipped, source.Resist, resist[source.Resist], want)
			}
		}
	}
	check(false)
	c.MissionInventory = []domain.InventoryRow{
		{Slot: 0, Codename: robe.Codename, TypeFlags: robe.TypeFlags(), Durability: 10},
		{Slot: 6, Codename: staff.Codename, TypeFlags: staff.TypeFlags(), Durability: 10},
	}
	check(true)
	c.MissionInventory = c.MissionInventory[1:] // the robe comes off
	check(false)
}
