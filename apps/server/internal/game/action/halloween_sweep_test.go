/*
===========================================================================

halloween_sweep_test.go - the boot sweep's picker for expired Halloween boxes (#3587)

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
sweepHolders

One character per container the entry cleanup sweeps, each holding a
box, plus a bystander with none.
================
*/
func sweepHolders() (holders []*enterworld.Character, bystander *enterworld.Character) {
	named := func(name string) *enterworld.Character {
		c := testCharacter()
		c.Name = name
		c.MissionInventory = nil
		return c
	}
	bag := named("InBag")
	bag.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 2)}
	avatar := named("InAvatar")
	avatar.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{halloweenBoxRow(1, 1)}}
	active := named("InActivePet")
	active.ActiveCOS = &enterworld.CharacterCOS{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{halloweenBoxRow(1, 1)}}}
	mercenary := named("InMercenary")
	mercenary.Mercenaries = []*enterworld.CharacterCOS{{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{halloweenBoxRow(1, 1)}}}}
	dismissed := named("InDismissedPetBag")
	dismissed.MissionInventory = []enterworld.InventoryRow{{
		Slot: 30, Codename: "ITEM_COS_P_TEST",
		Summon: &enterworld.CharacterCOS{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{halloweenBoxRow(1, 1)}}},
	}}
	bystander = named("Bystander")
	bystander.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 3}}
	return []*enterworld.Character{bag, avatar, active, mercenary, dismissed}, bystander
}

/*
================
TestHalloweenBoxHoldersPicksEveryHolderOnlyAfterTheWindow

Nobody is picked while the event is off or the window runs; after the
close every container the entry cleanup sweeps counts, and the holders
the picker names are exactly the ones the removal then empties.
================
*/
func TestHalloweenBoxHoldersPicksEveryHolderOnlyAfterTheWindow(t *testing.T) {
	holders, bystander := sweepHolders()
	roster := append(append([]*enterworld.Character{nil}, holders...), bystander)

	off, _ := newTestRuntime(testCharacter(), halloweenTestItems())
	if got := off.HalloweenBoxHolders(roster); got != nil {
		t.Fatalf("holders picked with the event off: %d", len(got))
	}

	rt, clock := halloweenRuntime(t, testCharacter(), halloweenTestWindow)
	clock.now = time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := rt.HalloweenBoxHolders(roster); got != nil {
		t.Fatalf("holders picked inside the window: %d", len(got))
	}

	clock.now = time.Date(1970, 1, 3, 0, 0, 0, 0, time.UTC)
	picked := rt.HalloweenBoxHolders(roster)
	if len(picked) != len(holders) {
		t.Fatalf("picked %d of %d holders", len(picked), len(holders))
	}
	for i, c := range picked {
		if c != holders[i] {
			t.Fatalf("holder %d is %s, want %s", i, c.Name, holders[i].Name)
		}
		if !rt.RemoveExpiredHalloweenBoxes(c) {
			t.Fatalf("%s: the picker named a holder the removal found empty", c.Name)
		}
	}
	// Picker and removal agree: nobody is left to pick, and the bystander's
	// potions were never touched.
	if again := rt.HalloweenBoxHolders(roster); again != nil {
		t.Fatalf("%d holder(s) left after the removal", len(again))
	}
	if bagCount(bystander, "ITEM_ETC_HP_POTION_01") != 3 {
		t.Fatal("the bystander lost a row", bystander.MissionInventory)
	}
	if bagCount(holders[0], progression.HalloweenBoxCodename) != 0 {
		t.Fatal("the bag still holds a box")
	}
}
