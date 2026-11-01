/*
===========================================================================

grabpet_share_test.go - a grab pet shares the top damager's drops

A report said that in an item-share party the member who dealt the most
damage got every item and the other none. Drops are reserved to the top
damager (OwnerJID). These tests build that case with the ownership rule
the GameWorld wires (party Registry.CanPickupOwnedDrop: an item-share
party's member may take another member's drop) and pin the native rotation
(the native party loot rotation): the lower-damage member's pet picks the
top damager's drops and the items alternate between the two. Without item
share the reservation holds.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/party"
)

/*
================
topDamagerPetFixture

The picker's grab pet in a party with the peer, every drop reserved to the
peer (the top damager), and the production ownership rule.
================
*/
func topDamagerPetFixture(t *testing.T, options uint8) (*Runtime, *enterworld.Character, *enterworld.Character, func() OpResult) {
	t.Helper()
	picker := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
	rt, _, _, peer, _ := sharedPickupFixtureWith(t, picker, refs)
	if options != party.PartyOptionItemShare {
		registry := party.NewRegistry()
		if _, refusal := registry.Form(testDivision, party.Member{MemberID: enterworld.ObjectIDForCharacter(picker), Name: picker.Name},
			party.Member{MemberID: enterworld.ObjectIDForCharacter(peer), Name: peer.Name}, options); refusal != "" {
			t.Fatal(refusal)
		}
		rt.NextPartyLootMember = registry.NextLootMember
		rt.CanPickupOwnedDrop = registry.CanPickupOwnedDrop
	}
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	gid, _ := enterworld.CosObjectIDForCharacter(picker)
	picker.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 8}}
	rt.BindPetSession(testDivision, picker, 1)
	rt.TickHook()(1000)
	pose := rt.PetPresentation(testDivision, picker.Name).World.Spawn
	pick := func() OpResult {
		potion := picker.MissionInventory[0]
		item := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: potion.RefObjID, Codename: potion.Codename,
			TypeFlags: potion.TypeFlags, StackCount: 1, OwnerJID: enterworld.ObjectIDForCharacter(peer), DroppedAt: rt.Now(),
			Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y)})
		payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: item.Gid}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		return rt.HandleItemMove(testDivision, picker, payload)
	}
	return rt, picker, peer, pick
}

/*
================
TestGrabPetSharesTheTopDamagersDrops

Item share on: the lower-damage member's pet picks four of the top
damager's drops, and the rotation hands two to each member.
================
*/
func TestGrabPetSharesTheTopDamagersDrops(t *testing.T) {
	rt, picker, peer, pick := topDamagerPetFixture(t, party.PartyOptionItemShare)
	peerBag := len(peer.MissionInventory)
	for range 4 {
		pick()
	}
	if left := len(rt.Ground.All(testDivision)); left != 0 {
		t.Fatalf("%d of the top damager's drops stayed on the ground", left)
	}
	peerGot, petGot := len(peer.MissionInventory)-peerBag, len(picker.ActiveCOS.Container.Rows)
	if peerGot != 2 || petGot != 2 {
		t.Fatalf("the rotation gave the peer %d and the picker's pet %d of four drops, want two each", peerGot, petGot)
	}
}

/*
================
TestGrabPetLeavesTheTopDamagersDropsWithoutItemShare

Item share off: the top damager's reservation holds, so the other member's
pet takes nothing (the drop waits for its owner, or for the reservation to
expire).
================
*/
func TestGrabPetLeavesTheTopDamagersDropsWithoutItemShare(t *testing.T) {
	rt, picker, peer, pick := topDamagerPetFixture(t, party.PartyOptionExpShare)
	peerBag := len(peer.MissionInventory)
	for range 2 {
		pick()
	}
	if left := len(rt.Ground.All(testDivision)); left != 2 || len(peer.MissionInventory) != peerBag || len(picker.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("without item share the pet took a reserved drop: %d left on the ground", left)
	}
}
