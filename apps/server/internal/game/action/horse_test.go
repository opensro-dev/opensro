/*
===========================================================================

horse_test.go - consumable riding horse admission and lifetime

Exercise the authenticated item and ride doors rather than constructing a
mounted actor that bypasses the native automatic-binding transaction.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestRidingHorseSummonMovesAndRetiresOnDismount
================
*/
func TestRidingHorseSummonMovesAndRetiresOnDismount(t *testing.T) {
	c := testCharacter()
	source := testCosSource(testItems())
	source.characters["COS_T_DHORSE3"].TidWord = 0x9c6
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3", TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1,
	})
	rt, _ := newTestRuntime(c, source)
	rt.BindPetSession(testDivision, c, 1)
	summoned := rt.HandleItemUse(testDivision, c, []byte{22, 0xec, 0x11})
	assertOpcodes(t, summoned.Frames, wire.OpItemUseResponse, wire.OpCosRecordCreate, wire.OpSingleObjectSpawn, wire.OpCosRideState, movementSpeedOpcode)
	if c.ActiveCOS == nil || !c.ActiveCOS.Mounted || c.ActiveCOS.StateFlags != 3 {
		t.Fatalf("summon did not bind the riding horse: %+v", c.ActiveCOS)
	}
	gid := c.ActiveCOS.GID
	if len(summoned.Frames[1].Payload) != 17 || binary.LittleEndian.Uint32(summoned.Frames[1].Payload) != gid {
		t.Fatalf("horse record must omit death word: %x", summoned.Frames[1].Payload)
	}
	presented := rt.PetPresentation(testDivision, c.Name)
	if presented == nil || !presented.Mounted || presented.Row.Band != 1 {
		t.Fatalf("horse missing from peer presentation: %+v", presented)
	}
	rider := enterworld.ObjectIDForCharacter(c)
	if !reflect.DeepEqual(summoned.Frames[3].Payload, wire.EncodeCosRideState(rider, true, gid)) {
		t.Fatal("summon did not publish native ride binding")
	}
	stopCalled := false
	rt.StopCOS = func(division string, owner *enterworld.Character, got uint32, heading uint16) ([]wire.Frame, []wire.Frame) {
		stopCalled = division == testDivision && owner == c && got == gid && heading == 55
		return nil, nil
	}
	rt.HandleCosCommand(testDivision, c, wire.NewWriter(7).U32(gid).U8(wire.CosCommandStopTag).U16(55).Payload())
	if !stopCalled {
		t.Fatal("riding horse did not reach the shared movement owner")
	}
	result := rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(0).U32(gid).Payload())
	if c.ActiveCOS != nil || rt.PetPresentation(testDivision, c.Name) != nil {
		t.Fatal("dismounted riding horse survived")
	}
	despawned := false
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpObjectDespawn && binary.LittleEndian.Uint32(frame.Payload) == gid {
			despawned = true
		}
	}
	if !despawned || !reflect.DeepEqual(result.Frames, result.Broadcast) {
		t.Fatal("riding retirement was not visible to owner and peers", result)
	}
	if retry := rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(1).U32(gid).Payload()); !reflect.DeepEqual(retry.Frames, cosRideFailure(cosRideUnknownActor).Frames) {
		t.Fatal("retired horse could be rebound", retry)
	}
}

/*
================
TestVehicleSummonRefusalDoesNotConsumeOrReplace
================
*/
func TestVehicleSummonRefusalDoesNotConsumeOrReplace(t *testing.T) {
	for _, battle := range []bool{false, true} {
		c := testCharacter()
		source := testCosSource(testItems())
		rt, _ := newTestRuntime(c, source)
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3", TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1})
		payload := []byte{22, 0xec, 0x11, 99}
		if battle {
			c.BattleUntilMs = rt.Now().UnixMilli() + 1000
			payload = payload[:3]
		}
		before := c.Snapshot()
		result := rt.HandleItemUse(testDivision, c, payload)
		if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("refused summon mutated durable state", battle, result)
		}
	}
}

/*
================
TestShippedRidingHorseSummonerAndReference

Pin the authored association and ride permission so synthetic transport
fixtures cannot conceal a column or family mismatch in the actual catalogue.
================
*/
func TestShippedRidingHorseSummonerAndReference(t *testing.T) {
	items := shippedItems(t)
	item, found := items.ItemRefByCodename("ITEM_COS_C_HORSE1")
	if !found {
		t.Fatal("missing riding horse item")
	}
	ref, found := items.CharacterRefByCodename(item.AssociatedCharacterCodename)
	if !found || ref.TidWord>>11 != 1 || !ref.CanRide {
		t.Fatalf("invalid riding horse reference: %+v", ref)
	}
	c := testCharacter()
	c.Level = testInt64(10)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 22, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(22).U16(item.TypeFlags()).Payload())
	if result.Frames[0].Payload[0] != 1 || c.ActiveCOS == nil || !c.ActiveCOS.Mounted || c.ActiveCOS.RefObjID != ref.RefObjID {
		t.Fatal("authored horse could not be summoned and ridden", result)
	}
}

/*
================
TestVehicleRestoreBindsOnlyNewActorLifetime
================
*/
func TestVehicleRestoreBindsOnlyNewActorLifetime(t *testing.T) {
	for _, band := range []uint16{1, 2} {
		c := testCharacter()
		source := testCosSource(testItems())
		ref := source.characters["COS_T_DHORSE3"]
		ref.TidWord = band<<11 | 0x1c6
		gid, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: ref.RefObjID, Codename: ref.Codename, CurrentHP: 100, Summoned: true}
		rt, _ := newTestRuntime(c, source)
		rt.RestoreTimedSkillJobs(testDivision, c.Name)
		if !c.ActiveCOS.Mounted {
			t.Fatalf("band %d saved vehicle did not bind", band)
		}
		walk, run := rt.EntryMovementSpeeds(testDivision, c.Name)
		if walk != ref.WalkSpeed || run != ref.RunSpeed {
			t.Fatal("restored vehicle inherited player speed", walk, run)
		}
		rt.BindPetSession(testDivision, c, 1)
		rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(0).U32(gid).Payload())
		rt.RestoreTimedSkillJobs(testDivision, c.Name)
		if band == 1 && c.ActiveCOS != nil || band == 2 && c.ActiveCOS.Mounted {
			t.Fatal("duplicate entry resurrected or remounted vehicle", band)
		}
	}
}
