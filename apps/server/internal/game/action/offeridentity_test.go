/*
===========================================================================

offeridentity_test.go - an exchange or stall offer is the item as offered

An offered bag item can still change in place while the offer stands (an
alchemy result rewrites its plus or options without moving it). The
commit must then refuse: the other player agreed to the item as it was
shown, not to whatever now holds its slot.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
setRowPlus

Rewrites a bag row's plus in place, as an alchemy result would.
================
*/
func setRowPlus(c *enterworld.Character, slot, plus int64) {
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == slot {
			c.MissionInventory[i].Plus = plus
		}
	}
}

/*
================
TestExchangeRefusesAnOfferChangedInPlace
================
*/
func TestExchangeRefusesAnOfferChangedInPlace(t *testing.T) {
	rt, a, b, pushed := exchangeFixture(t)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	setRowPlus(a, 20, 7)
	if put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 20}); len(put.Frames) != 1 || put.Frames[0].Payload[0] != 1 {
		t.Fatalf("put answered %+v", put.Frames)
	}
	rt.HandleExchangeConfirm(testDivision, a, nil)
	rt.HandleExchangeConfirm(testDivision, b, nil)
	setRowPlus(a, 20, 0)
	rt.HandleExchangeApprove(testDivision, a, nil)
	rt.HandleExchangeApprove(testDivision, b, nil)
	lastFrame(t, pushed[b.Name], wire.OpExchangeFailed)
	if len(a.MissionInventory) != 1 || len(b.MissionInventory) != 0 {
		t.Fatalf("a changed offer still moved: %+v / %+v", a.MissionInventory, b.MissionInventory)
	}
}

/*
================
TestStallRefusesAnOfferChangedInPlace
================
*/
func TestStallRefusesAnOfferChangedInPlace(t *testing.T) {
	rt, owner, buyer, _ := stallFixture(t)
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Blades").Payload())
	setRowPlus(owner, 20, 7)
	add := stallEdit(wire.StallEditAdd, 0, 20, 1, 0, 0x2c, 1, 0, 0, 0, 0, 0, 0, 0)
	if out := rt.HandleStallEdit(testDivision, owner, add); len(out.Frames) != 1 || out.Frames[0].Payload[0] != 1 {
		t.Fatalf("add %+v", out.Frames)
	}
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	rt.HandleStallVisit(testDivision, buyer, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(owner)).Payload())
	setRowPlus(owner, 20, 0)
	if out := rt.HandleStallBuy(testDivision, buyer, []byte{0}); bytes.Equal(out.Frames[0].Payload, []byte{1, 0}) {
		t.Fatal("a changed offer sold")
	}
	if goldOf(buyer) != 5000 || len(buyer.MissionInventory) != 0 {
		t.Fatalf("buyer gold %d bag %+v", goldOf(buyer), buyer.MissionInventory)
	}
}
