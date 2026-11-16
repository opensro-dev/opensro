/*
===========================================================================

wiring_eventsweep_test.go - the boot sweep of expired Halloween boxes (#3587)

===========================================================================
*/
package main

import (
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/progression"
)

const sweepTestShard = "test"

// sweepTestWindow closes at 1970-01-03T00:00:00Z.
const sweepTestWindow = "1970-01-01T00:00:00Z/1970-01-03T00:00:00Z"

/*
================
sweepTestRuntime

An item runtime that knows only the Halloween window and the clock,
which is all the sweep reads.
================
*/
func sweepTestRuntime(t *testing.T, now time.Time) *action.Runtime {
	t.Helper()
	t.Setenv(progression.EnvHalloweenEvent, "on")
	t.Setenv(progression.EnvHalloweenEventWindow, sweepTestWindow)
	return &action.Runtime{Halloween: progression.HalloweenEventFromEnv(), Now: func() time.Time { return now }}
}

/*
================
sweepBoxCount

The jewel boxes one reopened character still holds in its bag.
================
*/
func sweepBoxCount(authority *store.Store, name string) int {
	total := 0
	authority.ReadCharacters(sweepTestShard, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.Name != name {
				continue
			}
			for _, row := range c.MissionInventory {
				if row.Codename == progression.HalloweenBoxCodename {
					total += int(row.StackCount)
				}
			}
		}
	})
	return total
}

/*
================
TestBootSweepTakesTheBoxesFromAStoredCharacterWhoNeverEnters

Two offline characters hold boxes and one a potion. Before the window
closes the sweep writes nothing; after it, one boot sweep empties both
holders, and the change is in the database when the store reopens.
================
*/
func TestBootSweepTakesTheBoxesFromAStoredCharacterWhoNeverEnters(t *testing.T) {
	dir := t.TempDir()
	options := store.Options{DefaultSkills: func(string, []uint32) ([]uint32, error) { return []uint32{1}, nil }}
	authority, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	open := true
	t.Cleanup(func() {
		if open {
			authority.Close()
		}
	})
	box := domain.InventoryRow{Slot: 13, RefObjID: 3862, Codename: progression.HalloweenBoxCodename, StackCount: 5}
	potion := domain.InventoryRow{Slot: 14, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 3}
	pending := &domain.Character{Name: "Pending", ModelCodename: "CHAR_CH_MAN_ADVENTURER", MissionInventory: []domain.InventoryRow{box}}
	for _, c := range []*domain.Character{
		{Name: "Gone", ModelCodename: "CHAR_CH_MAN_ADVENTURER", MissionInventory: []domain.InventoryRow{box, potion}},
		pending,
		{Name: "Clean", ModelCodename: "CHAR_CH_MAN_ADVENTURER", MissionInventory: []domain.InventoryRow{potion}},
	} {
		if err := authority.CreateCharacter(sweepTestShard, "alpha", c); err != nil {
			t.Fatal(err)
		}
	}
	// A character pending deletion is still restorable, so the sweep covers it.
	authority.MutateCharacter(pending, "test-delete", func() { pending.DeletePending = true })

	inside := sweepTestRuntime(t, time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC))
	sweepExpiredHalloweenBoxes(authority, inside)
	if sweepBoxCount(authority, "Gone") != 5 || sweepBoxCount(authority, "Pending") != 5 {
		t.Fatal("the sweep took boxes while the window still runs")
	}

	closed := sweepTestRuntime(t, time.Date(1970, 1, 3, 0, 0, 0, 0, time.UTC))
	sweepExpiredHalloweenBoxes(authority, closed)
	sweepExpiredHalloweenBoxes(authority, closed) // a second boot finds nothing and is harmless
	authority.Close()
	open = false

	reopened, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if sweepBoxCount(reopened, "Gone") != 0 || sweepBoxCount(reopened, "Pending") != 0 {
		t.Fatal("a stored character still holds boxes after the sweep and a reopen")
	}
	rows := 0
	reopened.ReadCharacters(sweepTestShard, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.Name == "Gone" || c.Name == "Clean" {
				for _, row := range c.MissionInventory {
					if row.Codename == "ITEM_ETC_HP_POTION_01" && row.StackCount == 3 {
						rows++
					}
				}
			}
		}
	})
	if rows != 2 {
		t.Fatalf("potions survived on %d of 2 characters", rows)
	}
}
