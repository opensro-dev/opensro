/*
===========================================================================

itemmall_earned_test.go - what paid decides a mall item's tradability (#4133)

Every shipped mall row is record-bound: a probe over the generated
catalogue (the #4217 review, 81 offers / 81 distinct contents) found
canTrade 0 on every one, so the reference row alone can never admit a
mall item at a trade door. What paid has to ride the instance: the
delivery stamps the earned mark and the seal, and the doors compose
them in the one predicate (Runtime.Tradeable). The fixture below sells
a record-bound avatar-shaped package and carries the delivered row
through all three doors the decision owns - the exchange, the stall
(which offers through the exchange), and the market admission
(public_market.go wires Runtime.Tradeable itself, called here exactly
as wired).

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

// mallEarnedAvatar is the fixture content's codename: one shipped
// mall row's shape (record-bound, single, worn).
const mallEarnedAvatar = "ITEM_MALL_TEST_AVATAR"

/*
================
mallEarnedFixture

A mall selling one silk-only package that delivers a record-bound
avatar row, a buyer with the given wallet, and the tradable rule in
the given state.
================
*/
func mallEarnedFixture(balance domain.MallBalance, tradable bool) (*Runtime, *enterworld.Character, *mallTestAuthority, wire.MallPurchase) {
	c := testCharacter()
	refs := testItems()
	refs[mallEarnedAvatar] = &enterworld.ItemRef{
		RefObjID: 40001, Codename: mallEarnedAvatar, TypeIDs: [4]int64{3, 1, 13, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canTrade": 0}),
	}
	rt, _ := newTestRuntime(c, refs)
	authority := &mallTestAuthority{balance: balance}
	rt.mallAuthority = authority
	rt.MallEarnedTradable = tradable
	rt.mallCatalog = &commerce.MallCatalog{Offers: []commerce.MallPackage{{MallAddress: commerce.MallAddress{Group: 852, Shop: 2, Tab: 3, Slot: 4}, PackageID: 100, CurrencyMask: 22, Silk: 30, PurchaseLimit: 5, Contents: []commerce.Content{{Ref: refs[mallEarnedAvatar], Stack: 1}}}}}
	q := wire.MallPurchase{Group: 852, Shop: 2, Tab: 3, Slot: 4, Package: 100, Quantity: 1}
	return rt, c, authority, q
}

/*
================
boughtRow

The delivered row: its slot, seal and earned mark.
================
*/
func boughtRow(t *testing.T, c *enterworld.Character) domain.InventoryRow {
	t.Helper()
	for _, row := range c.MissionInventory {
		if row.Codename == mallEarnedAvatar {
			return row
		}
	}
	t.Fatalf("no mall item delivered: %+v", c.MissionInventory)
	return domain.InventoryRow{}
}

/*
================
TestMallPurchaseTradabilityFollowsWhatPaid
================
*/
func TestMallPurchaseTradabilityFollowsWhatPaid(t *testing.T) {
	for _, tc := range []struct {
		name       string
		balance    domain.MallBalance
		tradable   bool
		bound      bool
		earnedMark bool
	}{
		{"earned pays, rule on", domain.MallBalance{Silk: 100, EarnedSilk: 50}, true, false, true},
		{"earned runs out, rule on", domain.MallBalance{Silk: 100, EarnedSilk: 10}, true, true, false},
		{"only granted, rule on", domain.MallBalance{Silk: 100}, true, true, false},
		{"earned pays, rule off", domain.MallBalance{Silk: 100, EarnedSilk: 50}, false, true, true},
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
			row := boughtRow(t, c)
			if row.ForgeBound != tc.bound || row.EarnedBought != tc.earnedMark {
				t.Fatalf("delivered seal=%v earned mark=%v, want %v/%v", row.ForgeBound, row.EarnedBought, tc.bound, tc.earnedMark)
			}
			// Every door the decision owns reads the same composition:
			// admitted exactly when the purchase came out tradable.
			wantAdmit := !tc.bound
			if _, code := rt.exchangeOfferable(c, uint8(row.Slot)); (code == 0) != wantAdmit {
				t.Fatalf("exchange code = %#x, admit want %v", code, wantAdmit)
			}
			if _, code := rt.stallOfferable(testDivision, c, uint8(row.Slot)); (code == 0) != wantAdmit {
				t.Fatalf("stall code = %#x, admit want %v", code, wantAdmit)
			}
			// The market's admission is this call (public_market.go).
			if admit := rt.Tradeable(row.Codename, row.ForgeBound, row.EarnedBought, row.Summon); admit != wantAdmit {
				t.Fatalf("market admit = %v, want %v", admit, wantAdmit)
			}
		})
	}
}
