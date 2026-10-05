/*
===========================================================================

premiumticket_test.go - 49C2B0 cases 3 and 4, the premium time tickets

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestGoldTimeTicketRaisesBothPremiumKeepersOnce

A gold time ticket adds 100% EXP and skill EXP to every award for its
period; a second ticket while it runs answers 0x1894.
================
*/
func TestGoldTimeTicketRaisesBothPremiumKeepersOnce(t *testing.T) {
	c := testCharacter()
	ticket := &enterworld.ItemRef{RefObjID: 3700, Codename: "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_1D", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 4}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10,
			"itemParam1_29c": 86400, "itemParam4_2a8": 100, "itemParam5_2ac": 100})}
	items := testItems()
	items[ticket.Codename] = ticket
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: ticket.RefObjID,
		Codename: ticket.Codename, TypeFlags: ticket.TypeFlags(), StackCount: 2})
	rt, clock := newTestRuntime(c, items)
	use := wire.NewWriter(3).U8(25).U16(ticket.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("ticket = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	exp, skill := paramJobRewardBonus(c, 1000, 500, clock.NowMs())
	if exp != 2000 || skill != 1000 {
		t.Fatalf("award with the ticket: %d EXP %d skill EXP", exp, skill)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, errCodePremiumActive)
}
