/*
===========================================================================

spiritmallcopy_test.go - a mall-bought Devil's Spirit at the merchant, the
warehouse and the ground (#4459)

Port-only, not native. Every Spirit row is imported bound (#3456:
CanSell, CanBorrow and CanDrop cleared), but a copy the Item Mall
delivered follows the later client's own row (nasrun.MallCopy): it is
stored, sold when earned Silk paid for it (#4133), and never dropped.
Each door is driven with the three deliveries the server makes: a token
or drop copy (no mark), a sealed mall copy and an earned-Silk mall copy.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	mallSpiritCodename = "ITEM_MALL_AVATAR_M_NASRUN"
	mallSpiritRefObjID = 51676
	mallSpiritSlot     = 25
)

// spiritDelivery is how the server handed a Spirit out: the marks the
// row carries.
type spiritDelivery struct {
	name         string
	forgeBound   bool
	earnedBought bool
}

// spiritDeliveries are the token or drop copy, the sealed mall copy and
// the earned-Silk mall copy (itemmall.go HandleMallPurchase).
var spiritDeliveries = []spiritDelivery{
	{name: "token"},
	{name: "sealed mall", forgeBound: true},
	{name: "earned mall", earnedBought: true},
}

/*
================
addSpiritRef

The Spirit's reference as the import leaves it: type 3/1/14/1, bound
(canTrade, canSell, canBorrow 0, no drop), with the later row's zero
sell price and keeping fee.
================
*/
func addSpiritRef(items staticItemSource) {
	items[mallSpiritCodename] = &enterworld.ItemRef{
		RefObjID: mallSpiritRefObjID, Codename: mallSpiritCodename, TypeIDs: [4]int64{3, 1, 14, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canTrade": 0, "canSell": 0, "canBorrow": 0, "sellPrice": 0, "keepingFee": 0,
		}),
	}
}

/*
================
spiritRow
================
*/
func spiritRow(d spiritDelivery) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: mallSpiritSlot, RefObjID: mallSpiritRefObjID, Codename: mallSpiritCodename,
		TypeFlags: wire.PackTypeFlags(3, 1, 14, 1), StackCount: 1,
		ForgeBound: d.forgeBound, EarnedBought: d.earnedBought,
	}
}

/*
================
TestSpiritMallCopySellsOnlyWhenEarned

A token copy and a sealed mall copy are refused with the merchant's
cannot-deal code and stay in the bag; an earned-Silk mall copy is
bought for its 0 sell price. The sale quote agrees with the commit.
================
*/
func TestSpiritMallCopySellsOnlyWhenEarned(t *testing.T) {
	for _, d := range spiritDeliveries {
		t.Run(d.name, func(t *testing.T) {
			rt, c := merchantFixture(t)
			addSpiritRef(rt.deps.ItemReferences().(staticItemSource))
			c.MissionInventory = append(c.MissionInventory, spiritRow(d))
			before := goldOf(c)
			quoted := false
			for _, quote := range rt.shopSaleQuotes(testDivision, c, rt.NpcRoster[0]) {
				quoted = quoted || quote.Slot == mallSpiritSlot
			}
			r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: mallSpiritSlot, Quantity: 1})
			if !d.earnedBought {
				if quoted {
					t.Fatal("the sale quote lists a Spirit the merchant refuses")
				}
				if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotDealAtShop}) {
					t.Fatalf("sale = %+v, want cannot deal", r.Frames)
				}
				if bagCount(c, mallSpiritCodename) != 1 || goldOf(c) != before {
					t.Fatal("the refused sale moved the Spirit or gold")
				}
				return
			}
			if !quoted {
				t.Fatal("the sale quote omits the earned mall Spirit")
			}
			if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
				t.Fatalf("sale = %+v, want success", r.Frames)
			}
			if bagCount(c, mallSpiritCodename) != 0 || goldOf(c) != before {
				t.Fatalf("sale left %d Spirits, gold %d (want 0 and %d)", bagCount(c, mallSpiritCodename), goldOf(c), before)
			}
		})
	}
}

/*
================
TestSpiritMallCopyEntersTheWarehouse

Both mall copies are deposited with their marks intact, so a withdrawn
copy keeps its own rules; a token copy is refused with the client's
invalid-target notice (1:0x43).
================
*/
func TestSpiritMallCopyEntersTheWarehouse(t *testing.T) {
	for _, d := range spiritDeliveries {
		t.Run(d.name, func(t *testing.T) {
			rt, c, authority := storageFixture(t)
			addSpiritRef(rt.deps.ItemReferences().(staticItemSource))
			c.MissionInventory = append(c.MissionInventory, spiritRow(d))
			r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageDeposit, SourceSlot: mallSpiritSlot, DestSlot: 0, NpcGID: 17})
			if !d.forgeBound && !d.earnedBought {
				if r.Frames[0].Payload[0] != 2 || r.Frames[0].Payload[1] != errCodeInvalidStorageTarget || len(authority.room.Rows) != 0 {
					t.Fatalf("token deposit = %v, room %+v", r.Frames[0].Payload, authority.room.Rows)
				}
				return
			}
			if r.Frames[0].Payload[0] != 1 || len(authority.room.Rows) != 1 || bagCount(c, mallSpiritCodename) != 0 {
				t.Fatalf("deposit = %v, room %+v", r.Frames[0].Payload, authority.room.Rows)
			}
			stored := authority.room.Rows[0]
			if stored.Codename != mallSpiritCodename || stored.ForgeBound != d.forgeBound || stored.EarnedBought != d.earnedBought {
				t.Fatalf("stored row = %+v, want the delivery's marks", stored)
			}
		})
	}
}

/*
================
TestSpiritNeverLeavesForTheGround

The later row's CanDrop is 0 for every Spirit, mall copy or not: each
drop is refused with the cannot-be-dropped code and nothing lands.
================
*/
func TestSpiritNeverLeavesForTheGround(t *testing.T) {
	for _, d := range spiritDeliveries {
		t.Run(d.name, func(t *testing.T) {
			c := testCharacter()
			c.MissionInventory = []enterworld.InventoryRow{spiritRow(d)}
			items := testItems()
			addSpiritRef(items)
			rt, _ := newTestRuntime(c, items)
			r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
				MovementType: wire.MoveTypeGroundDrop, SourceSlot: mallSpiritSlot,
			}))
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotBeDropped}) {
				t.Fatalf("drop = %+v, want cannot be dropped", r.Frames)
			}
			if bagCount(c, mallSpiritCodename) != 1 || rt.Ground.Count(testDivision) != 0 {
				t.Fatal("the refused drop moved the Spirit")
			}
		})
	}
}
