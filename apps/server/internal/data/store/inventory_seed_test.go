/*
===========================================================================

inventory_seed_test.go - the creation inventory seed (Options.DefaultInventory)

Creation installs the starter inventory with the record (retail
_AddNewChar), so the character list shows a new character dressed before
its first world entry (BUG-045). The seed is optional wiring.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// testInventorySeeder grants one worn bow, standing in for
// enterworld.StarterInventorySeeder without the itemdata dependency.
func testInventorySeeder(c *enterworld.Character) {
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 3655, Codename: "ITEM_CH_BOW_01_A_DEF", StackCount: 1}}
}

/*
================
TestCreateCharacterSeedsTheStarterInventory
================
*/
func TestCreateCharacterSeedsTheStarterInventory(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	dir := t.TempDir()
	s, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder, DefaultInventory: testInventorySeeder})
	if err != nil {
		t.Fatal(err)
	}
	archer := &enterworld.Character{Name: "archer", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	if err := s.CreateCharacter(testDivision, "test-account", archer); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var stored []enterworld.InventoryRow
	reopened.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		for _, c := range characters {
			if c.Name == "archer" {
				stored = c.MissionInventory
			}
		}
	})
	if len(stored) != 1 || stored[0].RefObjID != 3655 {
		t.Fatalf("persisted creation inventory = %+v, want the seeded bow", stored)
	}
}

/*
================
TestCreateCharacterWithoutInventorySeed

Unwired, creation leaves the inventory unset for the first bootstrap.
================
*/
func TestCreateCharacterWithoutInventorySeed(t *testing.T) {
	t.Parallel()
	s, err := Open(t.TempDir(), Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c := &enterworld.Character{Name: "unseeded", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	if c.MissionInventory != nil {
		t.Fatalf("inventory = %+v, want none without the seed", c.MissionInventory)
	}
}
