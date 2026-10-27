/*
===========================================================================

cosground_test.go - pet inventory ownership, pickup arrival and gold routing

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"reflect"
	"testing"
	"time"
)

/*
================
TestCOSGroundRoundTripAndOwnership

The native pet arrives at 2200; the port-only pacing policy slows it to 2500.
================
*/
func TestCOSGroundRoundTripAndOwnership(t *testing.T) {
	for _, paced := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "paced"}[paced], func(t *testing.T) {
			cosGroundRoundTrip(t, paced)
		})
	}
}

/*
================
cosGroundRoundTrip
================
*/
func cosGroundRoundTrip(t *testing.T, paced bool) {
	arrivalMs := int64(2200)
	if paced {
		arrivalMs = 2500
	}
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
	rt, _ := newTestRuntime(c, refs)
	rt.PetPolicies.Pacing = paced
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	row := c.MissionInventory[0]
	row.Slot = 0
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 2, Rows: []domain.InventoryRow{row}}}
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.BindPetSession(testDivision, c, 1)
	rt.TickHook()(1000)
	bag := c.Snapshot().MissionInventory
	send := func(q wire.ItemMoveRequest) OpResult {
		p, e := q.Encode()
		if e != nil {
			t.Fatal(e)
		}
		return rt.HandleItemMove(testDivision, c, p)
	}
	if r := send(wire.ItemMoveRequest{MovementType: wire.MoveTypeCosDrop, CosGID: gid + 1, SourceSlot: 0}); r.Frames[0].Payload[0] != 2 {
		t.Fatal("foreign drop admitted")
	}
	r := send(wire.ItemMoveRequest{MovementType: wire.MoveTypeCosDrop, CosGID: gid, SourceSlot: 0})
	if r.Frames[0].Payload[0] != 1 || len(c.ActiveCOS.Container.Rows) != 0 || len(rt.Ground.All(testDivision)) != 1 {
		t.Fatal("drop failed", r)
	}
	ground := rt.Ground.All(testDivision)[0]
	q := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: ground.Gid}
	r = send(q)
	if r.Frames[0].Payload[0] != 1 || len(rt.Ground.All(testDivision)) != 0 || !reflect.DeepEqual(c.ActiveCOS.Container.Rows, []domain.InventoryRow{row}) || !reflect.DeepEqual(c.MissionInventory, bag) {
		t.Fatal("roundtrip changed owner or item", r, c.ActiveCOS.Container.Rows)
	}
	if r = send(q); r.Frames[0].Payload[0] != 2 {
		t.Fatal("duplicate pickup granted")
	}
	// Drop again, move only the ground target, and require pet arrival rather
	// than borrowing the owner's position or granting on the request tick.
	send(wire.ItemMoveRequest{MovementType: wire.MoveTypeCosDrop, CosGID: gid, SourceSlot: 0})
	old := rt.Ground.All(testDivision)[0]
	rt.Ground.Remove(testDivision, old.Gid)
	old.Position.X += 100
	far := rt.Ground.Add(testDivision, old)
	q.GroundGID = far.Gid
	if r = send(q); len(r.Frames) != 0 || len(c.ActiveCOS.Container.Rows) != 0 {
		t.Fatal("far pickup did not wait")
	}
	rt.TickHook()(1100)
	if len(c.ActiveCOS.Container.Rows) != 0 {
		t.Fatal("pickup granted before arrival")
	}
	if paced {
		rt.TickHook()(2200)
		if len(c.ActiveCOS.Container.Rows) != 0 {
			t.Fatal("slower pet granted pickup before arrival")
		}
	}
	rt.TickHook()(arrivalMs)
	if len(c.ActiveCOS.Container.Rows) != 1 || len(rt.Ground.All(testDivision)) != 0 || !reflect.DeepEqual(c.MissionInventory, bag) {
		t.Fatal("arrival failed to grant into COS")
	}
	rt.Now = func() time.Time { return time.UnixMilli(arrivalMs) }
	pose := rt.PetPresentation(testDivision, c.Name).World.LiveSpawnAt(arrivalMs)
	heap := rt.Ground.Add(testDivision, grounditem.Item{GoldAmount: 50, Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, OwnerJID: 999})
	q.GroundGID = heap.Gid
	before := goldOf(c)
	if r = send(q); r.Frames[0].Payload[0] != 2 || goldOf(c) != before {
		t.Fatal("foreign reservation consumed")
	}
	rt.Ground.Remove(testDivision, heap.Gid)
	heap.OwnerJID = 0
	heap = rt.Ground.Add(testDivision, heap)
	q.GroundGID = heap.Gid
	c.ActiveCOS.Container.Capacity = 1
	if r = send(q); r.Frames[0].Payload[0] != 1 || r.Frames[0].Payload[6] != wire.PickupGoldSlot || goldOf(c) != before+50 || len(c.ActiveCOS.Container.Rows) != 1 {
		t.Fatal("gold pickup required an empty slot or corrupted bag", r)
	}
}

/*
================
TestCOSGroundQuestItemGrantsToOwnerPlayerBagAndUpdatesQuestProgress

The native pickup recipient choice: when a grab pet
picks up a non-alchemy quest item (the quest-item test: TypeID1=3,
TypeID2=3, TypeID3=9), the grant goes to the owner's player bag
(the ground pickup grant) and updates quest progress rather than
entering the pet container (0x11). Alchemy-prefix quest items still enter
the pet container when solo.
================
*/
func TestCOSGroundQuestItemGrantsToOwnerPlayerBagAndUpdatesQuestProgress(t *testing.T) {
	c := testCharacter()
	items := testItems()
	const questRefID uint32 = 45165
	const alchemyRefID uint32 = 45166
	questFlags := wire.PackTypeFlags(3, 3, 9, 0)
	items["ITEM_QNO_EU_CON_19_01"] = &enterworld.ItemRef{
		RefObjID: questRefID, Codename: "ITEM_QNO_EU_CON_19_01", TypeIDs: [4]int64{3, 3, 9, 0},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
	}
	items["ITEM_QNO_RM_ARCHEMY_01"] = &enterworld.ItemRef{
		RefObjID: alchemyRefID, Codename: "ITEM_QNO_RM_ARCHEMY_01", TypeIDs: [4]int64{3, 3, 9, 0},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
	}
	refs := testCosSource(items)
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
	rt, _ := newTestRuntime(c, refs)
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	questUpdates := 0
	questFrame := wire.Frame{Opcode: 0x31ED, Payload: []byte{0x02, 0x01}}
	rt.UpdateQuestInventory = func(ch *enterworld.Character) ([]wire.Frame, bool) {
		if ch == c {
			questUpdates++
		}
		return []wire.Frame{questFrame}, true
	}
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 8}}
	rt.BindPetSession(testDivision, c, 1)
	rt.TickHook()(1000)
	pose := rt.PetPresentation(testDivision, c.Name).World.Spawn
	playerBefore := len(c.MissionInventory)

	drop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: questRefID, Codename: "ITEM_QNO_EU_CON_19_01", TypeFlags: questFlags, StackCount: 1,
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y),
	})
	payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: drop.Gid}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	res := rt.HandleItemMove(testDivision, c, payload)
	if len(c.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("quest item entered pet container: %+v", c.ActiveCOS.Container.Rows)
	}
	if len(c.MissionInventory) != playerBefore+1 || c.MissionInventory[len(c.MissionInventory)-1].RefObjID != questRefID {
		t.Fatalf("quest item did not land in player bag: %+v", c.MissionInventory)
	}
	if questUpdates != 1 {
		t.Fatalf("quest inventory updates = %d, want 1", questUpdates)
	}
	sawPlayerGrant := false
	sawQuestFrame := false
	for _, f := range res.Frames {
		if f.Opcode == wire.OpItemMoveResponse {
			if len(f.Payload) < 2 || f.Payload[0] != 1 || f.Payload[1] != wire.MoveTypePickup {
				t.Fatalf("unexpected 0xB06D payload for pet-picked quest item: %x", f.Payload)
			}
			sawPlayerGrant = true
		}
		if f.Opcode == 0x31ED && reflect.DeepEqual(f.Payload, questFrame.Payload) {
			sawQuestFrame = true
		}
	}
	if !sawPlayerGrant || !sawQuestFrame {
		t.Fatalf("missing player grant (%v) or quest progress frame (%v) in %+v", sawPlayerGrant, sawQuestFrame, res.Frames)
	}

	// Alchemy-prefix quest items remain eligible for normal pet bag storage when solo.
	alchemyDrop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: alchemyRefID, Codename: "ITEM_QNO_RM_ARCHEMY_01", TypeFlags: questFlags, StackCount: 1,
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y),
	})
	payload, err = wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: gid, GroundGID: alchemyDrop.Gid}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	res = rt.HandleItemMove(testDivision, c, payload)
	if len(c.ActiveCOS.Container.Rows) != 1 || c.ActiveCOS.Container.Rows[0].RefObjID != alchemyRefID {
		t.Fatalf("alchemy quest item did not enter pet container: %+v", c.ActiveCOS.Container.Rows)
	}
	if res.Frames[0].Opcode != wire.OpItemMoveResponse || res.Frames[0].Payload[1] != wire.MoveTypeCosPickup {
		t.Fatalf("unexpected alchemy pickup response: %+v", res.Frames)
	}
}
