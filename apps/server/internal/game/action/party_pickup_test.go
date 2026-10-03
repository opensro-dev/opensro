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
	"opensro.online/server/internal/game/world/monster"
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
	fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)[testDivision] = []*enterworld.Character{picker, peer}
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
goldRefreshesFor

The 0x30B3 type 1 frames one recipient received.
================
*/
func goldRefreshesFor(result OpResult, id int64) []wire.GoldRefresh {
	var out []wire.GoldRefresh
	for _, r := range result.Recipients {
		if r.CharacterID != id {
			continue
		}
		for _, frame := range r.Frames {
			if frame.Opcode == wire.OpPointsUpdate && frame.Payload[0] == wire.PointsTypeGold {
				refresh, err := wire.DecodeGoldRefresh(frame.Payload)
				if err == nil {
					out = append(out, refresh)
				}
			}
		}
	}
	return out
}

/*
================
TestSharedGoldSplitsEquallyAroundTheRotatedRecipient

CParty_DistributeGold: 101 between two members is 50 each, and the member
the rotation chose also takes the remainder. Every share is announced
(notify 1); the 0xFE receipt stays with the chosen recipient.
================
*/
func TestSharedGoldSplitsEquallyAroundTheRotatedRecipient(t *testing.T) {
	rt, _, picker, peer, _ := sharedPickupFixture(t)
	for index := 0; index < 2; index++ {
		before := [2]uint64{goldOf(picker), goldOf(peer)}
		heap := sharedGoldHeap(rt, picker, 0)
		result := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: heap.Gid}.Encode())
		if len(result.Broadcast) == 0 {
			t.Fatal("pickup refused")
		}
		gains := [2]uint64{goldOf(picker) - before[0], goldOf(peer) - before[1]}
		if gains[0]+gains[1] != 101 || (gains != [2]uint64{51, 50} && gains != [2]uint64{50, 51}) {
			t.Fatalf("pickup %d split %v, want 50 each plus the remainder to the recipient", index, gains)
		}
		recipient, other := peer, picker
		if gains[0] == 51 {
			recipient, other = picker, peer
		}
		if refreshes := goldRefreshesFor(result, other.ID); len(refreshes) != 1 || !refreshes[0].Notify ||
			refreshes[0].Balance != goldOf(other) {
			t.Fatalf("other member's share frame = %+v", refreshes)
		}
		if recipient == peer {
			for _, frame := range result.Frames {
				if frame.Opcode == wire.OpItemMoveResponse {
					t.Fatal("the recipient's 0xFE receipt reached the picker")
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
TestSoloGoldPickupRefreshesSilently

Without a split the 0xFE receipt announces the heap, so the balance refresh
carries notify 0 (4EAD12), never a second message.
================
*/
func TestSoloGoldPickupRefreshesSilently(t *testing.T) {
	rt, _, picker, _, registry := sharedPickupFixture(t)
	if _, refusal := registry.Leave(testDivision, picker.Name); refusal != "" {
		t.Fatal(refusal)
	}
	heap := sharedGoldHeap(rt, picker, 0)
	result := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: heap.Gid}.Encode())
	found := false
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpPointsUpdate && frame.Payload[0] == wire.PointsTypeGold {
			refresh, err := wire.DecodeGoldRefresh(frame.Payload)
			if err != nil || refresh.Notify || refresh.Balance != 5101 {
				t.Fatalf("solo refresh = %+v %v", refresh, err)
			}
			found = true
		}
	}
	if !found || len(result.Recipients) != 0 {
		t.Fatalf("solo pickup frames %+v recipients %+v", result.Frames, result.Recipients)
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
	delivered := map[int64]bool{}
	for _, d := range deliveries {
		delivered[d.OnlyCharacterID] = true
	}
	if !delivered[peer.ID] || !delivered[picker.ID] {
		t.Fatal("delayed pickup lost a share delivery", deliveries)
	}
	// The rotation chose the peer, who also takes the odd unit.
	if goldOf(picker) != 5050 || goldOf(peer) != 5051 {
		t.Fatalf("delayed split %d / %d", goldOf(picker), goldOf(peer))
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

/*
================
TestSharedItemSkipsADeadMember

The item rotation hands a pickup only to a living member (life byte 1,
525FAE); a dead member's turn passes and the picker keeps the item.
================
*/
func TestSharedItemSkipsADeadMember(t *testing.T) {
	rt, _, picker, peer, registry := sharedPickupFixture(t)
	registry.NextLootMember(testDivision, picker.Name)
	zero := int64(0)
	peer.CurrentHP = &zero
	potion := grounditem.Item{Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1}
	if got := rt.partyPickupRecipient(testDivision, picker, potion, rt.Now().UnixMilli()); got != picker {
		t.Fatalf("a dead member (%s) received the shared item", got.Name)
	}
}

/*
================
TestPartyMonsterIsHighNibbleOne

CGObjMob_IsPartyMonster (4C0DD0): (rarity & 0xF0) == 0x10, nothing wider.
================
*/
func TestPartyMonsterIsHighNibbleOne(t *testing.T) {
	for rarity, want := range map[uint8]bool{0x00: false, 0x10: true, 0x13: true, 0x20: false, 0x30: false} {
		// The party nibble arrives through the nest's per-instance rarity.
		instance := monster.Instance{}
		instance.Nest.HasRarityOverride, instance.Nest.RarityOverride = true, rarity
		if got := isPartyMonster(instance); got != want {
			t.Errorf("rarity %#x: party monster %v, want %v", rarity, got, want)
		}
	}
}
