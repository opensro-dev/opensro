/*
===========================================================================

mallextra_test.go - SRO_MALL_EXTRA_GOODS against the shipped reference data

"off" is the v1.150 mall; "on" lists the authored avatar outfits the mall
never sold, each at its own authored price, and a bad entry stops the boot.

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestMallExtraGoodsOffKeepsTheShippedMall
================
*/
func TestMallExtraGoodsOffKeepsTheShippedMall(t *testing.T) {
	for _, setting := range []string{"", "off"} {
		goods, err := ParseMallExtraGoods(setting)
		if err != nil || goods != nil {
			t.Fatalf("%q = %v, %v; want the shipped mall", setting, goods, err)
		}
	}
	catalog, err := LoadMall(gamedatatest.TextdataDir(t), nil)
	if err != nil || len(catalog.Offers) != 81 {
		t.Fatalf("shipped mall: %v offers, %v", len(catalog.Offers), err)
	}
}

/*
================
TestMallExtraGoodsListTheAvatarOutfits

Owner, 2026-10-13: fur, Halloween (with its attachment), pirate, holy knight
and the wing dresses, male and female. Each keeps its authored price: the
European wedding set's tier (dress 130, hat 30) and the attachment's 15.
================
*/
func TestMallExtraGoodsListTheAvatarOutfits(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	goods, err := ParseMallExtraGoods("on")
	if err != nil || len(goods) != 22 {
		t.Fatalf("on = %d goods, %v", len(goods), err)
	}
	catalog, err := LoadMall(dir, goods)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(enterworld.NewTextdataItems(dir)); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 81+22 {
		t.Fatalf("mall has %d offers, want 103", len(catalog.Offers))
	}
	byCode := map[string]MallPackage{}
	seen := map[MallAddress]bool{}
	for _, offer := range catalog.Offers {
		if seen[offer.MallAddress] {
			t.Fatalf("two offers at %+v", offer.MallAddress)
		}
		seen[offer.MallAddress] = true
		byCode[offer.Code] = offer
	}
	for _, good := range goods {
		offer, ok := byCode[good.Package]
		if !ok {
			t.Fatalf("%s not listed", good.Package)
		}
		want := uint32(130)
		switch {
		case strings.HasSuffix(good.Package, "_ATTACH"):
			want = 15
		case strings.HasSuffix(good.Package, "_HAT"):
			want = 30
		}
		if offer.Silk != want || offer.PackageID == 0 || len(offer.Scraps) == 0 || offer.Icon == "" {
			t.Fatalf("%s = silk %d (want %d), package %d, %d scraps, icon %q", good.Package, offer.Silk, want,
				offer.PackageID, len(offer.Scraps), offer.Icon)
		}
	}
	if byCode["PACKAGE_ITEM_MALL_AVATAR_M_EU_WEDDING"].Silk != 130 {
		t.Fatal("the shipped wedding dress changed")
	}
}

/*
================
TestMallExtraGoodsRefuseBadEntries

Each refusal names the setting: a typo never ships a different mall.
================
*/
func TestMallExtraGoodsRefuseBadEntries(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	for _, setting := range []string{
		"MALL_AVATAR_DRESS",
		"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_FUR:130",
		"STORE_CH_POTION_TAB1:PACKAGE_ITEM_MALL_AVATAR_M_FUR",
		"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_NOPE",
		"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_EU_WEDDING",
		"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_FUR,MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_FUR",
	} {
		goods, err := ParseMallExtraGoods(setting)
		if err == nil {
			_, err = LoadMall(dir, goods)
		}
		if err == nil || !strings.Contains(err.Error(), EnvMallExtraGoods) {
			t.Errorf("%q: err = %v, want a refusal naming %s", setting, err, EnvMallExtraGoods)
		}
	}
}
