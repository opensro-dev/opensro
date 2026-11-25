/*
===========================================================================

mall_unique_only_test.go - unique-only goods (the Magic Pop card, the
Devil's Spirit) leave the mall (owner 2026-11-25, port-only, not native)

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestUniqueOnlyGoodsLeaveTheMall

The pop ticket withholds the card, the Devil's Spirit lucky drops
withhold every Spirit (both sexes and colours) and Sabakun's Jewel and
leave the extension gear listed;
with both off the mall keeps every good.
================
*/
func TestUniqueOnlyGoodsLeaveTheMall(t *testing.T) {
	items := testItems()
	items[gacha.CardCodename] = &enterworld.ItemRef{RefObjID: 900, Codename: gacha.CardCodename, TypeIDs: [4]int64{3, 3, 14, 1}}
	items["ITEM_MALL_AVATAR_M_NASRUN"] = &enterworld.ItemRef{RefObjID: 51676, Codename: "ITEM_MALL_AVATAR_M_NASRUN", TypeIDs: [4]int64{3, 1, 14, 1}}
	items["ITEM_MALL_AVATAR_W_NASRUN_BLUE"] = &enterworld.ItemRef{RefObjID: 51681, Codename: "ITEM_MALL_AVATAR_W_NASRUN_BLUE", TypeIDs: [4]int64{3, 1, 14, 1}}
	items[nasrunDropJewel] = &enterworld.ItemRef{RefObjID: 51690, Codename: nasrunDropJewel, TypeIDs: [4]int64{3, 3, 10, 1}}
	items["ITEM_MALL_NASRUN_EXTENSION"] = &enterworld.ItemRef{RefObjID: 51684, Codename: "ITEM_MALL_NASRUN_EXTENSION", TypeIDs: [4]int64{3, 3, 13, 16}}
	codes := func(mall *commerce.MallCatalog) map[string]bool {
		out := map[string]bool{}
		for _, offer := range mall.Offers {
			out[offer.Code] = true
		}
		return out
	}
	cases := []struct {
		popTicket bool
		nasrun    bool
		want      []string
	}{
		{true, false, []string{"SPIRIT_M", "SPIRIT_W_BLUE", "JEWEL", "EXTENSION", "POTION"}},
		{false, true, []string{"CARD", "EXTENSION", "POTION"}},
		{true, true, []string{"EXTENSION", "POTION"}},
		{false, false, []string{"CARD", "SPIRIT_M", "SPIRIT_W_BLUE", "JEWEL", "EXTENSION", "POTION"}},
	}
	for _, tc := range cases {
		rt, _ := newTestRuntime(testCharacter(), gmItemSource{items})
		rt.NasrunTokens = tc.nasrun
		rt.UniqueRules = progression.UniqueRules{PopTicket: tc.popTicket}
		mall := &commerce.MallCatalog{Offers: []commerce.MallPackage{
			{Code: "CARD", ItemIDs: []uint32{900}},
			{Code: "SPIRIT_M", ItemIDs: []uint32{51676}},
			{Code: "SPIRIT_W_BLUE", ItemIDs: []uint32{51681}},
			{Code: "JEWEL", ItemIDs: []uint32{51690}},
			{Code: "EXTENSION", ItemIDs: []uint32{51684}},
			{Code: "POTION", ItemIDs: []uint32{7}},
		}}
		rt.withholdUniqueOnlyGoods(mall)
		got := codes(mall)
		if len(got) != len(tc.want) {
			t.Fatalf("pop ticket %t, nasrun %t: kept %v, want %v", tc.popTicket, tc.nasrun, got, tc.want)
		}
		for _, code := range tc.want {
			if !got[code] {
				t.Fatalf("pop ticket %t, nasrun %t: %s withheld; kept %v", tc.popTicket, tc.nasrun, code, got)
			}
		}
	}
}
