/*
===========================================================================

skillrepair_associated_test.go - associatedSkillId on item-use native fields

Pins ItemUseNativeFields publishing the Desc1 skill id for any item that
names one (SRO Macro buff-item row 006930a0), while repair kits still get
useSkillId / useSkillDurationMs.

===========================================================================
*/
package enterworld

import "testing"

type associatedSkillSource map[string]SkillRow

func (s associatedSkillSource) SkillByCodename(name string) (SkillRow, bool) {
	row, ok := s[name]
	return row, ok
}

func (s associatedSkillSource) SkillByID(id uint32) (SkillRow, bool) {
	for _, row := range s {
		if row.ID == id {
			return row, true
		}
	}
	return SkillRow{}, false
}

/*
================
TestItemUseNativeFieldsAssociatedSkillId
================
*/
func TestItemUseNativeFieldsAssociatedSkillId(t *testing.T) {
	skills := associatedSkillSource{
		"SKILL_SCROLL_SPEED": {ID: 4242, Codename: "SKILL_SCROLL_SPEED"},
		"SKILL_REPAIR_KIT": {
			ID: 9001, Codename: "SKILL_REPAIR_KIT",
			StructureRepair: SkillStructureRepair{Pinned: true, DurationMs: 5000, PeriodMs: 1000, HPPercent: 10},
		},
	}

	scroll := &ItemRef{
		RefObjID:                100,
		Codename:                "ITEM_ETC_SCROLL_SPEED",
		TypeIDs:                 [4]int64{3, 3, 13, 1},
		AssociatedSkillCodename: "SKILL_SCROLL_SPEED",
	}
	fields := ItemUseNativeFields(scroll, skills)
	if id, ok := fields.Lookup("associatedSkillId"); !ok || id != 4242 {
		t.Fatalf("scroll associatedSkillId = %v %v, want 4242", id, ok)
	}
	if _, ok := fields.Lookup("useSkillId"); ok {
		t.Fatal("scroll must not publish useSkillId")
	}

	repair := &ItemRef{
		RefObjID:                101,
		Codename:                "ITEM_FORTRESS_REPAIR",
		TypeIDs:                 [4]int64{3, 3, 1, 10},
		AssociatedSkillCodename: "SKILL_REPAIR_KIT",
	}
	fields = ItemUseNativeFields(repair, skills)
	if id, ok := fields.Lookup("associatedSkillId"); !ok || id != 9001 {
		t.Fatalf("repair associatedSkillId = %v %v, want 9001", id, ok)
	}
	if id, ok := fields.Lookup("useSkillId"); !ok || id != 9001 {
		t.Fatalf("repair useSkillId = %v %v, want 9001", id, ok)
	}
	if ms, ok := fields.Lookup("useSkillDurationMs"); !ok || ms != 5000 {
		t.Fatalf("repair useSkillDurationMs = %v %v, want 5000", ms, ok)
	}

	plain := &ItemRef{RefObjID: 102, Codename: "ITEM_ETC_HP_POTION", TypeIDs: [4]int64{3, 3, 1, 1}}
	if fields = ItemUseNativeFields(plain, skills); fields != "" {
		t.Fatalf("plain potion fields = %q, want empty", fields)
	}
}
