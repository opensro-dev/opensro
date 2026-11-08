/*
===========================================================================

starterrefill_test.go - the beta's HP/MP potion refill

===========================================================================
*/
package enterworld

import (
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	testhook "github.com/sirupsen/logrus/hooks/test"
)

/*
================
testRefills

Two families of five grades, each refilling to 50.
================
*/
func testRefills() []StarterRefill {
	family := func(base uint32) StarterRefill {
		refill := StarterRefill{}
		for i := uint32(0); i < 5; i++ {
			refill.Grades = append(refill.Grades, WireItem{RefObjID: base + i, Codename: "POTION", TypeFlags: 1})
			refill.Stack = append(refill.Stack, 50)
		}
		return refill
	}
	return []StarterRefill{family(4), family(11)}
}

/*
================
TestStarterRefillFollowsLevelGrade
================
*/
func TestStarterRefillFollowsLevelGrade(t *testing.T) {
	for _, c := range []struct {
		level  int64
		hp, mp uint32
	}{{1, 4, 11}, {9, 4, 11}, {10, 5, 12}, {25, 6, 13}, {45, 7, 14}, {90, 8, 15}} {
		level := c.level
		character := &Character{Name: "Refill", Level: &level}
		if RefillStarterPotions(character, testRefills()) != 2 {
			t.Fatalf("level %d: refilled %+v", c.level, character.MissionInventory)
		}
		got := map[uint32]int64{}
		for _, row := range character.MissionInventory {
			got[row.RefObjID] = row.StackCount
		}
		if got[c.hp] != 50 || got[c.mp] != 50 || len(got) != 2 {
			t.Fatalf("level %d: rows %v, want 50 of %d and %d", c.level, got, c.hp, c.mp)
		}
	}
}

/*
================
TestStarterRefillTopsUpAPartialStack

A used stack is raised in place; a full one is left alone on the next entry.
================
*/
func TestStarterRefillTopsUpAPartialStack(t *testing.T) {
	level := int64(1)
	character := &Character{Name: "Refill", Level: &level, MissionInventory: []InventoryRow{
		{Slot: 20, RefObjID: 4, StackCount: 7},
	}}
	if !StarterRefillShort(character, testRefills()) {
		t.Fatal("a used stack was reported full")
	}
	RefillStarterPotions(character, testRefills())
	if row := character.MissionInventory[0]; row.Slot != 20 || row.StackCount != 50 {
		t.Fatalf("partial stack = %+v, want raised to 50 in slot 20", row)
	}
	if len(character.MissionInventory) != 2 {
		t.Fatalf("rows = %+v, want the HP stack raised and one MP stack added", character.MissionInventory)
	}
	if StarterRefillShort(character, testRefills()) || RefillStarterPotions(character, testRefills()) != 0 {
		t.Fatal("full stacks were refilled again")
	}
}

/*
================
TestStarterRefillLeavesAFullBag
================
*/
func TestStarterRefillLeavesAFullBag(t *testing.T) {
	level := int64(1)
	character := &Character{Name: "Full", Level: &level}
	for slot := int64(13); slot < 45; slot++ {
		character.MissionInventory = append(character.MissionInventory, InventoryRow{Slot: slot, RefObjID: 1000, StackCount: 1})
	}
	before := len(character.MissionInventory)
	if RefillStarterPotions(character, testRefills()) != 0 || len(character.MissionInventory) != before {
		t.Fatal("a full bag received potions")
	}
}

/*
================
TestStarterRefillFullBagLogsInfoNotWarn

#2441: a full bag at starter refill is the player's own outcome, so
the line logs at info; warnings are reserved for faults (a refill
item missing from itemdata still warns).
================
*/
func TestStarterRefillFullBagLogsInfoNotWarn(t *testing.T) {
	hook := testhook.NewGlobal()
	defer hook.Reset()

	level := int64(1)
	character := &Character{Name: "Full", Level: &level}
	for slot := int64(13); slot < 45; slot++ {
		character.MissionInventory = append(character.MissionInventory, InventoryRow{Slot: slot, RefObjID: 1000, StackCount: 1})
	}
	if RefillStarterPotions(character, testRefills()) != 0 {
		t.Fatal("a full bag received potions")
	}

	lines := 0
	for _, entry := range hook.AllEntries() {
		if !strings.Contains(entry.Message, "has no free bag slot for starter refill") {
			continue
		}
		if entry.Level != log.InfoLevel {
			t.Fatalf("refill refusal logged at %s, want info", entry.Level)
		}
		lines++
	}
	if lines != 2 {
		t.Fatalf("refill refusal lines = %d, want 2 (one per family)", lines)
	}
}
