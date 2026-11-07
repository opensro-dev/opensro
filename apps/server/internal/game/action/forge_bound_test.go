/*
===========================================================================

forge_bound_test.go - the bound doors of a forged item (forge.go)

Port-only (issue #1980): a bound forged item refuses every exit that
would hand it to another player or into the economy - the trade
table, a stall offer, a merchant sale, a guild warehouse deposit, a
player's ground drop - and on the ground only its forger may pick it
up, by hand or by pet. A bound pickup also lands as its own stack:
it never pours into an unbound bag row, nor unbound quantity into a
bound one. Each refusal is pinned with the door's own failure code,
and each door's unbound control proves the refusal is the bound mark,
not the item.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
boundSwordCharacter

A character holding the forge sword at bag slot 20, bound or not;
its reference trades and sells natively so only the mark can refuse.
================
*/
func boundSwordCharacter(bound bool) *enterworld.Character {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 20, RefObjID: 2001, Codename: forgeSwordCodename,
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 1), StackCount: 1, ForgeBound: bound,
	}}
	return c
}

/*
================
boundDoorItems
================
*/
func boundDoorItems() cosTestItemSource {
	items := forgeTestItems()
	items.staticItemSource[forgeSwordCodename].NativeFields = enterworld.NewNativeFields(map[string]float64{"canTrade": 1})
	return items
}

/*
================
TestBoundItemRefusedAtTradeStallAndGuildWarehouse
================
*/
func TestBoundItemRefusedAtTradeStallAndGuildWarehouse(t *testing.T) {
	for _, bound := range []bool{true, false} {
		c := boundSwordCharacter(bound)
		rt, _ := newTestRuntime(c, boundDoorItems())
		if _, code := rt.exchangeOfferable(c, 20); bound && code != wire.ExchangeErrCannotTrade || !bound && code != 0 {
			t.Fatalf("bound=%v exchange code = %#x", bound, code)
		}
		if _, code := rt.stallOfferable(testDivision, c, 20); bound && code != wire.StallErrInvalidOp || !bound && code != 0 {
			t.Fatalf("bound=%v stall code = %#x", bound, code)
		}
		res := rt.applyGuildStorageMove(testDivision, c, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGuildStorageDeposit, SourceSlot: 20,
		})
		want := wire.ErrCodeInvalidRequest
		if bound {
			want = errCodeInvalidStorageTarget
		}
		if len(res.Frames) != 1 || !bytes.Equal(res.Frames[0].Payload, []byte{2, want}) {
			t.Fatalf("bound=%v guild deposit = %+v, want refusal %#x", bound, res.Frames, want)
		}
	}
}

/*
================
TestBoundItemRefusedAtMerchantSale
================
*/
func TestBoundItemRefusedAtMerchantSale(t *testing.T) {
	for _, bound := range []bool{true, false} {
		rt, c := merchantFixture(t)
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 5, ForgeBound: bound,
		})
		before := goldOf(c)
		r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 1})
		if bound {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotDealAtShop}) {
				t.Fatalf("bound sale = %+v", r.Frames)
			}
			if goldOf(c) != before || bagCount(c, "ITEM_ETC_HP_POTION_01") == 0 {
				t.Fatal("the refused sale moved gold or items")
			}
			continue
		}
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 || goldOf(c) != before+15 {
			t.Fatalf("unbound sale = %+v gold %d", r.Frames, goldOf(c))
		}
	}
}

/*
================
TestBoundItemRefusedAtPlayerDrop
================
*/
func TestBoundItemRefusedAtPlayerDrop(t *testing.T) {
	for _, bound := range []bool{true, false} {
		c := boundSwordCharacter(bound)
		rt, _ := newTestRuntime(c, boundDoorItems())
		r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
		}))
		if bound {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotBeDropped}) {
				t.Fatalf("bound drop = %+v", r.Frames)
			}
			if bagCount(c, forgeSwordCodename) != 1 || rt.Ground.Count(testDivision) != 0 {
				t.Fatal("the refused drop moved the item")
			}
			continue
		}
		echo := false
		for _, frame := range r.Frames {
			if len(frame.Payload) > 0 && frame.Payload[0] == 1 {
				echo = true
			}
		}
		if !echo || bagCount(c, forgeSwordCodename) != 0 {
			t.Fatalf("unbound drop = %+v", r.Frames)
		}
		if all := rt.Ground.All(testDivision); len(all) != 1 || all[0].ForgeBound {
			t.Fatalf("unbound drop landed as %+v", all)
		}
	}
}

/*
================
boundPickupRuntime

A runtime holding the forging GM and a stranger in one division,
with a bound forged sword lying at their feet.
================
*/
func boundPickupRuntime(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, grounditem.Item) {
	t.Helper()
	gm := forgeTestGM()
	stranger := testCharacter()
	stranger.ID, stranger.Name, stranger.MissionInventory = 4, "Stranger", nil
	items := forgeTestItems()
	deps := &enterworld.Deps{
		Characters:   enterworld.StaticCharacterSource{testDivision: {gm, stranger}},
		Items:        items,
		MagicOptions: forgeTestSource(),
		NpcSpawns:    enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	sword := items.staticItemSource[forgeSwordCodename]
	spawn := simulation.SeedWorldState(gm).Spawn
	drop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(),
		Plus: 5, ForgeBound: true, DroppedBy: gm.Name,
		Position: grounditem.Point{RegionID: spawn.RegionID, X: float32(spawn.X), Z: float32(spawn.Z)},
		Y:        float32(spawn.Y),
	})
	return rt, gm, stranger, drop
}

/*
================
TestBoundDropPickupBelongsToItsForger
================
*/
func TestBoundDropPickupBelongsToItsForger(t *testing.T) {
	rt, gm, stranger, drop := boundPickupRuntime(t)
	res := rt.HandleTargetInteract(testDivision, stranger, wire.TargetInteract{Gid: drop.Gid}.Encode())
	if len(res.Frames) == 0 || !bytes.Equal(res.Frames[0].Payload, []byte{2, wire.ErrCodeLockedByOther}) {
		t.Fatalf("stranger pickup = %+v", res.Frames)
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("the stranger's pickup consumed the drop")
	}
	res = rt.HandleTargetInteract(testDivision, gm, wire.TargetInteract{Gid: drop.Gid}.Encode())
	if len(res.Frames) == 0 {
		t.Fatal("forger pickup answered nothing")
	}
	for _, frame := range res.Frames {
		if frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) > 0 && frame.Payload[0] == 2 {
			t.Fatalf("forger pickup refused: %+v", res.Frames)
		}
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the forger's pickup left the drop")
	}
	found := false
	for _, row := range gm.MissionInventory {
		if row.Codename == forgeSwordCodename {
			found = row.ForgeBound && row.Plus == 5
		}
	}
	if !found {
		t.Fatalf("the forger's bag holds %+v", gm.MissionInventory)
	}
}

/*
================
boundBagPotionStacks

The potion quantities in a character's bag, split by the bound mark,
and how many potion rows carry them.
================
*/
func boundBagPotionStacks(c *enterworld.Character) (boundQty, unboundQty, rows int) {
	for _, row := range c.MissionInventory {
		if row.Codename != forgePotionCodename {
			continue
		}
		rows++
		if row.ForgeBound {
			boundQty += int(row.StackCount)
		} else {
			unboundQty += int(row.StackCount)
		}
	}
	return boundQty, unboundQty, rows
}

/*
================
TestBoundPickupNeverMergesIntoAnUnboundStack

The #2775 review's laundering probe, end to end: a bag holding an
unbound potion stack picks up a bound forged stack (and the mirror
image), and the two stay separate rows under their own marks.
================
*/
func TestBoundPickupNeverMergesIntoAnUnboundStack(t *testing.T) {
	seed := func(bound bool) *enterworld.Character {
		gm := forgeTestGM()
		gm.MissionInventory = []enterworld.InventoryRow{{
			Slot: 13, RefObjID: 3630, Codename: forgePotionCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 10, ForgeBound: bound,
		}}
		return gm
	}
	pickup := func(t *testing.T, rt *Runtime, gm *enterworld.Character, gid uint32) {
		t.Helper()
		res := rt.HandleTargetInteract(testDivision, gm, wire.TargetInteract{Gid: gid}.Encode())
		for _, frame := range res.Frames {
			if frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) > 0 && frame.Payload[0] == 2 {
				t.Fatalf("pickup refused: %+v", res.Frames)
			}
		}
	}

	gm := seed(false)
	rt, _ := forgeTestRuntime(gm, forgeTestItems())
	if _, ok := rt.ForgeGMItem(testDivision, gm.Name, forgePotionCodename+" count=25"); !ok {
		t.Fatal("forge refused")
	}
	pickup(t, rt, gm, forgedDrop(t, rt).Gid)
	if boundQty, unboundQty, rows := boundBagPotionStacks(gm); boundQty != 25 || unboundQty != 10 || rows != 2 {
		t.Fatalf("bound pickup poured into the unbound stack: bound %d, unbound %d, rows %d (%+v)",
			boundQty, unboundQty, rows, gm.MissionInventory)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the pickup left part of the stack on the ground")
	}

	gm = seed(true)
	rt, _ = forgeTestRuntime(gm, forgeTestItems())
	spawn := simulation.SeedWorldState(gm).Spawn
	drop := rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: 3630, Codename: forgePotionCodename, TypeFlags: wire.PackTypeFlags(3, 3, 1, 1),
		StackCount: 25, DroppedBy: gm.Name,
		Position: grounditem.Point{RegionID: spawn.RegionID, X: float32(spawn.X), Z: float32(spawn.Z)},
		Y:        float32(spawn.Y),
	})
	pickup(t, rt, gm, drop.Gid)
	if boundQty, unboundQty, rows := boundBagPotionStacks(gm); boundQty != 10 || unboundQty != 25 || rows != 2 {
		t.Fatalf("unbound pickup poured into the bound stack: bound %d, unbound %d, rows %d (%+v)",
			boundQty, unboundQty, rows, gm.MissionInventory)
	}
}

/*
================
TestBoundDropPetPickupBelongsToItsForger

The pet lane: a pickup pet fetches a bound drop only for the
character that forged it; a stranger's pet is refused the same drop.
================
*/
func TestBoundDropPetPickupBelongsToItsForger(t *testing.T) {
	for _, own := range []bool{true, false} {
		c := testCharacter()
		refs := forgeTestItems()
		refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
		rt, _ := newTestRuntime(c, refs)
		rt.Now = func() time.Time { return time.UnixMilli(1000) }
		gid, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
			Container: &domain.COSContainer{Capacity: 2}}
		rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
		rt.BindPetSession(testDivision, c, 1)
		rt.TickHook()(1000)
		pose := rt.PetPresentation(testDivision, c.Name).World.LiveSpawnAt(1000)
		droppedBy := "AnotherGM"
		if own {
			droppedBy = c.Name
		}
		sword := refs.staticItemSource[forgeSwordCodename]
		item := rt.Ground.Add(testDivision, grounditem.Item{
			RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(),
			ForgeBound: true, DroppedBy: droppedBy, StackCount: 1,
			Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)},
		})
		body := wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload()
		rt.HandleCosCommand(testDivision, c, body)
		if own {
			if len(rt.Ground.All(testDivision)) != 0 || len(c.ActiveCOS.Container.Rows) != 1 ||
				!c.ActiveCOS.Container.Rows[0].ForgeBound {
				t.Fatalf("own pet pickup: ground %v rows %+v", rt.Ground.All(testDivision), c.ActiveCOS.Container.Rows)
			}
			continue
		}
		if len(rt.Ground.All(testDivision)) != 1 || len(c.ActiveCOS.Container.Rows) != 0 {
			t.Fatalf("stranger pet pickup: ground %v rows %+v", rt.Ground.All(testDivision), c.ActiveCOS.Container.Rows)
		}
	}
}
