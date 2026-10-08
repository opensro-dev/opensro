/*
===========================================================================
summonlevel_test.go - level-mapped mounts use the real item and restore doors
===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestShippedOstrichSummonKeepsSelectedIdentityAcrossRestore
================
*/
func TestShippedOstrichSummonKeepsSelectedIdentityAcrossRestore(t *testing.T) {
	items := shippedItems(t)
	item, found := items.ItemRefByCodename("ITEM_COS_C_OSTRICH_SCROLL")
	if !found {
		t.Fatal("missing ostrich scroll")
	}
	ref, found := enterworld.SummonCharacterReference(items, item, 10)
	if !found || ref.Codename != "COS_C_OSTRICH_10" || ref.RunSpeed != 150 {
		t.Fatalf("wrong authored mount: %+v", ref)
	}
	c := testCharacter()
	c.Level = testInt64(10)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 22, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	rt.BindPetSession(testDivision, c, 1)
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(22).U16(item.TypeFlags()).Payload())
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, wire.OpItemUseVisual, wire.OpCosRecordCreate, wire.OpSingleObjectSpawn, wire.OpCosRideState, movementSpeedOpcode)
	if c.ActiveCOS == nil || c.ActiveCOS.Codename != ref.Codename || c.ActiveCOS.RefObjID != ref.RefObjID || !c.ActiveCOS.Mounted || c.ActiveCOS.CurrentHP != ref.MaxHP {
		t.Fatalf("summon did not install chosen variant: %+v", c.ActiveCOS)
	}
	if len(c.MissionInventory) != 0 {
		t.Fatal("consumed scroll remains in inventory", c.MissionInventory)
	}
	_, run := rt.EntryMovementSpeeds(testDivision, c.Name)
	if run != 150 {
		t.Fatalf("mounted speed=%v", run)
	}
	c.Level = testInt64(60)
	restored, _ := newTestRuntime(c, items)
	restored.RestoreTimedSkillJobs(testDivision, c.Name)
	if c.ActiveCOS.Codename != ref.Codename || c.ActiveCOS.RefObjID != ref.RefObjID {
		t.Fatal("restore reselected a different level variant", c.ActiveCOS)
	}
	_, run = restored.EntryMovementSpeeds(testDivision, c.Name)
	if run != 150 {
		t.Fatalf("restored mounted speed=%v", run)
	}
}

/*
================
TestMissingSelectedMountVariantDoesNotConsumeOrFallback
================
*/
func TestMissingSelectedMountVariantDoesNotConsumeOrFallback(t *testing.T) {
	c := testCharacter()
	source := testCosSource(testItems())
	item := source.staticItemSource["ITEM_COS_T_DHORSE3"]
	item.SummonLevelThresholds = []uint8{1, 5, 255}
	c.Level = testInt64(10)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 22, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, source)
	before := c.Snapshot()
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(22).U16(item.TypeFlags()).Payload())
	if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("missing level variant consumed scroll or used direct base", result)
	}
}
