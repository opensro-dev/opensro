/*
===========================================================================

item_identity_rebind_test.go - tests for item_identity_rebind.go

A gears stone stored under the fox pet's old id (#3994) takes its
codename's current id in the bag, the warehouse and a market listing, and
keeps it across a reopen. A row the catalogue does not know, and one
already current, are left alone.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

const (
	rebindStone   = "ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_STR_01"
	rebindOldID   = 50853
	rebindNewID   = 61001
	rebindUnknown = "ITEM_GONE"
)

/*
================
rebindCatalogue

The test's catalogue: the stone and the sword have current ids, nothing
else is known.
================
*/
func rebindCatalogue(codename string) (uint32, bool) {
	switch codename {
	case rebindStone:
		return rebindNewID, true
	case marketSword:
		return 7, true
	}
	return 0, false
}

/*
================
TestRebindItemIdentitiesFollowsTheCodename
================
*/
func TestRebindItemIdentitiesFollowsTheCodename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, _, seller, _ := marketFixtureIn(t, dir)
	stone := domain.InventoryRow{RefObjID: rebindOldID, Codename: rebindStone, StackCount: 3}
	s.Mutate("rebind-fixture", func() {
		bag := stone
		bag.Slot = 20
		gone := domain.InventoryRow{Slot: 21, RefObjID: 900, Codename: rebindUnknown, StackCount: 1}
		seller.MissionInventory = append(seller.MissionInventory, bag, gone)
	})
	if _, err := s.TransactStorage(seller, func(_ *domain.Character, storage *domain.AccountStorage) error {
		for slot := int64(1); slot <= 2; slot++ {
			row := stone
			row.Slot = slot
			storage.Rows = append(storage.Rows, row)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The stone in warehouse slot 1 goes on the market.
	listing, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Slot: 1,
		Currency: domain.MarketSilk, Price: 10, Admit: admitAll, Rules: testMarketRules})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := s.RebindItemIdentities(testDivision, rebindCatalogue)
	if err != nil {
		t.Fatal(err)
	}
	// The bag stone, the warehouse stone, the listed stone; the sword in
	// warehouse slot 0 is moved from its fixture id to 7 as well.
	if moved != 4 {
		t.Fatalf("moved %d rows, want 4", moved)
	}
	again, err := s.RebindItemIdentities(testDivision, rebindCatalogue)
	if err != nil || again != 0 {
		t.Fatalf("a second boot moved %d rows (%v)", again, err)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	// The GameWorld's boot order: the rebind runs on the store as it opens.
	if again, err := reopened.RebindItemIdentities(testDivision, rebindCatalogue); err != nil || again != 0 {
		t.Fatalf("the rebind on a freshly opened store moved %d rows (%v)", again, err)
	}
	var loaded *domain.Character
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name == seller.Name {
			loaded = c
		}
	}
	if loaded == nil {
		t.Fatal("seller not reloaded")
	}
	for _, row := range loaded.MissionInventory {
		switch row.Codename {
		case rebindStone:
			if row.RefObjID != rebindNewID {
				t.Fatalf("bag stone id = %d", row.RefObjID)
			}
		case rebindUnknown:
			if row.RefObjID != 900 {
				t.Fatalf("an unknown codename was rebound to %d", row.RefObjID)
			}
		}
	}
	storage, err := reopened.AccountStorage(loaded)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range storage.Rows {
		if row.Codename == rebindStone && row.RefObjID != rebindNewID {
			t.Fatalf("warehouse stone id = %d", row.RefObjID)
		}
	}
	stored, err := readListing(reopened.syncDBLocked(), listing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Item.RefObjID != rebindNewID || stored.Item.Codename != rebindStone {
		t.Fatalf("listed stone = %+v", stored.Item)
	}
}
