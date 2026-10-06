/*
===========================================================================

mercenary_test.go - licensed soldier families and their authored level rows

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestLicensedMercenaryFamilies
================
*/
func TestLicensedMercenaryFamilies(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := NewTextdataItems(dir)
	skills := NewTextdataSkills(dir)
	items.Len()
	families, rows := 0, 0
	seen := map[uint32]bool{}
	for _, item := range items.byCodename {
		if item.TypeIDs != [4]int64{3, 3, 12, 1} {
			continue
		}
		families++
		for level := int64(1); level < 140; level++ {
			ref, ok := MercenaryReference(items, item.AssociatedCharacterCodename, level)
			if !ok || int64(ref.Level) != level {
				t.Fatalf("%s level %d missing", item.Codename, level)
			}
			rows++
			if ref.Parameters.DefaultSkillIDs[0] == 0 {
				t.Fatalf("%s level %d has no authored attack", item.Codename, level)
			}
			for _, id := range ref.Parameters.DefaultSkillIDs {
				if id == 0 || seen[id] {
					continue
				}
				seen[id] = true
				skill, ok := skills.SkillByID(id)
				if !ok {
					t.Fatalf("missing soldier skill %d", id)
				}
				if skill.AIWeight > 0 && !skill.Attack.Present && !skill.CreatureStatusCast {
					t.Fatalf("uncompiled soldier skill %s", skill.Codename)
				}
				if skill.Consumption.HP != 0 || skill.Consumption.MP != 0 || skill.Consumption.HPPercent != 0 || skill.Consumption.MPPercent != 0 || skill.Ammunition.Count != 0 {
					t.Fatalf("soldier skill needs resource execution: %s", skill.Codename)
				}
				if skill.ID != id {
					t.Fatalf("soldier skill identity %d resolved as %d", id, skill.ID)
				}
			}
		}
		if _, ok := MercenaryReference(items, item.AssociatedCharacterCodename, 140); ok {
			t.Fatal("native final-row boundary admitted")
		}
	}
	if families != 15 || rows != 2085 {
		t.Fatalf("census %d families, %d rows", families, rows)
	}
}
