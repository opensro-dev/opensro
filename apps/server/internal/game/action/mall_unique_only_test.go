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
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestUniqueOnlyGoodsLeaveTheMall

The pop ticket withholds the card, SRO_NASRUN withholds every Devil's
Spirit (both sexes and colours) and leaves the extension gear listed;
with both off the mall keeps every good.
================
*/
func TestUniqueOnlyGoodsLeaveTheMall(t *testing.T) {
	items := testItems()
	items[gacha.CardCodename] = &enterworld.ItemRef{RefObjID: 900, Codename: gacha.CardCodename, TypeIDs: [4]int64{3, 3, 14, 1}}
	items["ITEM_MALL_AVATAR_M_NASRUN"] = &enterworld.ItemRef{RefObjID: 51676, Codename: "ITEM_MALL_AVATAR_M_NASRUN", TypeIDs: [4]int64{3, 1, 14, 1}}
	items["ITEM_MALL_AVATAR_W_NASRUN_BLUE"] = &enterworld.ItemRef{RefObjID: 51681, Codename: "ITEM_MALL_AVATAR_W_NASRUN_BLUE", TypeIDs: [4]int64{3, 1, 14, 1}}
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
		nasrun    string
		want      []string
	}{
		{true, "off", []string{"SPIRIT_M", "SPIRIT_W_BLUE", "EXTENSION", "POTION"}},
		{false, "on", []string{"CARD", "EXTENSION", "POTION"}},
		{true, "on", []string{"EXTENSION", "POTION"}},
		{false, "off", []string{"CARD", "SPIRIT_M", "SPIRIT_W_BLUE", "EXTENSION", "POTION"}},
	}
	for _, tc := range cases {
		t.Setenv(nasrun.Env, tc.nasrun)
		rt, _ := newTestRuntime(testCharacter(), gmItemSource{items})
		rt.UniqueRules = progression.UniqueRules{PopTicket: tc.popTicket}
		mall := &commerce.MallCatalog{Offers: []commerce.MallPackage{
			{Code: "CARD", ItemIDs: []uint32{900}},
			{Code: "SPIRIT_M", ItemIDs: []uint32{51676}},
			{Code: "SPIRIT_W_BLUE", ItemIDs: []uint32{51681}},
			{Code: "EXTENSION", ItemIDs: []uint32{51684}},
			{Code: "POTION", ItemIDs: []uint32{7}},
		}}
		rt.withholdUniqueOnlyGoods(mall)
		got := codes(mall)
		if len(got) != len(tc.want) {
			t.Fatalf("pop ticket %t, nasrun %s: kept %v, want %v", tc.popTicket, tc.nasrun, got, tc.want)
		}
		for _, code := range tc.want {
			if !got[code] {
				t.Fatalf("pop ticket %t, nasrun %s: %s withheld; kept %v", tc.popTicket, tc.nasrun, code, got)
			}
		}
	}
}
