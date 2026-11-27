/*
===========================================================================

canpick_test.go - a ground row whose item forbids pickup stays down

The item's authored pickup permission (itemdata canPick) is the pickup
gate the cannot-be-picked notice exists for: a row with the permission
clear answers 0x39 and stays on the ground, by hand for the player
lane; the COS lane shares the same gate (#4490, U6-10).

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// unpickableCodename is the fixture's gated row.
const unpickableCodename = "ITEM_FIXTURE_NOPICK"

/*
================
canPickRefs
================
*/
func canPickRefs() staticItemSource {
	return staticItemSource{
		unpickableCodename: {
			RefObjID: 88001, Codename: unpickableCodename, TypeIDs: [4]int64{3, 3, 1, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canPick": 0, "canTrade": 1, "maxStack": 10}),
		},
		"ITEM_FIXTURE_PICKABLE": {
			RefObjID: 88002, Codename: "ITEM_FIXTURE_PICKABLE", TypeIDs: [4]int64{3, 3, 1, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canPick": 1, "canTrade": 1, "maxStack": 10}),
		},
	}
}

/*
================
TestPickupOfAnUnpickableRowAnswers0x39
================
*/
func TestPickupOfAnUnpickableRowAnswers0x39(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, canPickRefs())
	spawn := simulation.SeedWorldState(c).Spawn
	drop := PlanItemDrop(inventory.Item{RefObjID: 88001, Codename: unpickableCodename, TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), Quantity: 1}, 1, spawn, c.Name, clock.Now())
	added := rt.Ground.Add(testDivision, drop)
	result := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: added.Gid}.Encode())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemMoveResponse {
		t.Fatalf("refusal frames: %+v", result.Frames)
	}
	if want := wire.EncodeItemMoveError(wire.ErrCodeCannotBePicked); !bytes.Equal(result.Frames[0].Payload, want) {
		t.Fatalf("refusal = % X, want % X", result.Frames[0].Payload, want)
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatalf("ground count = %d, want the drop still down", rt.Ground.Count(testDivision))
	}
	for _, row := range c.MissionInventory {
		if row.Codename == unpickableCodename {
			t.Fatalf("unpickable row entered the bag: %+v", c.MissionInventory)
		}
	}
}

/*
================
TestPickupOfAPickableRowStillGrants
================
*/
func TestPickupOfAPickableRowStillGrants(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, canPickRefs())
	spawn := simulation.SeedWorldState(c).Spawn
	drop := PlanItemDrop(inventory.Item{RefObjID: 88002, Codename: "ITEM_FIXTURE_PICKABLE", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), Quantity: 1}, 1, spawn, c.Name, clock.Now())
	added := rt.Ground.Add(testDivision, drop)
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: added.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatalf("ground count = %d, want the row taken", rt.Ground.Count(testDivision))
	}
	found := false
	for _, row := range c.MissionInventory {
		if row.Codename == "ITEM_FIXTURE_PICKABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pickable row missing from the bag: %+v", c.MissionInventory)
	}
}

/*
================
popCardRefs

The shipped Magic Pop card row: its pickup permission is clear (BUG-103).
================
*/
func popCardRefs() staticItemSource {
	return staticItemSource{
		gacha.CardCodename: {
			RefObjID: 88003, Codename: gacha.CardCodename, TypeIDs: [4]int64{3, 3, 3, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canPick": 0, "canTrade": 0, "maxStack": 50}),
		},
	}
}

/*
================
pickUpPopCard

Drops one Magic Pop card beside c and asks for it: whether it left the
ground.
================
*/
func pickUpPopCard(t *testing.T, popTicket bool) bool {
	t.Helper()
	c := testCharacter()
	rt, clock := newTestRuntime(c, popCardRefs())
	rt.UniqueRules.PopTicket = popTicket
	spawn := simulation.SeedWorldState(c).Spawn
	drop := PlanItemDrop(inventory.Item{RefObjID: 88003, Codename: gacha.CardCodename, TypeFlags: wire.PackTypeFlags(3, 3, 3, 1), Quantity: 1}, 1, spawn, c.Name, clock.Now())
	added := rt.Ground.Add(testDivision, drop)
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: added.Gid}.Encode())
	return rt.Ground.Count(testDivision) == 0
}

/*
================
TestPopTicketCardIsPickedUpWhileTheTicketRuleRuns

BUG-103: the unique kill's Magic Pop ticket answered "cannot be
obtained". With SRO_UNIQUE_POP_TICKET on it is picked up; off, the native
permission gate still holds.
================
*/
func TestPopTicketCardIsPickedUpWhileTheTicketRuleRuns(t *testing.T) {
	if !pickUpPopCard(t, true) {
		t.Fatal("the Pop ticket stayed on the ground with SRO_UNIQUE_POP_TICKET on")
	}
	if pickUpPopCard(t, false) {
		t.Fatal("a native card with its permission clear was picked up with the flag off")
	}
}
