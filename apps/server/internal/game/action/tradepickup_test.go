/*
===========================================================================

tradepickup_test.go - who may pick trade goods up, and where they go

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pickTradeGoods

Lays a heap of goods owned by owner at the character's feet and picks it.
================
*/
func pickTradeGoods(t *testing.T, rt *Runtime, c *domain.Character, owner string) (OpResult, grounditem.Item) {
	t.Helper()
	spawn := simulation.SeedWorldState(c).Spawn
	heap := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 2151, Codename: "ITEM_ETC_TRADE_WC_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 8, 1), StackCount: 5, TradeOwner: owner,
		Position: grounditem.Point{RegionID: spawn.RegionID, X: float32(spawn.X), Z: float32(spawn.Z)}})
	return rt.grantPickup(testDivision, simulation.WorldKey(testDivision, c.Name), c, c.Snapshot(), heap, rt.handlerDoors()), heap
}

/*
================
TestTraderPicksOwnGoodsIntoTheTransport
================
*/
func TestTraderPicksOwnGoodsIntoTheTransport(t *testing.T) {
	rt, c := traderFixture(t, false)
	bag := len(c.MissionInventory)
	out, heap := pickTradeGoods(t, rt, c, "Trader")
	if len(out.Frames) != 5 || out.Frames[0].Opcode != wire.OpPickupAnim || out.Frames[1].Opcode != wire.OpItemMoveResponse ||
		out.Frames[2].Opcode != wire.OpObjectDespawn || out.Frames[3].Opcode != wire.OpActionState ||
		out.Frames[4].Opcode != wire.OpTradeStarCount || len(out.Frames[4].Payload) != 1 {
		t.Fatalf("frames %+v, want scoop, receipt, despawn, the action release and the trade level", out.Frames)
	}
	p := out.Frames[1].Payload
	if p[0] != 1 || p[1] != wire.MoveTypeCosPickup || uint32(p[2])|uint32(p[3])<<8|uint32(p[4])<<16|uint32(p[5])<<24 != c.ActiveCOS.GID {
		t.Fatalf("receipt % X, want a 0x11 pickup for the transport", p)
	}
	rows := c.ActiveCOS.Container.Rows
	if len(rows) != 1 || rows[0].RefObjID != 2151 || rows[0].StackCount != 5 || rows[0].TradeOwner != "Trader" {
		t.Fatalf("transport cargo %+v", rows)
	}
	if len(c.MissionInventory) != bag {
		t.Fatal("trade goods went to the bag")
	}
	if _, left := rt.Ground.Get(testDivision, heap.Gid); left {
		t.Fatal("the heap stayed on the ground")
	}
}

/*
================
TestTradeGoodsPickupFollowsTheJobRule
================
*/
func TestTradeGoodsPickupFollowsTheJobRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		thief bool
		owner string
		strip bool
		cart  bool
		// teleport is the picker's teleport mode (2: the Thief Den
		// scroll's free-moving cast).
		teleport uint8
		code     uint8
	}{
		{"a trader cannot take another trader's goods", false, "Other", false, true, 0, tradePickupErrNotBuyer},
		{"a thief cannot take the goods it bought", true, "Trader", false, true, 0, tradePickupErrOwnGoods},
		{"a thief takes another's goods", true, "Other", false, true, 0, 0},
		{"a thief scrolling to the den cannot", true, "Other", false, true, teleportModeFree, tradePickupErrTeleporting},
		{"a player out of job mode cannot", false, "Trader", true, true, 0, tradePickupErrNoJob},
		{"no transport, no goods", false, "Trader", false, false, 0, tradePickupErrNoCart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c := traderFixture(t, tc.thief)
			if tc.strip {
				c.MissionInventory = c.MissionInventory[1:]
			}
			if !tc.cart {
				c.ActiveCOS = nil
			}
			c.NativeTeleportMode = tc.teleport
			out, heap := pickTradeGoods(t, rt, c, tc.owner)
			_, left := rt.Ground.Get(testDivision, heap.Gid)
			if tc.code == 0 {
				if left || len(c.ActiveCOS.Container.Rows) != 1 {
					t.Fatalf("admitted pickup left the heap (%v) or cargo %+v", left, c.ActiveCOS.Container.Rows)
				}
				return
			}
			if want := pickupRefusal(tc.code).Frames; !reflect.DeepEqual(out.Frames, want) || !left {
				t.Fatalf("frames %+v (heap left %v), want refusal %#x", out.Frames, left, tc.code)
			}
		})
	}
}
