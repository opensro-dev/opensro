/*
===========================================================================

cossatiety_test.go - live pet hunger, feeding recovery and shared fatal cleanup

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestShippedPetSatietyTickFeedingAndDeath
================
*/
func TestShippedPetSatietyTickFeedingAndDeath(t *testing.T) {
	items := shippedItems(t)
	c := testCharacter()
	rt, _ := newTestRuntime(c, items)
	equipShippedPet(t, rt, c, items, "COS_P_WOLF_002")
	ref, valid := rt.cosCharacterRef(c)
	if !valid || ref.SatietyMinutes != 5 {
		t.Fatal("shipped hunger parameter missing", ref)
	}
	pet := c.ActiveCOS
	pet.Satiety = 3000
	pet.StateFlags = 3
	rt.BindPetSession(testDivision, c, 1)
	rt.advancePets(1000)
	rt.advancePets(4000)
	if pet.Satiety != 2999 {
		t.Fatal("live tick failed", pet.Satiety)
	}
	stats, err := cosCombatStats(ref, pet, nil)
	if err != nil || stats.PhysicalDefense != ref.Parameters.PhysicalDefense/2 {
		t.Fatal("hungry defense", stats, err)
	}
	food, _ := items.ItemRefByCodename("ITEM_COS_P_HGP_POTION_01")
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21, RefObjID: food.RefObjID, Codename: food.Codename, TypeFlags: food.TypeFlags(), StackCount: 1})
	result := rt.HandleItemUse(testDivision, c, petUse(c, food, pet.GID, -1))
	if len(result.Frames) < 2 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatal("feeding refused", result)
	}
	stats, err = cosCombatStats(ref, pet, nil)
	if err != nil || stats.PhysicalDefense != ref.Parameters.PhysicalDefense {
		t.Fatal("feeding failed to restore keeper", stats, err)
	}
	pet.Satiety = 1
	rt.storeCosAbnormal(testDivision, c.Name, pet.GID, &abnormal.Block{Mask: abnormal.Burn.Bit()})
	output := rt.advancePets(7000)
	if pet.Satiety != 0 || pet.CurrentHP != 0 || pet.StateFlags&1 != 0 || len(output) == 0 {
		t.Fatal("starvation did not retire pet", pet, output)
	}
	if block := rt.cosAbnormal(testDivision, c.Name, pet.GID); block != nil && block.Mask != 0 {
		t.Fatal("starvation retained statuses")
	}
	if again := rt.advancePets(10000); len(again) != 0 {
		t.Fatal("death replayed", again)
	}
}
