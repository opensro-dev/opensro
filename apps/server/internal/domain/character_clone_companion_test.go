/*
===========================================================================

character_clone_companion_test.go - the pet-owner view (#2030)

The per-tick pet and brood passes copy only what their readers read:
identity, world, companions, life and status, the PvP and relation
facts, and of the inventory only the companion rows and the relation
slot. Everything else is zero, and what is kept is detached.

===========================================================================
*/
package domain

import "testing"

/*
================
TestSnapshotCompanionKeepsThePetOwnerView

Companion resolution over the view visits the same companions in the
same order as over the full copy; the relation slot and the companion
rows survive, other worn rows, plain bag rows and the avatar inventory
do not; the PvP and relation facts survive, unread children (skills,
quests, gold) are zero; and everything kept is detached, not shared.
================
*/
func TestSnapshotCompanionKeepsThePetOwnerView(t *testing.T) {
	hp, gold, guild := int64(50), int64(9000), int64(77)
	character := &Character{
		ID: 9, AccountID: "owner-account", Name: "companionOwner", Job: CharacterJob{Type: 1},
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "ITEM_WEAPON", MagicOptions: []uint64{7}},
			{Slot: JobSuitSlot, Codename: "ITEM_CAPE", MagicOptions: []uint64{5}},
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
		Gold:            &gold,
		GuildID:         &guild,
		Aggressions:     map[uint32]uint32{4: 30},
		Skills:          []uint32{1},
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

	slots := map[int64]bool{}
	for _, row := range narrow.MissionInventory {
		slots[row.Slot] = true
	}
	if len(slots) != 3 || !slots[JobSuitSlot] || !slots[14] || !slots[15] {
		t.Fatalf("view kept slots %v, want the relation slot %d and companion rows 14, 15", slots, JobSuitSlot)
	}
	if narrow.AvatarInventory != nil {
		t.Fatalf("avatar inventory = %+v, want nil", narrow.AvatarInventory)
	}
	if narrow.ID != 9 || narrow.AccountID != "owner-account" || narrow.Name != "companionOwner" ||
		narrow.Job.Type != 1 || *narrow.CurrentHP != 50 || *narrow.GuildID != 77 || narrow.Aggressions[4] != 30 {
		t.Fatalf("view lost a kept fact: %+v", narrow)
	}
	if narrow.Gold != nil || len(narrow.Skills) != 0 {
		t.Fatal("view carried an unread child (gold, skills)")
	}

	character.MissionInventory[4].Summon.CurrentHP = 1
	character.ActiveCOS.CurrentHP = 2
	character.Aggressions[4] = 0
	*character.GuildID = 1
	if narrow.MissionInventory[2].Summon.CurrentHP != 80 || narrow.ActiveCOS.CurrentHP != 70 ||
		narrow.Aggressions[4] != 30 || *narrow.GuildID != 77 {
		t.Fatal("view shares state with the source")
	}
	character.MissionInventory[1].MagicOptions[0] = 99
	if narrow.MissionInventory[0].MagicOptions[0] != 5 {
		t.Fatal("view shares the relation row's magic options with the source")
	}
}
