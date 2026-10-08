/*
===========================================================================

npcrepair_test.go - repairing equipment at a smith

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
repairFixture

The test character beside a selected smith (gid 3001) with its sword
equipped at 30 of 71 durability; CostRepair 142 is 2 gold a point.
================
*/
func repairFixture(t *testing.T, codename string) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	c.MissionInventory[0].Slot = wearWeaponSlot
	c.MissionInventory[0].Durability = 30
	c.MissionInventory[0].VarianceBits = "31"
	items := testItems()
	sword := *items["ITEM_CH_SWORD_01_A_RARE"]
	low := int64(40)
	sword.MaxDurability, sword.VarianceIntMin1c0 = 71, &low
	sword.NativeFields = enterworld.NewNativeFields(map[string]float64{"canRepair": 1, "canRevive": 1, "repairCostB4": 142, "reviveCostB8": 0})
	items[sword.Codename] = &sword
	rt, _ := newTestRuntime(c, items)
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 3001, RefObjID: 3011, Codename: codename, TalkFlags: 1,
		Services:      simulation.NpcServicesForCodename(codename),
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.NpcSpawn.Enabled = true
	rt.Selected.Set(testDivision, c.Name, 3001)
	return rt, c
}

/*
================
TestSmithRepairsEverythingForItsPrice
================
*/
func TestSmithRepairsEverythingForItsPrice(t *testing.T) {
	rt, c := repairFixture(t, "NPC_CH_SMITH")
	out := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(5).U32(3001).U8(repairAllSlots).Payload())
	if len(out.Frames) == 0 || !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("repair answered %+v", out.Frames)
	}
	if durabilityAt(c, wearWeaponSlot) != 71 || *c.Gold != 5000-82 {
		t.Fatalf("durability %d gold %d; want 71 and %d", durabilityAt(c, wearWeaponSlot), *c.Gold, 5000-82)
	}
	again := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(6).U32(3001).U8(repairOneSlot).U8(wearWeaponSlot).Payload())
	if *c.Gold != 5000-82 || len(again.Frames) != 1 || !bytes.Equal(again.Frames[0].Payload, []byte{2, repairErrNothingLost}) {
		t.Fatalf("a full item was charged again: %+v", again.Frames)
	}
}

/*
================
TestRepairOfAWholeItemIsNotAPurseRefusal

A whole item, such as a fresh drop the client prices one point short
(78BD00 rounds where 495D60 truncates), answers 496E60's 0x1C09, which the
client keeps silent; Repair All with nothing damaged still answers 1. Only
a quote the gold cannot pay says 0x1C07.
================
*/
func TestRepairOfAWholeItemIsNotAPurseRefusal(t *testing.T) {
	rt, c := repairFixture(t, "NPC_CH_SMITH")
	c.MissionInventory[0].Slot = 13
	c.MissionInventory[0].Durability = 71
	one := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(6).U32(3001).U8(repairOneSlot).U8(13).Payload())
	if len(one.Frames) != 1 || !bytes.Equal(one.Frames[0].Payload, []byte{2, repairErrNothingLost}) || *c.Gold != 5000 {
		t.Fatalf("whole item answered %+v gold %d", one.Frames, *c.Gold)
	}
	all := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(5).U32(3001).U8(repairAllSlots).Payload())
	if len(all.Frames) != 1 || !bytes.Equal(all.Frames[0].Payload, []byte{1}) || *c.Gold != 5000 {
		t.Fatalf("repair all of whole items answered %+v gold %d", all.Frames, *c.Gold)
	}
	empty := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(6).U32(3001).U8(repairOneSlot).U8(40).Payload())
	if !bytes.Equal(empty.Frames[0].Payload, []byte{2, repairErrEmptySlot}) {
		t.Fatalf("empty slot answered %+v", empty.Frames)
	}
	c.MissionInventory[0].Durability = 30
	poor := int64(1)
	c.Gold = &poor
	short := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(6).U32(3001).U8(repairOneSlot).U8(13).Payload())
	if !bytes.Equal(short.Frames[0].Payload, []byte{2, repairErrNoGold}) || durabilityAt(c, 13) != 30 {
		t.Fatalf("a purse below one point answered %+v", short.Frames)
	}
}

/*
================
TestBrokenItemNeedsCanRevive

4C7BB2: a broken item without itemdata CanRevive answers 0x1C12.
================
*/
func TestBrokenItemNeedsCanRevive(t *testing.T) {
	rt, c := repairFixture(t, "NPC_CH_SMITH")
	c.MissionInventory[0].Durability = 0
	sword := *rt.deps.ItemReferences().(staticItemSource)["ITEM_CH_SWORD_01_A_RARE"]
	sword.NativeFields = enterworld.NewNativeFields(map[string]float64{"canRepair": 1, "canRevive": 0, "repairCostB4": 142})
	rt.deps.ItemReferences().(staticItemSource)[sword.Codename] = &sword
	out := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(6).U32(3001).U8(repairOneSlot).U8(wearWeaponSlot).Payload())
	if !bytes.Equal(out.Frames[0].Payload, []byte{2, repairErrNotRevived}) || durabilityAt(c, wearWeaponSlot) != 0 {
		t.Fatalf("broken item without CanRevive answered %+v", out.Frames)
	}
}

/*
================
TestRepairRefusesAMerchantAndAnEmptyPurse
================
*/
func TestRepairRefusesAMerchantAndAnEmptyPurse(t *testing.T) {
	rt, c := repairFixture(t, "NPC_CH_POTION")
	out := rt.HandleNpcRepair(testDivision, c, wire.NewWriter(5).U32(3001).U8(repairAllSlots).Payload())
	if !bytes.Equal(out.Frames[0].Payload, []byte{2, repairErrNotService}) || durabilityAt(c, wearWeaponSlot) != 30 {
		t.Fatalf("a merchant repaired: %+v", out.Frames)
	}
	rt, c = repairFixture(t, "NPC_CH_SMITH")
	zero := int64(0)
	c.Gold = &zero
	out = rt.HandleNpcRepair(testDivision, c, wire.NewWriter(5).U32(3001).U8(repairAllSlots).Payload())
	if !bytes.Equal(out.Frames[0].Payload, []byte{2, repairErrNoGold}) || durabilityAt(c, wearWeaponSlot) != 30 {
		t.Fatalf("an empty purse repaired: %+v", out.Frames)
	}
}

/*
================
TestRepairHammerRestoresEverythingFree

49C2B0 case 6: the hammer repairs every damaged item without gold; with
nothing damaged it answers 0x1888 and stays in the bag.
================
*/
func TestRepairHammerRestoresEverythingFree(t *testing.T) {
	rt, c := repairFixture(t, "NPC_CH_SMITH")
	hammer := &enterworld.ItemRef{RefObjID: 3829, Codename: "ITEM_MALL_REPAIR_HAMMER", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 7}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10})}
	rt.deps.(*enterworld.Deps).Items.(staticItemSource)[hammer.Codename] = hammer
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21, RefObjID: hammer.RefObjID,
		Codename: hammer.Codename, TypeFlags: hammer.TypeFlags(), StackCount: 2})
	use := wire.NewWriter(3).U8(21).U16(hammer.TypeFlags()).Payload()
	out := rt.HandleItemUse(testDivision, c, use)
	if out.Frames[0].Opcode != wire.OpItemUseResponse || out.Frames[0].Payload[0] != wire.ResultSuccess ||
		durabilityAt(c, wearWeaponSlot) != 71 || *c.Gold != 5000 {
		t.Fatalf("hammer: durability %d gold %d frames %+v", durabilityAt(c, wearWeaponSlot), *c.Gold, out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, errCodeNothingToRepair)
}
