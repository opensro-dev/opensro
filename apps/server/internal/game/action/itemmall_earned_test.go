/*
===========================================================================

itemmall_earned_test.go - a mall purchase's bound flag (#4133)

What paid decides the delivered items' one bound flag: earned silk
alone (with the tradable rule on) delivers a tradable item; granted
silk anywhere in the price, or the rule off, delivers it bound. The
trade doors read the flag from there - the exchange and stall doors
stand for all of them (forge_bound_test.go pins each door itself).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
mallEarnedFixture

A mall selling one silk-only potion package, a buyer with the given
wallet, and the tradable rule in the given state.
================
*/
func mallEarnedFixture(balance domain.MallBalance, tradable bool) (*Runtime, *enterworld.Character, *mallTestAuthority, wire.MallPurchase) {
	c := testCharacter()
	refs := testItems()
	refs["ITEM_ETC_HP_POTION_01"].NativeFields = enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1, "canTrade": 1})
	rt, _ := newTestRuntime(c, refs)
	authority := &mallTestAuthority{balance: balance}
	rt.mallAuthority = authority
	rt.MallEarnedTradable = tradable
	rt.mallCatalog = &commerce.MallCatalog{Offers: []commerce.MallPackage{{MallAddress: commerce.MallAddress{Group: 852, Shop: 2, Tab: 3, Slot: 4}, PackageID: 100, CurrencyMask: 22, Silk: 30, PurchaseLimit: 5, Contents: []commerce.Content{{Ref: refs["ITEM_ETC_HP_POTION_01"], Stack: 50, Data: 10}}}}}
	q := wire.MallPurchase{Group: 852, Shop: 2, Tab: 3, Slot: 4, Package: 100, Quantity: 1}
	return rt, c, authority, q
}

/*
================
boughtPotion

The delivered potion row: its slot and bound flag.
================
*/
func boughtPotion(t *testing.T, c *enterworld.Character) (int64, bool) {
	t.Helper()
	for _, row := range c.MissionInventory {
		if row.Codename == "ITEM_ETC_HP_POTION_01" && row.Slot >= 13 {
			return row.Slot, row.ForgeBound
		}
	}
	t.Fatalf("no potion delivered: %+v", c.MissionInventory)
	return 0, false
}

/*
================
TestMallPurchaseBoundFlagFollowsWhatPaid
================
*/
func TestMallPurchaseBoundFlagFollowsWhatPaid(t *testing.T) {
	for _, tc := range []struct {
		name     string
		balance  domain.MallBalance
		tradable bool
		bound    bool
	}{
		{"earned pays, rule on", domain.MallBalance{Silk: 100, EarnedSilk: 50}, true, false},
		{"earned runs out, rule on", domain.MallBalance{Silk: 100, EarnedSilk: 10}, true, true},
		{"only granted, rule on", domain.MallBalance{Silk: 100}, true, true},
		{"earned pays, rule off", domain.MallBalance{Silk: 100, EarnedSilk: 50}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, _, q := mallEarnedFixture(tc.balance, tc.tradable)
			payload, err := q.Encode()
			if err != nil {
				t.Fatal(err)
			}
			result := rt.HandleItemMove(testDivision, c, payload)
			if len(result.Frames) == 0 || len(result.Frames[0].Payload) == 0 || result.Frames[0].Payload[0] == 2 {
				t.Fatalf("purchase refused: %+v", result.Frames)
			}
			slot, bound := boughtPotion(t, c)
			if bound != tc.bound {
				t.Fatalf("bound = %v, want %v", bound, tc.bound)
			}
			// The doors read the same flag: a bound buy is refused at
			// the trade table and the stall, a tradable buy is not.
			if _, code := rt.exchangeOfferable(c, uint8(slot)); tc.bound && code != wire.ExchangeErrCannotTrade || !tc.bound && code != 0 {
				t.Fatalf("exchange code = %#x for bound %v", code, tc.bound)
			}
			if _, code := rt.stallOfferable(testDivision, c, uint8(slot)); tc.bound && code != wire.StallErrInvalidOp || !tc.bound && code != 0 {
				t.Fatalf("stall code = %#x for bound %v", code, tc.bound)
			}
		})
	}
}
