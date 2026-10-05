/*
===========================================================================

premiumticket_test.go - 49C2B0 cases 3 and 4, the premium time tickets

===========================================================================
*/

package action

import (
	"testing"
	"time"

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
			"itemParam1_29c": 86400 * 3, "itemParam3_2a4": 10800000, "itemParam4_2a8": 100, "itemParam5_2ac": 100})}
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

/*
================
TestPremiumTicketBonusLastsTheDailyAllotment

The bonus holds for three online hours a day (Param3). Spent, it stops
until the next day, which refills the grant; a day left unspent carries
its remainder into the next one, spent first.
================
*/
func TestPremiumTicketBonusLastsTheDailyAllotment(t *testing.T) {
	c := testCharacter()
	ticket := &enterworld.ItemRef{RefObjID: 3700, Codename: "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_4W", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 4}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10,
			"itemParam1_29c": 86400 * 28, "itemParam3_2a4": 10800000, "itemParam4_2a8": 100, "itemParam5_2ac": 100})}
	items := testItems()
	items[ticket.Codename] = ticket
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: ticket.RefObjID,
		Codename: ticket.Codename, TypeFlags: ticket.TypeFlags(), StackCount: 1})
	rt, clock := newTestRuntime(c, items)
	rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(25).U16(ticket.TypeFlags()).Payload())
	award := func() int64 {
		exp, _ := paramJobRewardBonus(c, 1000, 0, clock.NowMs())
		return exp
	}
	play := func(d time.Duration) {
		for step := time.Duration(0); step < d; step += time.Minute {
			clock.Advance(time.Minute)
			rt.advanceParamJobs(clock.NowMs())
		}
	}
	if got := award(); got != 2000 {
		t.Fatalf("fresh ticket award = %d", got)
	}
	play(3*time.Hour + time.Minute)
	if got := award(); got != 1000 {
		t.Fatalf("after three online hours award = %d, want no bonus", got)
	}
	// Offline into the next day: the day's grant returns.
	rt.forgetCharacterLocked(testDivision, c.Name)
	clock.Advance(21 * time.Hour)
	rt.advanceParamJobs(clock.NowMs())
	if got := award(); got != 2000 {
		t.Fatalf("next day award = %d", got)
	}
	// Day 1 spends one hour; day 2 then holds a two-hour carry and three
	// fresh hours.
	play(time.Hour)
	rt.forgetCharacterLocked(testDivision, c.Name)
	clock.Advance(23 * time.Hour)
	rt.advanceParamJobs(clock.NowMs())
	if clock := c.PremiumClock; clock == nil || clock.CarriedMs != 2*3600000 || clock.TodayMs != 3*3600000 {
		t.Fatalf("day 2 clock = %+v", c.PremiumClock)
	}
	play(4 * time.Hour)
	if got := award(); got != 2000 {
		t.Fatalf("four hours into five, award = %d", got)
	}
	play(time.Hour + time.Minute)
	if got := award(); got != 1000 {
		t.Fatalf("carry and grant spent, award = %d", got)
	}
}
