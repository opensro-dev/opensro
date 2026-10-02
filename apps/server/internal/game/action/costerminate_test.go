/*
===========================================================================

costerminate_test.go - the Clean command retires vehicles, never pets

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestCosTerminateRefusesPetsAndForeignRecords

511E00 answers 5 for an unknown record and for attack or pickup pets,
without touching the record.
================
*/
func TestCosTerminateRefusesPetsAndForeignRecords(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x19c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 50, Summoned: true}
	before := c.Snapshot()
	refused := cosTerminateResult(cosTerminateInvalid).Frames
	for _, bad := range [][]byte{nil, {1}, wire.NewWriter(4).U32(gid + 1).Payload(), wire.NewWriter(4).U32(gid).Payload()} {
		result := rt.HandleCosTerminate(testDivision, c, bad)
		if !reflect.DeepEqual(result.Frames, refused) || len(result.Broadcast) != 0 || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("terminate accepted a pet or foreign record", bad, result)
		}
	}
}

/*
================
TestCosTerminateDismountsAndRetiresRidingHorse

4FBDD0 dismounts the rider before the despawn; a riding horse leaves with
its dismount (4EC78B), so the reply follows the ride frames.
================
*/
func TestCosTerminateDismountsAndRetiresRidingHorse(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["COS_C_HORSE1"] = &enterworld.CharacterRef{Codename: "COS_C_HORSE1", RefObjID: 2191,
		TidWord: 0x09c6, RunSpeed: 100, CanRide: true}
	rt, _ := newTestRuntime(c, refs)
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 2191, Codename: "COS_C_HORSE1", CurrentHP: 100,
		Summoned: true, Mounted: true}
	rt.BindPetSession(testDivision, c, 1)
	result := rt.HandleCosTerminate(testDivision, c, wire.NewWriter(4).U32(gid).Payload())
	last := result.Frames[len(result.Frames)-1]
	if c.ActiveCOS != nil || last.Opcode != opCosTerminateResponse || !reflect.DeepEqual(last.Payload, []byte{1}) {
		t.Fatal("riding horse survived terminate", result)
	}
	var ride, despawn bool
	for _, frame := range result.Broadcast {
		ride = ride || frame.Opcode == wire.OpCosRideState
		despawn = despawn || frame.Opcode == wire.OpObjectDespawn
	}
	if !ride || !despawn {
		t.Fatal("observers did not see the dismount and despawn", result.Broadcast)
	}
	if again := rt.HandleCosTerminate(testDivision, c, wire.NewWriter(4).U32(gid).Payload()); len(again.Broadcast) != 0 {
		t.Fatal("repeated terminate published again")
	}
}

/*
================
TestCosTerminateDropsTransportCargoUnowned

4D1FD0 publishes a transport's goods like a monster's drops with no owner
argument, then the record is retired.
================
*/
func TestCosTerminateDropsTransportCargoUnowned(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	rt.DropRoll = constantDropRoll(0)
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	row := c.MissionInventory[0]
	row.Slot = 0
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100,
		Summoned: true, Container: &domain.COSContainer{Capacity: 1, Rows: []domain.InventoryRow{row}}}
	rt.BindPetSession(testDivision, c, 1)
	result := rt.HandleCosTerminate(testDivision, c, wire.NewWriter(4).U32(gid).Payload())
	if c.ActiveCOS != nil || !reflect.DeepEqual(result.Frames[len(result.Frames)-1].Payload, []byte{1}) {
		t.Fatal("transport survived terminate", result)
	}
	ground := rt.CharacterGroundItems(testDivision, c)
	if len(ground) != 1 || ground[0].RefObjID != row.RefObjID || ground[0].OwnerJID != 0 {
		t.Fatal("cargo was not dropped unowned", ground)
	}
}
