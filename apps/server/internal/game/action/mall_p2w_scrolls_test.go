/*
===========================================================================

mall_p2w_scrolls_test.go - the pay-to-win scrolls leave the mall (owner
2026-11-28, port-only, not native)

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
TestP2WScrollsLeaveTheMall

With the switch on, the damage scroll's single and its pack of eleven
and the instant return scroll leave; the EXP booster and the reverse
return scroll stay. With the switch off the mall keeps every good.
================
*/
func TestP2WScrollsLeaveTheMall(t *testing.T) {
	items := testItems()
	for id, codename := range map[uint32]string{
		61001: "ITEM_MALL_DAMAGE_INC_10P_SCROLL",
		61002: "ITEM_MALL_RETURN_SCROLL_HIGH_SPEED",
		61003: "ITEM_MALL_REVERSE_RETURN_SCROLL",
		61004: "ITEM_MALL_100_EXP_SCROLL_1TIME",
	} {
		items[codename] = &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 3, 13, 1}}
	}
	for _, tc := range []struct {
		off  bool
		want []string
	}{
		{true, []string{"REVERSE_RETURN", "EXP_BOOSTER"}},
		{false, []string{"DAMAGE_INC_10P", "DAMAGE_INC_10P_11", "INSTANT_RETURN", "REVERSE_RETURN", "EXP_BOOSTER"}},
	} {
		rt, _ := newTestRuntime(testCharacter(), gmItemSource{items})
		rt.MallP2WScrollsOff = tc.off
		mall := &commerce.MallCatalog{Offers: []commerce.MallPackage{
			{Code: "DAMAGE_INC_10P", ItemIDs: []uint32{61001}},
			{Code: "DAMAGE_INC_10P_11", ItemIDs: []uint32{61001}},
			{Code: "INSTANT_RETURN", ItemIDs: []uint32{61002}},
			{Code: "REVERSE_RETURN", ItemIDs: []uint32{61003}},
			{Code: "EXP_BOOSTER", ItemIDs: []uint32{61004}},
		}}
		rt.withholdP2WScrolls(mall)
		if len(mall.Offers) != len(tc.want) {
			t.Fatalf("off %t: kept %+v, want %v", tc.off, mall.Offers, tc.want)
		}
		for i, code := range tc.want {
			if mall.Offers[i].Code != code {
				t.Fatalf("off %t: offer %d is %s, want %s", tc.off, i, mall.Offers[i].Code, code)
			}
		}
	}
}
