/*
===========================================================================

equipmentwear_test.go - CGObjPC_RollEquipmentWear and the durability point

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
wearFixture

The test character with its sword equipped (slot 6, durability 96) and a
helmet in slot 1 (durability 2). rolls are the wear roll's values in order.
================
*/
func wearFixture(t *testing.T, rolls ...uint32) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	c.MissionInventory[0].Slot = wearWeaponSlot
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 1, RefObjID: 9001,
		Codename: "ITEM_CH_M_HEAVY_01_HA_A", TypeFlags: wire.PackTypeFlags(3, 1, 1, 1), Durability: 2, StackCount: 1})
	items := testItems()
	items["ITEM_CH_M_HEAVY_01_HA_A"] = &enterworld.ItemRef{RefObjID: 9001, Codename: "ITEM_CH_M_HEAVY_01_HA_A",
		TypeIDs: [4]int64{3, 1, 1, 1}, Country: 3, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		Combat: &enterworld.ItemCombatRef{}}
	rt, _ := newTestRuntime(c, items)
	rt.WearRoll = func() (uint32, error) {
		if len(rolls) == 0 {
			return 99, nil
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value, nil
	}
	return rt, c
}

/*
================
durabilityAt
================
*/
func durabilityAt(c *enterworld.Character, slot int64) int64 {
	for _, row := range c.MissionInventory {
		if row.Slot == slot {
			return row.Durability
		}
	}
	return -1
}

/*
================
TestAttackWearsTheWeaponAtTwoPercentPerAttempt

4A9A90 mode 0: one unblocked attempt is a 2% chance; abs(rand()) % 100 + 1
<= 2 passes for 0 and 1 only.
================
*/
func TestAttackWearsTheWeaponAtTwoPercentPerAttempt(t *testing.T) {
	rt, c := wearFixture(t, 1)
	var tally wearTally
	tally.note(false, true)
	tally.note(true, true) // a blocked attempt does not count
	wear := rt.applyEquipmentWear(testDivision, c, tally)
	if durabilityAt(c, wearWeaponSlot) != 95 || len(wear.actor) != 1 || wear.actor[0].Opcode != wire.OpItemDurability {
		t.Fatalf("weapon durability %d, frames %+v; want 95 and one 0x31E8", durabilityAt(c, wearWeaponSlot), wear.actor)
	}
	if p := wear.actor[0].Payload; p[0] != wearWeaponSlot || binary.LittleEndian.Uint32(p[1:]) != 95 {
		t.Fatalf("durability frame %x", p)
	}
	rt, c = wearFixture(t, 2)
	if rt.applyEquipmentWear(testDivision, c, tally); durabilityAt(c, wearWeaponSlot) != 96 {
		t.Fatal("a roll of 3 wore the weapon at a 2% chance")
	}
}

/*
================
TestHitsWearTheFirstArmourSlotWhoseRollSucceeds

Mode 1 rolls slots 0..7 but 6 in order and takes the first success, even
on an empty socket (the point is then lost). Slot 0 is empty and slot 1 is
the helmet; a broken item then stops wearing and its zero crossing
re-derives the stats.
================
*/
func TestHitsWearTheFirstArmourSlotWhoseRollSucceeds(t *testing.T) {
	rt, c := wearFixture(t, 99, 0, 99, 0, 99, 0)
	var tally wearTally
	tally.note(false, false)
	wear := rt.applyEquipmentWear(testDivision, c, tally)
	if durabilityAt(c, 1) != 1 || durabilityAt(c, wearWeaponSlot) != 96 || len(wear.actor) != 1 {
		t.Fatalf("helmet %d weapon %d frames %d; want 1, 96 and one frame", durabilityAt(c, 1), durabilityAt(c, wearWeaponSlot), len(wear.actor))
	}
	wear = rt.applyEquipmentWear(testDivision, c, tally)
	stats := false
	for _, frame := range wear.actor {
		stats = stats || frame.Opcode == wire.OpBaseStats
	}
	if durabilityAt(c, 1) != 0 || !stats {
		t.Fatalf("breaking the helmet left %d and published stats %v", durabilityAt(c, 1), stats)
	}
	if wear = rt.applyEquipmentWear(testDivision, c, tally); durabilityAt(c, 1) != 0 || len(wear.actor) != 0 {
		t.Fatal("a broken item lost another point")
	}
}
