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
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
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
fillPlayerBag

Every bag slot of c occupied, so the share probe finds no free slot.
================
*/
func fillPlayerBag(t *testing.T, c *enterworld.Character) {
	t.Helper()
	occupied := map[int64]bool{}
	for _, row := range c.MissionInventory {
		occupied[row.Slot] = true
	}
	row := c.MissionInventory[0]
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagEnd(c)); slot++ {
		if occupied[slot] {
			continue
		}
		full := row
		full.Slot = slot
		c.MissionInventory = append(c.MissionInventory, full)
	}
	if _, free := bagOf(c).FirstFreeBagSlot(); free {
		t.Fatal("the filled bag still has a free slot")
	}
}

/*
================
TestGrabPetSpillWithoutRecipientPetAnswersBagFullClass

The rotation names a member whose bag is full and who has no pickup
pet: the spill can never land for this drop. Natively the selection
returns 0x1807 there and the answer tail remaps it to 0x18B4, the
bag-full class that pauses the pet; the retry byte instead made the
client re-propose the same drop every 3 s for its whole ground life.
Two proposals of the same drop pin that loop: neither may carry 0x9C,
and the drop is never consumed.
================
*/
func TestGrabPetSpillWithoutRecipientPetAnswersBagFullClass(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	fillPlayerBag(t, peer)
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1}
	drop := heap(potion)
	peerRows := len(peer.MissionInventory)
	for proposal := 1; proposal <= 2; proposal++ {
		rt.NextPartyLootMember(testDivision, picker.Name) // the peer is next
		result := rt.HandleCosCommand(testDivision, picker, wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(drop.Gid).Payload())
		assertCosPickupAck(t, result.Frames, gid, drop.Gid, false)
		if len(result.Frames) != 1 || result.Frames[0].Payload[2] != cosPickupBagFull {
			t.Fatalf("proposal %d answered %#x, want the bag-full class %#x (the retry byte re-proposes the drop every 3 s)",
				proposal, result.Frames[0].Payload[2], cosPickupBagFull)
		}
	}
	if len(rt.Ground.All(testDivision)) != 1 || len(peer.MissionInventory) != peerRows || len(picker.ActiveCOS.Container.Rows) != 0 {
		t.Fatal("a spill with no recipient pet consumed or granted the drop")
	}
}

/*
================
TestGrabPetSpillIntoFullRecipientPetKeepsTheRetry

The spill can still land later when the recipient's pickup pet exists
but its bag is full: natively the member gets 0x18B4 for their pet and
the picker's pet keeps the 3 s retry (0x189C).
================
*/
func TestGrabPetSpillIntoFullRecipientPetKeepsTheRetry(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	fillPlayerBag(t, peer)
	peerPetGID, _ := enterworld.CosObjectIDForCharacter(peer)
	fullRow := picker.MissionInventory[0]
	fullRow.Slot = 0
	peer.ActiveCOS = &enterworld.CharacterCOS{GID: peerPetGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 1, Rows: []domain.InventoryRow{fullRow}}}
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1}
	drop := heap(potion)
	rt.NextPartyLootMember(testDivision, picker.Name) // the peer is next
	result := rt.HandleCosCommand(testDivision, picker, wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(drop.Gid).Payload())
	assertCosPickupAck(t, result.Frames, gid, drop.Gid, false)
	if len(result.Frames) != 1 || result.Frames[0].Payload[2] != cosPickupRetryBackoff {
		t.Fatalf("picker answered %#x, want the retry byte %#x for a spill that can still land",
			result.Frames[0].Payload[2], cosPickupRetryBackoff)
	}
	told := false
	for _, r := range result.Recipients {
		if r.CharacterID != peer.ID {
			continue
		}
		for _, frame := range r.Frames {
			if frame.Opcode == wire.OpCosCommandResult && len(frame.Payload) == 11 && frame.Payload[2] == cosPickupBagFull &&
				binary.LittleEndian.Uint32(frame.Payload[3:]) == peerPetGID {
				told = true
			}
		}
	}
	if !told {
		t.Fatal("the recipient never got the bag-full answer for their pet")
	}
	if len(rt.Ground.All(testDivision)) != 1 {
		t.Fatal("the full recipient pet consumed the drop")
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

/*
================
TestGrabPetItemMoveIntoFullBagsAdvancesTheRotation

#3423: every member's player bag is full and the picker's pet has room;
the pet's item-move pickup (move type 0x11) proposes one drop again and
again. The reference server's item-move handler serves 0x11 as 6, and
its ground pickup rotates the loot member while it selects the
recipient, before the grant, so a refused attempt still spends the turn.
On the peer's turn the grant fails bag-full with no fallback: the peer
gets [2, 0x07], the picker [2, 0xDA], the drop stays. The next attempt is
the picker's own turn and lands in the pet's bag.
================
*/
func TestGrabPetItemMoveIntoFullBagsAdvancesTheRotation(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	fillPlayerBag(t, picker)
	fillPlayerBag(t, peer)
	peerRows := len(peer.MissionInventory)
	drop := heap(grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1})
	payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: drop.Gid}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	rt.NextPartyLootMember(testDivision, picker.Name) // the peer is next

	refused := rt.HandleItemMove(testDivision, picker, payload)
	if len(refused.Frames) != 1 || refused.Frames[0].Opcode != wire.OpItemMoveResponse ||
		!bytes.Equal(refused.Frames[0].Payload, []byte{2, wire.ErrCodePartyInventoryFull}) {
		t.Fatalf("picker frames on the peer's turn = %+v, want one 0xB06D [02 DA]", refused.Frames)
	}
	if len(refused.Recipients) != 1 || refused.Recipients[0].CharacterID != peer.ID || len(refused.Recipients[0].Frames) != 1 ||
		!bytes.Equal(refused.Recipients[0].Frames[0].Payload, []byte{2, wire.ErrCodeStorageFull}) {
		t.Fatalf("peer frames = %+v, want one 0xB06D [02 07]", refused.Recipients)
	}
	if len(rt.Ground.All(testDivision)) != 1 || len(picker.ActiveCOS.Container.Rows) != 0 {
		t.Fatal("the refused attempt consumed the drop or filled the pet")
	}

	rt.HandleItemMove(testDivision, picker, payload)
	if len(rt.Ground.All(testDivision)) != 0 || len(picker.ActiveCOS.Container.Rows) != 1 {
		t.Fatal("the refused attempt did not spend the peer's turn: the picker's own turn never came")
	}
	if len(peer.MissionInventory) != peerRows {
		t.Fatal("the full peer's bag changed")
	}
}
