/*
===========================================================================

freegrant_doors_test.go - the kit and the refill are two doors (#3307)

Each grant at entry commits under its own door, so the item ledger and
the free-grant days tell them apart: a character missing the kit and
short of potions passes both, one holding the kit only the refill.

===========================================================================
*/
package enterworld

import (
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
entryGrantDoors

The starter doors one entry passed, in order.
================
*/
func entryGrantDoors(t *testing.T, character *Character) []string {
	t.Helper()
	deps := testDeps(character)
	deps.StarterKit = []WireItem{{RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01", StackCount: 1}}
	deps.StarterRefills = []StarterRefill{{
		Grades: []WireItem{{RefObjID: 8, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 1}},
		Stack:  []int64{50},
	}}
	var doors []string
	deps.MutateCharacter = func(_ *Character, label string, fn func()) {
		fn()
		if strings.HasPrefix(label, "starter-") {
			doors = append(doors, label)
		}
	}
	if result := Build(deps, BootstrapRequest{CharacterName: character.Name}); result.NativeResult != 1 {
		t.Fatalf("entry failed: %+v", result)
	}
	return doors
}

/*
================
TestStarterKitAndRefillAreSeparateDoors
================
*/
func TestStarterKitAndRefillAreSeparateDoors(t *testing.T) {
	fresh := chinaSpearman()
	fresh.ID = 7
	if got, want := entryGrantDoors(t, fresh), []string{domain.GrantStarterKit, domain.GrantStarterRefill}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a fresh character passed %v, want %v", got, want)
	}
	// The second entry holds the kit; only the refill is short again
	// once the potions are spent.
	for i := range fresh.MissionInventory {
		if fresh.MissionInventory[i].RefObjID == 8 {
			fresh.MissionInventory[i].StackCount = 1
		}
	}
	if got, want := entryGrantDoors(t, fresh), []string{domain.GrantStarterRefill}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a kitted character passed %v, want %v", got, want)
	}
}
