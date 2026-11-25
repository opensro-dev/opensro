/*
===========================================================================

mall_unique_only_test.go - the Magic Pop card leaves the mall while
uniques guarantee it (owner 2026-11-25, port-only, not native)

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
TestPopTicketWithholdsTheMallCard

With SRO_UNIQUE_POP_TICKET on the card's package leaves the mall and
every other good stays; off, the native mall keeps selling it.
================
*/
func TestPopTicketWithholdsTheMallCard(t *testing.T) {
	items := testItems()
	items[gacha.CardCodename] = &enterworld.ItemRef{RefObjID: 900, Codename: gacha.CardCodename, TypeIDs: [4]int64{3, 3, 14, 1}}
	catalog := func() *commerce.MallCatalog {
		return &commerce.MallCatalog{Offers: []commerce.MallPackage{
			{Code: "PACKAGE_ITEM_MALL_GACHA_CARD", ItemIDs: []uint32{900}},
			{Code: "PACKAGE_POTION", ItemIDs: []uint32{7}},
		}}
	}
	for _, on := range []bool{true, false} {
		rt, _ := newTestRuntime(testCharacter(), gmItemSource{items})
		rt.UniqueRules = progression.UniqueRules{PopTicket: on}
		mall := catalog()
		rt.withholdUniqueOnlyGoods(mall)
		want := 2
		if on {
			want = 1
		}
		if len(mall.Offers) != want || mall.Offers[len(mall.Offers)-1].Code != "PACKAGE_POTION" {
			t.Fatalf("pop ticket %t: offers %+v, want %d with the potion kept", on, mall.Offers, want)
		}
	}
}
