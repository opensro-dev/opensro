package domain

import "testing"

/*
================
TestSnapshotCompanionKeepsCompanionsAndEquipmentOnly

The pet pass's copy (#2030): equipment rows and every row carrying a
companion survive, plain bag rows and the avatar inventory do not.
Companion resolution over the narrow copy visits the same companions
in the same order as over the full copy, and the kept children are
detached, not shared.
================
*/
func TestSnapshotCompanionKeepsCompanionsAndEquipmentOnly(t *testing.T) {
	hp := int64(50)
	character := &Character{
		Name: "companionOwner",
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "ITEM_WEAPON", MagicOptions: []uint64{7}},
			{Slot: 13, Codename: "ITEM_ETC_PLAIN", MagicOptions: []uint64{8}},
			{Slot: 14, Codename: "ITEM_COS_P_WOLF", Summon: &CharacterCOS{GID: 7001, Codename: "COS_P_WOLF", CurrentHP: 90, Summoned: true}},
			{Slot: 15, Codename: "ITEM_COS_P_CROW", Summon: &CharacterCOS{GID: 7002, Codename: "COS_P_CROW", CurrentHP: 80}},
		},
		AvatarInventory: &AvatarInventory{Rows: []InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR"}}},
		ActiveCOS:       &CharacterCOS{GID: 7003, Codename: "COS_ACTIVE", CurrentHP: 70, Summoned: true},
		CapturedCOS:     &CharacterCOS{GID: 7004, Codename: "COS_CAPTURED", CurrentHP: 60},
		Mercenaries:     []*CharacterCOS{{GID: 7005, Codename: "COS_MERC", CurrentHP: 55}},
		Brood:           []*CharacterCOS{{GID: 7006, Codename: "COS_BROOD", CurrentHP: 44}},
		CurrentHP:       &hp,
	}

	full := character.Snapshot()
	narrow := character.SnapshotCompanion()

	var fullGids, narrowGids []uint32
	full.VisitCompanions(func(pet *CharacterCOS) bool { fullGids = append(fullGids, pet.GID); return true })
	narrow.VisitCompanions(func(pet *CharacterCOS) bool { narrowGids = append(narrowGids, pet.GID); return true })
	if len(fullGids) != len(narrowGids) {
		t.Fatalf("companion walk: full %v, narrow %v", fullGids, narrowGids)
	}
	for i := range fullGids {
		if fullGids[i] != narrowGids[i] {
			t.Fatalf("companion walk order diverged: full %v, narrow %v", fullGids, narrowGids)
		}
	}
	if got := narrow.CompanionByGID(7001); got == nil || got.CurrentHP != 90 {
		t.Fatalf("narrow CompanionByGID(7001) = %+v", got)
	}
	if got := narrow.CompanionByGID(7003); got == nil || got.CurrentHP != 70 {
		t.Fatalf("narrow CompanionByGID(7003) = %+v", got)
	}

	slots := map[int64]bool{}
	for _, row := range narrow.MissionInventory {
		slots[row.Slot] = true
	}
	if !slots[6] || !slots[14] || !slots[15] {
		t.Fatalf("narrow copy kept slots %v, want equipment 6 and companion rows 14, 15", slots)
	}
	if slots[13] {
		t.Fatal("plain bag row survived in the companion copy")
	}
	if narrow.AvatarInventory != nil {
		t.Fatalf("avatar inventory = %+v, want nil", narrow.AvatarInventory)
	}

	character.MissionInventory[3].Summon.CurrentHP = 1
	character.ActiveCOS.CurrentHP = 2
	if narrow.MissionInventory[2].Summon.CurrentHP != 80 || narrow.ActiveCOS.CurrentHP != 70 {
		t.Fatal("companion copy shares COS records with the source")
	}
	character.MissionInventory[0].MagicOptions[0] = 99
	if narrow.MissionInventory[0].MagicOptions[0] != 7 {
		t.Fatal("companion copy shares equipment magic options with the source")
	}
}
