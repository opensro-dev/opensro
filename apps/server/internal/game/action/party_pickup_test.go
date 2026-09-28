/*
===========================================================================

party_pickup_test.go - shared loot keeps economy and receipt ownership aligned

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sharedPickupFixture
================
*/
func sharedPickupFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character, *party.Registry) {
	t.Helper()
	picker := testCharacter()
	peer := picker.Snapshot()
	peer.ID, peer.Name = 4, "LootPeer"
	rt, clock := newTestRuntime(picker, testItems())
	rt.deps.(*enterworld.Deps).Characters.(enterworld.StaticCharacterSource)[testDivision] = []*enterworld.Character{picker, peer}
	registry := party.NewRegistry()
	_, refusal := registry.Form(testDivision, party.Member{MemberID: enterworld.ObjectIDForCharacter(picker), Name: picker.Name}, party.Member{MemberID: enterworld.ObjectIDForCharacter(peer), Name: peer.Name}, party.PartyOptionItemShare)
	if refusal != "" {
		t.Fatal(refusal)
	}
	rt.NextPartyLootMember = registry.NextLootMember
	rt.RewardParties = func(division string) []RewardParty {
		var result []RewardParty
		for _, row := range registry.RewardSnapshots(division) {
			p := RewardParty{Order: row.ObjectOrder, Options: row.OptionBits}
			for _, member := range row.Members {
				p.Members = append(p.Members, member.MemberID)
			}
			result = append(result, p)
		}
		return result
	}
	return rt, clock, picker, peer, registry
}

/*
================
sharedGoldHeap
================
*/
func sharedGoldHeap(rt *Runtime, c *enterworld.Character, offset float32) grounditem.Item {
	pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c.Snapshot(), rt.Now().UnixMilli())
	return rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeFlags: wire.PackTypeFlags(3, 3, 5, 2), GoldAmount: 101, Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X) + offset, Z: float32(pose.Z)}, Y: float32(pose.Y)})
}

/*
================
TestSharedGoldRotatesWholePilesAndPrivateReceipts
================
*/
func TestSharedGoldRotatesWholePilesAndPrivateReceipts(t *testing.T) {
	rt, _, picker, peer, _ := sharedPickupFixture(t)
	for index := 0; index < 2; index++ {
		heap := sharedGoldHeap(rt, picker, 0)
		result := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: heap.Gid}.Encode())
		if len(result.Broadcast) == 0 {
			t.Fatal("pickup refused")
		}
		if index == 1 {
			if len(result.Recipients) != 1 || result.Recipients[0].CharacterID != peer.ID {
				t.Fatal("missing recipient receipt", result.Recipients)
			}
			for _, frame := range result.Frames {
				if frame.Opcode == wire.OpGoldRefresh || frame.Opcode == wire.OpItemMoveResponse {
					t.Fatal("peer's gold leaked into picker's private receipt")
				}
			}
		}
		if _, found := rt.Ground.Get(testDivision, heap.Gid); found {
			t.Fatal("credited heap remains pickable")
		}
	}
	if goldOf(picker) != 5101 || goldOf(peer) != 5101 {
		t.Fatalf("balances %d / %d", goldOf(picker), goldOf(peer))
	}
}

/*
================
TestSharedGoldApproachDeliversRecipientAtCompletion
================
*/
func TestSharedGoldApproachDeliversRecipientAtCompletion(t *testing.T) {
	rt, clock, picker, peer, registry := sharedPickupFixture(t)
	registry.NextLootMember(testDivision, picker.Name)
	heap := sharedGoldHeap(rt, picker, 50)
	result := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: heap.Gid}.Encode())
	if result.Pending == nil {
		t.Fatal("expected approach")
	}
	clock.Advance(result.Pending.Eta)
	deliveries := rt.advancePendingPickups(clock.NowMs())
	if len(deliveries) != 1 || deliveries[0].OnlyCharacterID != peer.ID {
		t.Fatal("delayed pickup lost recipient delivery", deliveries)
	}
	if goldOf(picker) != 5000 || goldOf(peer) != 5101 {
		t.Fatal("wrong delayed recipient")
	}
}

/*
================
TestSharedGoldSkipsAnotherWorldAndOutOfRangeMembers
================
*/
func TestSharedGoldSkipsAnotherWorldAndOutOfRangeMembers(t *testing.T) {
	for _, differentWorld := range []bool{false, true} {
		rt, _, picker, peer, registry := sharedPickupFixture(t)
		registry.NextLootMember(testDivision, picker.Name)
		if differentWorld {
			packed := uint32(0x20001)
			peer.World = &enterworld.CharacterWorld{PackedInstance: &packed}
		} else {
			key := simulation.WorldKey(testDivision, peer.Name)
			rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(peer) }, func(world *simulation.WorldState) { world.Spawn.Y += sharedLootRange + 1 })
		}
		heap := sharedGoldHeap(rt, picker, 0)
		rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: heap.Gid}.Encode())
		if goldOf(picker) != 5101 || goldOf(peer) != 5000 {
			t.Fatal("ineligible member received shared gold")
		}
	}
}
