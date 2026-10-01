package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// THE MULTIPLEXED-OPCODE REGRESSION GUARD: a pickup 0x72CD payload must
// still reach the pickup decoder unchanged - the accept branch fires only
// on positively-matched skill shapes, and every pickup form keeps its
// existing conversation (grant, cancel release, genuine pickup refusals).
func TestPickupPayloadsStillReachThePickupDecoderAfterTheAcceptLanding(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	// An underfoot gold heap: the pickup interact form must still GRANT.
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		777,
		simulation.Spawn{RegionID: start.RegionID, X: start.X, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))

	grant := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: heap.Gid}.Encode())
	assertOpcodes(t, grant.Frames,
		wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn)
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("underfoot pickup did not consume the heap")
	}

	// The bare [02] cancel still answers the latch release.
	cancel := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Cancel: true}.Encode())
	assertOpcodes(t, cancel.Frames, wire.OpActionState)

	// A pickup of a gone gid still answers the genuine pickup refusal pair.
	refusal := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: heap.Gid}.Encode())
	assertOpcodes(t, refusal.Frames, wire.OpActionState, wire.OpItemMoveResponse)
}

// Family ownership is decided before strict decoding. These are either
// malformed members of a non-pickup family or the one byte-valid retail
// family whose gateway gameplay owner is not implemented yet. None may emit the
// B2CD/B06D pickup refusal pair, clear a pending pickup, or consume a drop.
func TestNonPickup72CDFamiliesNeverFallThroughToPickup(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		777,
		simulation.Spawn{RegionID: start.RegionID, X: start.X, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))

	forms := map[string][]byte{
		"malformed basic attack": {0x01, 0x01, 0x02, 0x44, 0x33, 0x22, 0x11},
		"malformed skill":        {0x01, 0x04, 0x78, 0x56, 0x34, 0x12, 0x03},
		"fortress structure":     wire.FortressStructureInteract{TargetGid: 0x11223344}.Encode(),
		"unknown lane":           {0x01, 0x7f, 0x00},
	}
	for name, payload := range forms {
		t.Run(name, func(t *testing.T) {
			result := rt.HandleTargetInteract(testDivision, character, payload)
			if len(result.Frames) != 0 || len(result.Broadcast) != 0 || result.Pending != nil {
				t.Fatalf("non-pickup family answered pickup state: %+v", result)
			}
			if rt.Ground.Count(testDivision) != 1 {
				t.Fatal("non-pickup family consumed the ground item")
			}
		})
	}

	grant := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: heap.Gid}.Encode())
	assertOpcodes(t, grant.Frames,
		wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn)
}
