/*
===========================================================================

coscancel_test.go - companion cancellation authority and retained pet state

===========================================================================
*/
package action

import (
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCosCancellationPreservesDurablePetAndRetiresPickup
================
*/
func TestCosCancellationPreservesDurablePetAndRetiresPickup(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, clock := newTestRuntime(c, refs)
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 73,
		CurrentMP: 12, Summoned: true, StateFlags: 3, Experience: 900, Level: 5, Satiety: 8100,
		Name: "Companion", InventorySlot: 17, Container: &domain.COSContainer{Capacity: 28}}
	rt.BindPetSession(testDivision, c, 101)
	rt.advancePets(clock.NowMs())
	state := rt.petSessions[petOwnerKey{testDivision, strings.ToLower(c.Name)}]
	state.pickup = &wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: 44}
	state.pickupCommand = true
	payload := wire.NewWriter(4).U32(gid).Payload()
	before := c.Snapshot()
	for _, bad := range [][]byte{nil, {1}, append(append([]byte{}, payload...), 0), wire.NewWriter(4).U32(gid + 1).Payload()} {
		rt.HandleCosCancel(testDivision, c, bad)
		if !reflect.DeepEqual(before, c.Snapshot()) || state.pickup == nil {
			t.Fatal("invalid cancellation changed pet authority")
		}
	}
	key := simulation.WorldKey(testDivision, c.Name)
	initial := rt.cosLiveSpawn(testDivision, c, clock.NowMs())
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn.X = initial.X + cosCancelRange
	})
	result := rt.HandleCosCancel(testDivision, c, payload)
	if !reflect.DeepEqual(result.Frames[0].Payload, []byte{2, 4}) || !c.ActiveCOS.Summoned {
		t.Fatal("cancellation accepted the excluded range boundary", result)
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = initial
	})
	result = rt.HandleCosCancel(testDivision, c, payload)
	assertOpcodes(t, result.Frames, opCosCancelResponse, wire.OpCosCommandResult, wire.OpObjectDespawn)
	assertOpcodes(t, result.Broadcast, wire.OpObjectDespawn)
	want := *before.ActiveCOS
	want.Summoned = false
	want.StateFlags &^= cosStateSummoned
	if !reflect.DeepEqual(c.ActiveCOS, &want) {
		t.Fatal("cancellation lost durable pet progress or inventory", c.ActiveCOS)
	}
	if state.pickup != nil || state.follower != nil || rt.PetPresentation(testDivision, c.Name) != nil {
		t.Fatal("cancellation retained the live pet")
	}
	if repeated := rt.HandleCosCancel(testDivision, c, payload); len(repeated.Broadcast) != 0 {
		t.Fatal("repeated cancellation published another despawn")
	}
	// 511A60 checks the owner's life and pet combat target, not pet HP.
	// A retained corpse must be cancellable without reviving it first.
	c.ActiveCOS.Summoned = true
	c.ActiveCOS.CurrentHP = 0
	c.ActiveCOS.StateFlags = cosStateSummoned
	result = rt.HandleCosCancel(testDivision, c, payload)
	assertOpcodes(t, result.Frames, opCosCancelResponse, wire.OpObjectDespawn)
	if c.ActiveCOS.Summoned || c.ActiveCOS.CurrentHP != 0 || c.ActiveCOS.StateFlags != 0 {
		t.Fatal("dead-pet cancellation revived or retained the actor")
	}
}
