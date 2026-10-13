/*
===========================================================================

transportbag_test.go - a summoned transport carries its cargo bag

Players with a transport out were told "Cannot obtain special products
without a summoned transport": a new transport record had no bag, so the
trade goods pickup and purchase found no cargo. Native creation
(CCOSDatabaseJob_InitializeOperation 4404A1..4404DA) keeps the reference
capacity for everything but pets.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// transportCargoSlots is the test reference's authored capacity.
const transportCargoSlots = 101

/*
================
summonedTradeTransport

A dressed trader who summons the transport through the real item use.
================
*/
func summonedTradeTransport(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	dressTrader(c)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3",
		TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1})
	refs := testCosSource(testItems())
	refs.characters["COS_T_DHORSE3"].InventoryCapacity = transportCargoSlots
	rt, _ := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 101)
	rt.HandleItemUse(testDivision, c, []byte{22, 0xEC, 0x11})
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned {
		t.Fatal("the transport was not summoned")
	}
	return rt, c
}

/*
================
TestSummonedTransportPicksTradeGoodsIntoItsCargo
================
*/
func TestSummonedTransportPicksTradeGoodsIntoItsCargo(t *testing.T) {
	rt, c := summonedTradeTransport(t)
	if c.ActiveCOS.Container == nil || c.ActiveCOS.Container.Capacity != transportCargoSlots {
		t.Fatalf("the new transport's bag is %+v, want %d slots", c.ActiveCOS.Container, transportCargoSlots)
	}
	out, _ := pickTradeGoods(t, rt, c, c.Job.Alias)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != wire.OpPickupAnim {
		t.Fatalf("the pickup was refused: %+v", out.Frames)
	}
	if rows := c.ActiveCOS.Container.Rows; len(rows) != 1 || rows[0].RefObjID != 2151 {
		t.Fatalf("transport cargo %+v", rows)
	}
}
