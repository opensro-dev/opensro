package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

func ownedGoldAtPlayer(t *testing.T, rt *Runtime, clock *fakeClock, character *enterworld.Character, owner uint32) grounditem.Item {
	t.Helper()
	spawn := simulation.SeedWorldState(character).Spawn
	drop := PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		777, spawn, "killer", clock.Now(),
	)
	drop.OwnerJID = owner
	return rt.Ground.Add(testDivision, drop)
}

func TestOwnedPickupAllowsSelfAndItemSharingPartyOnly(t *testing.T) {
	t.Run("foreign owner refuses before approach", func(t *testing.T) {
		character := testCharacter()
		rt, clock := newTestRuntime(character, testItems())
		drop := ownedGoldAtPlayer(t, rt, clock, character, 100099)
		result := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: drop.Gid}.Encode())
		assertOpcodes(t, result.Frames, wire.OpActionState, wire.OpItemMoveResponse)
		if rt.Ground.Count(testDivision) != 1 || character.Gold == nil || *character.Gold != 5000 {
			t.Fatalf("foreign pickup mutated state: ground=%d gold=%v", rt.Ground.Count(testDivision), character.Gold)
		}
	})

	t.Run("self owner grants", func(t *testing.T) {
		character := testCharacter()
		rt, clock := newTestRuntime(character, testItems())
		drop := ownedGoldAtPlayer(t, rt, clock, character, enterworld.ObjectIDForCharacter(character))
		result := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: drop.Gid}.Encode())
		assertOpcodes(t, result.Frames, wire.OpActionState, wire.OpPickupAnim,
			wire.OpItemMoveResponse, wire.OpPointsUpdate, wire.OpObjectDespawn)
	})

	t.Run("item-sharing party grants", func(t *testing.T) {
		character := testCharacter()
		rt, clock := newTestRuntime(character, testItems())
		drop := ownedGoldAtPlayer(t, rt, clock, character, 100099)
		rt.CanPickupOwnedDrop = func(divisionID, characterName string, ownerJID uint32) bool {
			return divisionID == testDivision && characterName == character.Name && ownerJID == 100099
		}
		result := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: drop.Gid}.Encode())
		assertOpcodes(t, result.Frames, wire.OpActionState, wire.OpPickupAnim,
			wire.OpItemMoveResponse, wire.OpPointsUpdate, wire.OpObjectDespawn)
	})
}

func TestGroundOwnershipReleasesAtNativeThirtySecondBoundaryOnce(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	drop := ownedGoldAtPlayer(t, rt, clock, character, enterworld.ObjectIDForCharacter(character))

	if frames := rt.TickHook()(clock.At(grounditem.OwnerLifetime - time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("owner released early: %+v", frames)
	}
	routed := rt.TickHook()(clock.At(grounditem.OwnerLifetime).UnixMilli())
	if len(routed) != 1 || routed[0].DivisionID != testDivision || len(routed[0].Frames) != 1 {
		t.Fatalf("owner release = %+v, want one division frame", routed)
	}
	frame := routed[0].Frames[0]
	if frame.Opcode != wire.OpGroundOwnershipExpired || len(frame.Payload) != 4 ||
		binary.LittleEndian.Uint32(frame.Payload) != drop.Gid {
		t.Fatalf("owner release frame = 0x%04X % X, want 0x31E2 {%d}", frame.Opcode, frame.Payload, drop.Gid)
	}
	stored, ok := rt.Ground.Get(testDivision, drop.Gid)
	if !ok || stored.OwnerJID != 0 {
		t.Fatalf("released ground row = %+v/%v, want public", stored, ok)
	}
	if again := rt.TickHook()(clock.At(grounditem.OwnerLifetime + time.Millisecond).UnixMilli()); len(again) != 0 {
		t.Fatalf("owner release replayed: %+v", again)
	}
}
