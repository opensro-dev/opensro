/*
===========================================================================

combatstats_copy_test.go - the identical-numbers harness (#2207)

The #1943 census (#2207) classifies every remaining whole-copy stat
root by what actually reads its copy. Every stat read funnels through
rt.playerCombatStats into combat.PlayerStatsWithModifiers, so the
numbers a root can produce are exactly what that function returns.
This harness pins the contract the #2209 conversions rely on: the
full copy and the equipment-band SnapshotCombat of the same character
produce identical Stats and Loadout, cold and warm (the projection
cache keys on the equipment band), a bag-only mutation moves
neither, and an equipment mutation moves both identically. The last
leg is the seeing-eye check: the harness must notice when a
projection input actually changes.
===========================================================================
*/

package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
combatStatsOf

One character's projected numbers through the production funnel.
================
*/
func combatStatsOf(t *testing.T, rt *Runtime, c *enterworld.Character) (combat.Stats, combat.Loadout) {
	t.Helper()
	stats, loadout, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), nil, nil)
	if err != nil {
		t.Fatalf("PlayerStatsWithModifiers: %v", err)
	}
	return stats, loadout
}

/*
================
mustSameCombatNumbers

The full copy and the narrow copy must agree exactly.
================
*/
func mustSameCombatNumbers(t *testing.T, label string, full, narrow *enterworld.Character, rt *Runtime) (combat.Stats, combat.Loadout) {
	t.Helper()
	fullStats, fullLoadout := combatStatsOf(t, rt, full)
	narrowStats, narrowLoadout := combatStatsOf(t, rt, narrow)
	if !reflect.DeepEqual(fullStats, narrowStats) {
		t.Fatalf("%s: stats differ\nfull:   %+v\nnarrow: %+v", label, fullStats, narrowStats)
	}
	if !reflect.DeepEqual(fullLoadout, narrowLoadout) {
		t.Fatalf("%s: loadout differs\nfull:   %+v\nnarrow: %+v", label, fullLoadout, narrowLoadout)
	}
	return narrowStats, narrowLoadout
}

/*
================
TestCombatStatsIdenticalOnCombatCopy

The fixture character carries the poisoned bag from the copy
differential (plain arrows, an attack pet's summon row, a quest
item). Its numbers on the full copy and on SnapshotCombat agree,
cold and with the projection cache warm; mutating only bag rows
afterwards moves neither; raising the sword's plus moves the full
copy and the narrow copy by exactly the same amount.
================
*/
func TestCombatStatsIdenticalOnCombatCopy(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 1000)
	loadCombatBag(character)

	full := rt.characterSnapshot(testDivision, character)
	narrow := full.SnapshotCombat()
	for _, row := range narrow.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) {
			t.Fatalf("narrow copy kept bag row at slot %d", row.Slot)
		}
	}
	baseStats, _ := mustSameCombatNumbers(t, "cold cache", full, narrow, rt)
	// The second read is served by the equipment-keyed projection
	// cache; it must still agree with the full copy bit for bit.
	mustSameCombatNumbers(t, "warm cache", full, narrow, rt)

	for index := range character.MissionInventory {
		row := &character.MissionInventory[index]
		if row.Slot >= int64(inventory.EquipmentSlotEnd) {
			row.StackCount++
		}
		if row.Summon != nil {
			row.Summon.CurrentHP++
		}
	}
	mutated := rt.characterSnapshot(testDivision, character)
	bagStats, _ := mustSameCombatNumbers(t, "bag-only mutation", mutated, mutated.SnapshotCombat(), rt)
	if !reflect.DeepEqual(baseStats, bagStats) {
		t.Fatalf("bag-only mutation moved the numbers\nbefore: %+v\nafter:  %+v", baseStats, bagStats)
	}

	for index := range character.MissionInventory {
		row := &character.MissionInventory[index]
		if row.Slot == 6 {
			row.Plus++
		}
	}
	geared := rt.characterSnapshot(testDivision, character)
	gearedStats, _ := mustSameCombatNumbers(t, "equipment mutation", geared, geared.SnapshotCombat(), rt)
	if reflect.DeepEqual(baseStats, gearedStats) {
		t.Fatal("sword plus moved neither copy; the harness cannot see a projection input")
	}
}
