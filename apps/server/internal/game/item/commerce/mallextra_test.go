/*
===========================================================================

mallextra_test.go - SRO_MALL_EXTRA_GOODS against the shipped reference data

"off" is the v1.150 mall; "on" lists the authored avatar outfits the mall
never sold, each at its own authored price; "statrecall" authors the stat
point recall scroll's six tiers (#1609); and a bad entry stops the boot.

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestMallExtraGoodsOffKeepsTheShippedMall
================
*/
func TestMallExtraGoodsOffKeepsTheShippedMall(t *testing.T) {
	tier.Long(t, "the shipped mall is unchanged with extra goods off")
	for _, setting := range []string{"", "off"} {
		goods, err := ParseMallExtraGoods(setting)
		if err != nil || goods != nil {
			t.Fatalf("%q = %v, %v; want the shipped mall", setting, goods, err)
		}
	}
	catalog, err := LoadMall(gamedatatest.TextdataDir(t), nil, nil)
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
	tier.Long(t, "the shipped mall lists the avatar outfits with extra goods on")
	dir := gamedatatest.TextdataDir(t)
	goods, err := ParseMallExtraGoods("on")
	if err != nil || len(goods) != 22 {
		t.Fatalf("on = %d goods, %v", len(goods), err)
	}
	catalog, err := LoadMall(dir, goods, nil)
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
			_, err = LoadMall(dir, goods, enterworld.NewTextdataItems(dir))
		}
		if err == nil || !strings.Contains(err.Error(), EnvMallExtraGoods) {
			t.Errorf("%q: err = %v, want a refusal naming %s", setting, err, EnvMallExtraGoods)
		}
	}
}

// statRecallSilk is the "statrecall" preset's price per package (#1609).
var statRecallSilk = map[string]uint32{
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL":      30,
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL_50LV": 60,
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL_60LV": 90,
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL_70LV": 120,
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL_80LV": 160,
	"PACKAGE_ITEM_MALL_STATS_POINT_RECALL_81LV": 250,
}

/*
================
TestMallExtraGoodsStatRecallPreset

Both settings of the preset. "on" alone is today's mall with no recall
scroll; "on,statrecall" adds the six tiers, each an authored silk-only
package that sells exactly its own scroll, drawn and named as the scroll,
under a package ID past every shipped one.
================
*/
func TestMallExtraGoodsStatRecallPreset(t *testing.T) {
	tier.Long(t, "the statrecall preset authors the recall scrolls on the shipped mall")
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	shipped, err := LoadMall(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	maxShippedID := uint32(0)
	for _, offer := range shipped.Offers {
		if offer.PackageID > maxShippedID {
			maxShippedID = offer.PackageID
		}
	}
	for _, setting := range []string{"on", "on,statrecall"} {
		goods, err := ParseMallExtraGoods(setting)
		if err != nil {
			t.Fatalf("%q: %v", setting, err)
		}
		catalog, err := LoadMall(dir, goods, items)
		if err != nil {
			t.Fatalf("%q: %v", setting, err)
		}
		if err := catalog.Hydrate(items); err != nil {
			t.Fatalf("%q: %v", setting, err)
		}
		recalls := map[string]MallPackage{}
		for _, offer := range catalog.Offers {
			if _, ok := statRecallSilk[offer.Code]; ok {
				recalls[offer.Code] = offer
			}
		}
		want := 0
		if strings.Contains(setting, "statrecall") {
			want = len(statRecallSilk)
		}
		if StatRecallListed(goods) != (want > 0) {
			t.Fatalf("%q: StatRecallListed = %t", setting, !(want > 0))
		}
		if len(recalls) != want || len(catalog.Offers) != 81+22+want {
			t.Fatalf("%q: %d recall offers of %d, want %d of %d", setting, len(recalls), len(catalog.Offers),
				want, 81+22+want)
		}
		ids := map[uint32]bool{}
		for code, offer := range recalls {
			item := "ITEM" + strings.TrimPrefix(code, "PACKAGE_ITEM")
			ref, ok := items.ItemRefByCodename(item)
			if !ok {
				t.Fatalf("%s: no item %s", code, item)
			}
			if offer.Silk != statRecallSilk[code] || offer.GiftSilk != 0 || offer.AllowsPoints {
				t.Errorf("%s: silk %d, gift %d, points %t; want silk %d only", code, offer.Silk, offer.GiftSilk,
					offer.AllowsPoints, statRecallSilk[code])
			}
			if len(offer.ItemIDs) != 1 || offer.ItemIDs[0] != ref.RefObjID || len(offer.Scraps) != 1 {
				t.Errorf("%s: items %v, %d scraps; want only %s (%d)", code, offer.ItemIDs, len(offer.Scraps), item,
					ref.RefObjID)
			}
			if offer.Icon != ref.Icon {
				t.Errorf("%s: icon %q, want the scroll's %q", code, offer.Icon, ref.Icon)
			}
			if offer.PackageID <= maxShippedID || ids[offer.PackageID] {
				t.Errorf("%s: package ID %d is shipped (max %d) or repeated", code, offer.PackageID, maxShippedID)
			}
			ids[offer.PackageID] = true
		}
	}
}
