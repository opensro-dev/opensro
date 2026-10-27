/*
===========================================================================

party_pickup_test.go - shared loot keeps economy and receipt ownership aligned

===========================================================================
*/
package action

import (
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/domain"

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
	return sharedPickupFixtureWith(t, testCharacter(), testItems())
}

/*
================
sharedPickupFixtureWith

The item-share party of picker and a peer, over source.
================
*/
func sharedPickupFixtureWith(t *testing.T, picker *enterworld.Character, source enterworld.ItemRefSource) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character, *party.Registry) {
	t.Helper()
	peer := picker.Snapshot()
	peer.ID, peer.Name = 4, "LootPeer"
	rt, clock := newTestRuntime(picker, source)
	fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)[testDivision] = []*enterworld.Character{picker, peer}
	registry := party.NewRegistry()
	_, refusal := registry.Form(testDivision, party.Member{MemberID: enterworld.ObjectIDForCharacter(picker), Name: picker.Name}, party.Member{MemberID: enterworld.ObjectIDForCharacter(peer), Name: peer.Name}, party.PartyOptionItemShare)
	if refusal != "" {
		t.Fatal(refusal)
	}
	rt.NextPartyLootMember = registry.NextLootMember
	rt.CanPickupOwnedDrop = registry.CanPickupOwnedDrop
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
itemSharePartyWith

An item-share party of c and the member ownerJID, for tests whose drop is
reserved to a party member the fixture does not otherwise build.
================
*/
func itemSharePartyWith(t *testing.T, c *enterworld.Character, ownerJID uint32) *party.Registry {
	t.Helper()
	registry := party.NewRegistry()
	if _, refusal := registry.Form(testDivision, party.Member{MemberID: enterworld.ObjectIDForCharacter(c), Name: c.Name},
		party.Member{MemberID: ownerJID, Name: "ShareOwner"}, party.PartyOptionItemShare); refusal != "" {
		t.Fatal(refusal)
	}
	return registry
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

The native party gold split: 101 between two members is 50 each, and the member
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
carries notify 0, never a second message.
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
as natively); a dead member's turn passes and the picker keeps the item.
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

The native party-monster test: (rarity & 0xF0) == 0x10, nothing wider.
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

/*
================
sharedPetFixture

The shared-loot party with a summoned pickup pet beside the picker, its
bag empty; heap drops one item where the pet stands.
================
*/
func sharedPetFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, uint32, func(grounditem.Item) grounditem.Item) {
	t.Helper()
	picker := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
	rt, _, _, peer, _ := sharedPickupFixtureWith(t, picker, refs)
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	gid, _ := enterworld.CosObjectIDForCharacter(picker)
	picker.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 4}}
	rt.BindPetSession(testDivision, picker, 1)
	rt.TickHook()(1000)
	pose := rt.PetPresentation(testDivision, picker.Name).World.Spawn
	heap := func(item grounditem.Item) grounditem.Item {
		item.Position = grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}
		item.Y = float32(pose.Y)
		return rt.Ground.Add(testDivision, item)
	}
	return rt, picker, peer, gid, heap
}

/*
================
TestPetPickupRunsThePartyItemShare

The native rotation runs item share for a pet's pickup (move type 0x11)
as for the owner's own: over two pickups the rotation hands one potion to
the peer's inventory and leaves the other in the pet's bag, and the pet's
gold pickup is split between the members.
================
*/
func TestPetPickupRunsThePartyItemShare(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	pick := func(item grounditem.Item) OpResult {
		payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: item.Gid}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		return rt.HandleItemMove(testDivision, picker, payload)
	}
	peerBag := len(peer.MissionInventory)
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1}
	for range 2 {
		pick(heap(potion))
	}
	if len(rt.Ground.All(testDivision)) != 0 {
		t.Fatal("a shared pickup left the item on the ground")
	}
	if got := len(peer.MissionInventory) - peerBag; got != 1 || len(picker.ActiveCOS.Container.Rows) != 1 {
		t.Fatalf("the peer gained %d items and the pet bag holds %d, want one each", got, len(picker.ActiveCOS.Container.Rows))
	}
	before := goldOf(picker) + goldOf(peer)
	pickerBefore, peerBefore := goldOf(picker), goldOf(peer)
	pick(heap(grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeFlags: wire.PackTypeFlags(3, 3, 5, 2), GoldAmount: 101}))
	if goldOf(picker)+goldOf(peer)-before != 101 || goldOf(picker) == pickerBefore || goldOf(peer) == peerBefore {
		t.Fatalf("the pet's gold was not split: %d -> %d, %d -> %d", pickerBefore, goldOf(picker), peerBefore, goldOf(peer))
	}
}

/*
================
TestPetPickupShareDeliversThePeerReceiptAfterTheApproach

A pickup pet walks to a drop beyond reach (grounditem.ExecuteRange) and
picks it up in its own tick. When the rotation hands the item to another
member, that member's inventory receipt must leave the pet tick: it was
dropped there, so the member owned the item on the server and never saw it.
================
*/
func TestPetPickupShareDeliversThePeerReceiptAfterTheApproach(t *testing.T) {
	rt, picker, peer, gid, _ := sharedPetFixture(t)
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.NextPartyLootMember(testDivision, picker.Name) // the peer is next
	pose := rt.PetPresentation(testDivision, picker.Name).World.LiveSpawnAt(1000)
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1, Y: float32(pose.Y),
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X) + 30, Z: float32(pose.Z)}}
	item := rt.Ground.Add(testDivision, potion)
	body := wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload()
	if result := rt.HandleCosCommand(testDivision, picker, body); len(result.Frames) != 0 {
		t.Fatal("the pet did not start its approach", result)
	}
	peerBag := len(peer.MissionInventory)
	delivered := false
	for now := int64(1100); now <= 6000 && len(rt.Ground.All(testDivision)) != 0; now += 100 {
		rt.Now = func() time.Time { return time.UnixMilli(now) }
		for _, batch := range rt.TickHook()(now) {
			if batch.OnlyCharacterID == peer.ID {
				delivered = true
			}
		}
	}
	if len(rt.Ground.All(testDivision)) != 0 {
		t.Fatal("the pet never picked the drop up")
	}
	if len(peer.MissionInventory)-peerBag != 1 {
		t.Fatalf("the rotation did not hand the drop to the peer: %d new rows", len(peer.MissionInventory)-peerBag)
	}
	if !delivered {
		t.Fatal("the peer's receipt never left the pet tick")
	}
}

/*
================
TestPetPickupShareKeepsThePetAdmissions

The share runs only after the pet path's own admissions. Another player's
reserved drop stays on the ground whatever the rotation names, and a
mounted pet takes nothing for the party.
================
*/
func TestPetPickupShareKeepsThePetAdmissions(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	pick := func(item grounditem.Item) {
		payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: item.Gid}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		rt.HandleItemMove(testDivision, picker, payload)
	}
	peerBag := len(peer.MissionInventory)
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1}
	const strangerJID = 0x7777777
	reserved := potion
	reserved.OwnerJID, reserved.DroppedAt = strangerJID, rt.Now()
	for range 2 {
		pick(heap(reserved))
	}
	gold := grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeFlags: wire.PackTypeFlags(3, 3, 5, 2), GoldAmount: 101,
		OwnerJID: strangerJID, DroppedAt: rt.Now()}
	pickerGold, peerGold := goldOf(picker), goldOf(peer)
	pick(heap(gold))
	if len(rt.Ground.All(testDivision)) != 3 || len(peer.MissionInventory) != peerBag || goldOf(picker) != pickerGold ||
		goldOf(peer) != peerGold || len(picker.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("the pet took a stranger's reserved drop: %d left on the ground", len(rt.Ground.All(testDivision)))
	}
	for _, item := range rt.Ground.All(testDivision) {
		rt.Ground.Remove(testDivision, item.Gid)
	}
	picker.ActiveCOS.Mounted = true
	for range 2 {
		pick(heap(potion))
	}
	if len(rt.Ground.All(testDivision)) != 2 || len(peer.MissionInventory) != peerBag {
		t.Fatalf("a mounted pet shared a pickup: %d left on the ground", len(rt.Ground.All(testDivision)))
	}
}

/*
================
partyLootNoticesFor

The 0x317D party loot notice payloads delivered to one party member.
================
*/
func partyLootNoticesFor(result OpResult, id int64) [][]byte {
	var out [][]byte
	for _, r := range result.Recipients {
		if r.CharacterID != id {
			continue
		}
		for _, frame := range r.Frames {
			if frame.Opcode == wire.OpPartyLootNotice {
				out = append(out, frame.Payload)
			}
		}
	}
	return out
}

/*
================
TestPetPickupEmitsPartyLootNoticeOnOwnerAndPeerTurns

Natively both the COS and the player ground pickups
broadcast the party loot notice after every
granted non-gold pickup in an item-share party, including when the rotation
selects the pet owner and the item lands in the pet bag (move type 0x11).
================
*/
func TestPetPickupEmitsPartyLootNoticeOnOwnerAndPeerTurns(t *testing.T) {
	rt, picker, peer, gid, heap := sharedPetFixture(t)
	pick := func(item grounditem.Item) OpResult {
		payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: item.Gid}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		return rt.HandleItemMove(testDivision, picker, payload)
	}
	potion := grounditem.Item{RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1}
	wantPicker := wire.EncodePartyLootNotice(enterworld.ObjectIDForCharacter(picker), potion.RefObjID, potion.TypeFlags, 1)
	wantPeer := wire.EncodePartyLootNotice(enterworld.ObjectIDForCharacter(peer), potion.RefObjID, potion.TypeFlags, 1)

	// Pet gold pickup must not advance the party item-share cursor, and
	// shared pet pickup frames must never include the player 0xB2CD latch.
	goldRes := pick(heap(grounditem.Item{GoldAmount: 40}))
	for _, f := range goldRes.Frames {
		if f.Opcode == wire.OpActionState {
			t.Fatalf("pet shared gold pickup emitted player OpActionState: %+v", goldRes.Frames)
		}
	}

	first := pick(heap(potion))
	second := pick(heap(potion))
	for _, f := range second.Frames {
		if f.Opcode == wire.OpActionState {
			t.Fatalf("pet shared item pickup emitted player OpActionState: %+v", second.Frames)
		}
	}
	for _, memberID := range []int64{picker.ID, peer.ID} {
		n1 := partyLootNoticesFor(first, memberID)
		n2 := partyLootNoticesFor(second, memberID)
		if len(n1) != 1 || len(n2) != 1 {
			t.Fatalf("member %d notices = %d then %d, want 1 on both owner and peer turns", memberID, len(n1), len(n2))
		}
		if string(n1[0]) != string(wantPicker) || string(n2[0]) != string(wantPeer) {
			t.Fatalf("member %d notice order = %x then %x, want picker (%x) then peer (%x)", memberID, n1[0], n2[0], wantPicker, wantPeer)
		}
	}
}

/*
================
TestSharedPlayerPickupFullRecipientBagRoutes07ToRecipientAndDAToPicker

The native player ground pickup: when the party rotation selects
another member whose player bag is full (0x1807), the recipient receives
0xB06D [2, 0x07] (ErrCodeStorageFull) and the picker receives 0xB06D [2, 0xDA]
(ErrCodePartyInventoryFull) plus the 0xB2CD release.
================
*/
func TestSharedPlayerPickupFullRecipientBagRoutes07ToRecipientAndDAToPicker(t *testing.T) {
	rt, _, picker, peer, registry := sharedPickupFixture(t)
	registry.NextLootMember(testDivision, picker.Name) // peer is next
	peer.MissionInventory = nil
	for slot := int64(13); slot < 45; slot++ {
		peer.MissionInventory = append(peer.MissionInventory, domain.InventoryRow{
			Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
		})
	}

	pose := rt.liveSpawn(simulation.WorldKey(testDivision, picker.Name), picker.Snapshot(), rt.Now().UnixMilli())
	drop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1,
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y),
	})
	res := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: drop.Gid}.Encode())
	if len(res.Broadcast) != 0 || len(rt.Ground.All(testDivision)) != 1 {
		t.Fatalf("full-bag shared pickup consumed drop: %+v", res)
	}
	assertOpcodes(t, res.Frames, wire.OpItemMoveResponse, wire.OpActionState)
	if got := res.Frames[0].Payload; len(got) != 2 || got[0] != 2 || got[1] != wire.ErrCodePartyInventoryFull {
		t.Fatalf("picker 0xB06D = %x, want [02 DA]", got)
	}
	if len(res.Recipients) != 1 || res.Recipients[0].CharacterID != peer.ID || len(res.Recipients[0].Frames) != 1 {
		t.Fatalf("peer recipient frames = %+v, want one 0xB06D [02 07]", res.Recipients)
	}
	if got := res.Recipients[0].Frames[0]; got.Opcode != wire.OpItemMoveResponse || len(got.Payload) != 2 ||
		got.Payload[0] != 2 || got.Payload[1] != wire.ErrCodeStorageFull {
		t.Fatalf("peer 0xB06D = %+v, want [02 07]", got)
	}
}

/*
================
TestPetPickupSharedPeerFullPlayerBagSpillsToPeerPetAndNotifiesBothWhenFull

The native COS pickup recipient choice and COS ground pickup:
 1. When the rotated party member's player bag is full and they have no pickup
    pet, the picker's owner receives 0xB69E [2, 8, 0x9C, pickerPet, item].
 2. When the rotated party member has a summoned pickup pet with free space,
    the grant spills into that member's pickup pet (0xB06D type 0x11 to the
    member, 0xB69E [1, 8, pickerPet, item] to the picker's owner, and 0x317D
    party loot notices).
 3. When both the rotated member's player bag and their pickup pet bag are
    full, the member receives 0xB69E [2, 8, 0xB4, peerPet, item] and the
    picker's owner receives 0xB69E [2, 8, 0x9C, pickerPet, item].

================
*/
func TestPetPickupSharedPeerFullPlayerBagSpillsToPeerPetAndNotifiesBothWhenFull(t *testing.T) {
	rt, picker, peer, pickerPetGID, heap := sharedPetFixture(t)
	occupied := domain.InventoryRow{
		Slot: 13, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
	}
	potion := grounditem.Item{
		RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1,
	}
	peer.MissionInventory = []domain.InventoryRow{{
		Slot: 13, RefObjID: potion.RefObjID, Codename: potion.Codename,
		TypeFlags: potion.TypeFlags, StackCount: 3,
	}}
	for slot := int64(14); slot < 45; slot++ {
		row := occupied
		row.Slot = slot
		peer.MissionInventory = append(peer.MissionInventory, row)
	}
	drop1 := heap(potion)
	cmd1 := wire.NewWriter(9).U32(pickerPetGID).U8(wire.CosCommandPickupTag).U32(drop1.Gid).Payload()

	// 1. Peer has no free bag slot (even with a below-cap potion stack at slot 13) and no pickup pet:
	// Natively the first-empty-slot probe from 0x0D returns 0xFF and refuses with 0x9C without merging.
	rt.NextPartyLootMember(testDivision, picker.Name)
	noPetRes := rt.HandleCosCommand(testDivision, picker, cmd1)
	assertCosPickupAck(t, noPetRes.Frames, pickerPetGID, drop1.Gid, false)
	if noPetRes.Frames[0].Payload[2] != cosPickupRetryBackoff || len(noPetRes.Recipients) != 0 || len(rt.Ground.All(testDivision)) != 1 {
		t.Fatalf("peer without pet full-bag result = %+v, want 0x9C to picker and no peer frame", noPetRes)
	}
	if peer.MissionInventory[0].StackCount != 3 {
		t.Fatalf("peer slot 13 stack = %d, want 3 (no merge when no empty slot exists, as natively)", peer.MissionInventory[0].StackCount)
	}

	// 2. Summon a 1-slot pickup pet for peer and rotate back to peer: drop1 spills to peer's pet without merging into slot 13.
	peerPetGID, _ := enterworld.CosObjectIDForCharacter(peer)
	peer.ActiveCOS = &enterworld.CharacterCOS{GID: peerPetGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 1}}
	rt.BindPetSession(testDivision, peer, 1)
	rt.NextPartyLootMember(testDivision, picker.Name) // advance past picker back to peer
	spillRes := rt.HandleCosCommand(testDivision, picker, cmd1)
	assertCosPickupAck(t, spillRes.Frames, pickerPetGID, drop1.Gid, true)
	if len(rt.Ground.All(testDivision)) != 0 || len(peer.ActiveCOS.Container.Rows) != 1 || peer.MissionInventory[0].StackCount != 3 {
		t.Fatalf("spillover into peer pet failed: ground=%d peerPetRows=%+v peerSlot13=%d", len(rt.Ground.All(testDivision)), peer.ActiveCOS.Container.Rows, peer.MissionInventory[0].StackCount)
	}
	sawPeerPetReceipt := false
	for _, r := range spillRes.Recipients {
		if r.CharacterID != peer.ID {
			continue
		}
		for _, f := range r.Frames {
			if f.Opcode == wire.OpItemMoveResponse && len(f.Payload) >= 6 && f.Payload[0] == 1 && f.Payload[1] == wire.MoveTypeCosPickup {
				sawPeerPetReceipt = true
			}
		}
	}
	if !sawPeerPetReceipt || len(partyLootNoticesFor(spillRes, picker.ID)) != 1 || len(partyLootNoticesFor(spillRes, peer.ID)) != 1 {
		t.Fatalf("missing peer 0x11 receipt (%v) or 0x317D notices in %+v", sawPeerPetReceipt, spillRes.Recipients)
	}

	// 3. Peer's player bag and peer's 1-slot pickup pet are both full (for a non-stacking item).
	swordDrop := heap(grounditem.Item{
		RefObjID: occupied.RefObjID, Codename: occupied.Codename, TypeFlags: occupied.TypeFlags, StackCount: 1,
	})
	cmd2 := wire.NewWriter(9).U32(pickerPetGID).U8(wire.CosCommandPickupTag).U32(swordDrop.Gid).Payload()
	rt.NextPartyLootMember(testDivision, picker.Name) // advance past picker back to peer
	bothFullRes := rt.HandleCosCommand(testDivision, picker, cmd2)
	assertCosPickupAck(t, bothFullRes.Frames, pickerPetGID, swordDrop.Gid, false)
	if bothFullRes.Frames[0].Payload[2] != cosPickupRetryBackoff || len(rt.Ground.All(testDivision)) != 1 {
		t.Fatalf("picker ack on dual-full = %+v, want 0x9C with drop preserved", bothFullRes)
	}
	if len(bothFullRes.Recipients) != 1 || bothFullRes.Recipients[0].CharacterID != peer.ID {
		t.Fatalf("dual-full recipients = %+v, want peer 0xB4 ack", bothFullRes.Recipients)
	}
	assertCosPickupAck(t, bothFullRes.Recipients[0].Frames, peerPetGID, swordDrop.Gid, false)
	if bothFullRes.Recipients[0].Frames[0].Payload[2] != cosPickupBagFull {
		t.Fatalf("peer pet ack code = 0x%02X, want 0xB4", bothFullRes.Recipients[0].Frames[0].Payload[2])
	}
}

/*
================
TestManualCosItemMoveKeepsB06DErrorCodesOnFullBags

The native item move request dispatches 0x706D move type 0x11 to
the player ground pickup, which uses 0xB06D error codes (0x07 /
0xDA) rather than the 0x769E command codes (0x9C / 0xB4 / 0xB5) or peer-pet
spillover from the COS pickup recipient choice.
================
*/
func TestManualCosItemMoveKeepsB06DErrorCodesOnFullBags(t *testing.T) {
	rt, picker, peer, pickerPetGID, heap := sharedPetFixture(t)
	occupied := domain.InventoryRow{
		Slot: 13, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
	}
	peer.MissionInventory = nil
	picker.MissionInventory = nil
	for slot := int64(13); slot < 45; slot++ {
		row := occupied
		row.Slot = slot
		peer.MissionInventory = append(peer.MissionInventory, row)
		picker.MissionInventory = append(picker.MissionInventory, row)
	}
	peerPetGID, _ := enterworld.CosObjectIDForCharacter(peer)
	peer.ActiveCOS = &enterworld.CharacterCOS{GID: peerPetGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 4}}
	rt.BindPetSession(testDivision, peer, 1)

	// 1. Manual 0x706D move type 0x11 when the rotated peer's player bag is full:
	// routes 0xB06D [2, 0x07] to peer and [2, 0xDA] to picker without spilling to peer's pet.
	swordDrop := heap(grounditem.Item{
		RefObjID: occupied.RefObjID, Codename: occupied.Codename, TypeFlags: occupied.TypeFlags, StackCount: 1,
	})
	rt.NextPartyLootMember(testDivision, picker.Name)
	req, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: pickerPetGID, GroundGID: swordDrop.Gid}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	res := rt.HandleItemMove(testDivision, picker, req)
	if len(res.Frames) != 1 || res.Frames[0].Opcode != wire.OpItemMoveResponse ||
		len(res.Frames[0].Payload) != 2 || res.Frames[0].Payload[0] != 2 || res.Frames[0].Payload[1] != wire.ErrCodePartyInventoryFull {
		t.Fatalf("picker manual 0x706D frames = %+v, want 0xB06D [02 DA]", res.Frames)
	}
	if len(res.Recipients) != 1 || res.Recipients[0].CharacterID != peer.ID || len(res.Recipients[0].Frames) != 1 ||
		res.Recipients[0].Frames[0].Opcode != wire.OpItemMoveResponse || len(res.Recipients[0].Frames[0].Payload) != 2 ||
		res.Recipients[0].Frames[0].Payload[0] != 2 || res.Recipients[0].Frames[0].Payload[1] != wire.ErrCodeStorageFull {
		t.Fatalf("peer manual 0x706D recipients = %+v, want 0xB06D [02 07]", res.Recipients)
	}
	if len(peer.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("manual 0x706D spilled into peer pet container: %+v", peer.ActiveCOS.Container.Rows)
	}

	// 2. Manual 0x706D move type 0x11 on a personal quest drop when owner's player bag is full:
	// returns 0xB06D [2, 0x07] rather than 0x9C/0xB5.
	questDrop := heap(grounditem.Item{
		RefObjID: 45165, Codename: "ITEM_QNO_EU_CON_19_01", TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: 1,
	})
	questReq, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: pickerPetGID, GroundGID: questDrop.Gid}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	questRes := rt.HandleItemMove(testDivision, picker, questReq)
	if len(questRes.Frames) != 1 || questRes.Frames[0].Opcode != wire.OpItemMoveResponse ||
		len(questRes.Frames[0].Payload) != 2 || questRes.Frames[0].Payload[0] != 2 || questRes.Frames[0].Payload[1] != wire.ErrCodeStorageFull {
		t.Fatalf("picker manual 0x706D quest full-bag frames = %+v, want 0xB06D [02 07]", questRes.Frames)
	}
}

/*
================
TestGrantPeerCosPickupRechecksReservationUnderRecipientUpdate

grantPeerCosPickup re-reads the ground item under the recipient update door
and must re-verify OwnerJID and TradeOwner before mutating the peer's pet bag.
================
*/
func TestGrantPeerCosPickupRechecksReservationUnderRecipientUpdate(t *testing.T) {
	rt, picker, peer, _, heap := sharedPetFixture(t)
	peerPetGID, _ := enterworld.CosObjectIDForCharacter(peer)
	peer.ActiveCOS = &enterworld.CharacterCOS{GID: peerPetGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 4}}
	rt.BindPetSession(testDivision, peer, 1)

	drop := heap(grounditem.Item{
		RefObjID: picker.MissionInventory[0].RefObjID, Codename: picker.MissionInventory[0].Codename,
		TypeFlags: picker.MissionInventory[0].TypeFlags, StackCount: 1,
		OwnerJID: 0x7777777, DroppedAt: rt.Now(),
	})
	res := rt.grantPeerCosPickup(testDivision, picker, peer, drop, wire.PickupAnim{Gid: peerPetGID}, petPickupAt{
		now: rt.Now(), command: true,
	})
	if len(res.Broadcast) != 0 || len(peer.ActiveCOS.Container.Rows) != 0 || len(rt.Ground.All(testDivision)) != 1 {
		t.Fatalf("grantPeerCosPickup bypassed OwnerJID recheck: res=%+v rows=%+v", res, peer.ActiveCOS.Container.Rows)
	}
}

/*
================
TestSharedPickupRecipientGetsTheReferenceBeforeTheGrant

#1612: when the item-share rotation awards a ground item to another member,
that member receives the 0xB06D [1][6][slot][item] grant without ever having
seen the drop, so it holds no reference row for the item. The browser has
no item table, and an unknown reference in the grant ends its session
("Unknown inventory reference"). The 0x000E reference must reach the
recipient before the grant, not with the loot notice after it.
================
*/
func TestSharedPickupRecipientGetsTheReferenceBeforeTheGrant(t *testing.T) {
	rt, _, picker, peer, registry := sharedPickupFixture(t)
	registry.NextLootMember(testDivision, picker.Name) // peer is next
	peer.MissionInventory = nil

	// A weapon, not a starter-seed potion: the seed is in every browser's
	// static references, so only loot outside it needs a row.
	pose := rt.liveSpawn(simulation.WorldKey(testDivision, picker.Name), picker.Snapshot(), rt.Now().UnixMilli())
	drop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1,
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y),
	})
	if slices.Contains(rt.StaticRefItemCodenames(), drop.Codename) {
		t.Fatalf("%s is a static reference; the test needs loot outside it", drop.Codename)
	}
	res := rt.HandleTargetInteract(testDivision, picker, wire.TargetInteract{Gid: drop.Gid}.Encode())

	assertRecipientReferenceLeads(t, res, peer.ID, drop.RefObjID, 6, 3)
}

/*
================
TestSharedPetPickupSpillGivesThePeerTheReferenceFirst

#1612, pet path: the native COS ground pickup spills a shared pickup into the
rotated member's pickup pet (0xB06D [1][0x11][pet gid][slot][item]). That
member may never have seen the drop either, so its reference row leads.
================
*/
func TestSharedPetPickupSpillGivesThePeerTheReferenceFirst(t *testing.T) {
	rt, picker, peer, pickerPetGID, heap := sharedPetFixture(t)
	peer.MissionInventory = nil
	for slot := int64(13); slot < 45; slot++ {
		peer.MissionInventory = append(peer.MissionInventory, domain.InventoryRow{
			Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
		})
	}
	peerPetGID, _ := enterworld.CosObjectIDForCharacter(peer)
	peer.ActiveCOS = &enterworld.CharacterCOS{GID: peerPetGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 1}}
	rt.BindPetSession(testDivision, peer, 1)
	drop := heap(grounditem.Item{RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1})
	if slices.Contains(rt.StaticRefItemCodenames(), drop.Codename) {
		t.Fatalf("%s is a static reference; the test needs loot outside it", drop.Codename)
	}
	rt.NextPartyLootMember(testDivision, picker.Name) // peer is next
	res := rt.HandleCosCommand(testDivision, picker,
		wire.NewWriter(9).U32(pickerPetGID).U8(wire.CosCommandPickupTag).U32(drop.Gid).Payload())
	assertRecipientReferenceLeads(t, res, peer.ID, drop.RefObjID, wire.MoveTypeCosPickup, 7)
}

/*
================
assertRecipientReferenceLeads

In everything recipient receives, in delivery order, the 0xB06D [1][move]
grant naming item (its refObjId at refAt) must follow a 0x000E reference
row for that item.
================
*/
func assertRecipientReferenceLeads(t *testing.T, res OpResult, recipient int64, item uint32, move uint8, refAt int) {
	t.Helper()
	var received []wire.Frame
	for _, r := range res.Recipients {
		if r.CharacterID == recipient {
			received = append(received, r.Frames...)
		}
	}
	referenced, granted := false, false
	for index, frame := range received {
		switch {
		case frame.Opcode == opCommerceItemReferences:
			var delta struct {
				Items []struct {
					ID uint32 `json:"refObjId"`
				} `json:"items"`
			}
			if err := json.Unmarshal(frame.Payload, &delta); err != nil {
				t.Fatalf("recipient frame %d: reference payload: %v", index, err)
			}
			for _, row := range delta.Items {
				referenced = referenced || row.ID == item
			}
		case frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) >= refAt+4 &&
			frame.Payload[0] == 1 && frame.Payload[1] == move:
			if ref := binary.LittleEndian.Uint32(frame.Payload[refAt:]); ref != item {
				t.Fatalf("recipient grant names item %d, want %d", ref, item)
			}
			if !referenced {
				t.Fatalf("recipient frame %d is the 0xB06D grant before a 0x000E reference row for item %d: %+v", index, item, received)
			}
			granted = true
		}
	}
	if !granted {
		t.Fatalf("recipient got no 0xB06D [1][0x%02X] grant: %+v", move, received)
	}
}
