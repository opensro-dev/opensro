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

/*
================
TestMallExtraGoodsCap110IsItsOwnWord

"cap110" adds the imported Cursed Soul set to whatever else is listed, and
"on" alone keeps the v1.150 outfits only.
================
*/
func TestMallExtraGoodsCap110IsItsOwnWord(t *testing.T) {
	only, err := ParseMallExtraGoods("cap110")
	if err != nil || len(only) != 10 {
		t.Fatalf("cap110 = %d goods, %v", len(only), err)
	}
	both, err := ParseMallExtraGoods("on, cap110")
	if err != nil || len(both) != 22+10 {
		t.Fatalf("on, cap110 = %d goods, %v", len(both), err)
	}
	plain, err := ParseMallExtraGoods("on")
	if err != nil || len(plain) != 22 {
		t.Fatalf("on = %d goods, %v", len(plain), err)
	}
	for _, good := range only {
		if !strings.Contains(good.Package, "PIRATECREW") {
			t.Errorf("cap110 lists %s", good.Package)
		}
		wantTab := "MALL_AVATAR_HAT"
		if !strings.HasSuffix(good.Package, "_HAT") && !strings.HasSuffix(good.Package, "_HAT_2") &&
			!strings.HasSuffix(good.Package, "_ATTACH") {
			wantTab = "MALL_AVATAR_DRESS"
		}
		if good.Tab != wantTab {
			t.Errorf("%s is on %s, want %s", good.Package, good.Tab, wantTab)
		}
	}
}

/*
================
TestMallExtraGoodsCap110NeedsTheImport

Without the imported package rows the boot stops naming the setting, so a
v1.150-only install never serves a half-listed set.
================
*/
func TestMallExtraGoodsCap110NeedsTheImport(t *testing.T) {
	tables := map[string][][]string{
		"refshoptab":     {{"1", "11", "0", "MALL_AVATAR_DRESS"}, {"1", "11", "1", "MALL_AVATAR_HAT"}},
		"refshopgoods":   {},
		"refpackageitem": {},
	}
	goods, err := ParseMallExtraGoods("cap110")
	if err != nil {
		t.Fatal(err)
	}
	err = addMallExtraRows(tables, goods)
	if err == nil || !strings.Contains(err.Error(), EnvMallExtraGoods) || !strings.Contains(err.Error(), "import_cap110_avatars") {
		t.Fatalf("err = %v, want a refusal naming %s and the import", err, EnvMallExtraGoods)
	}
}

/*
================
TestMallExtraGoodsCap110ListsTheImportedSet

Runs only where scripts/import_cap110_avatars.py was applied to the extraction:
each outfit is a priced mall offer with scraps and an icon, in its own tab.
================
*/
func TestMallExtraGoodsCap110ListsTheImportedSet(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	goods, err := ParseMallExtraGoods("cap110")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadMall(dir, goods)
	if err != nil && strings.Contains(err.Error(), "has no package data") {
		t.Skip("the cap-110 avatars are not imported into this extraction")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(enterworld.NewTextdataItems(dir)); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 81+len(goods) {
		t.Fatalf("mall has %d offers, want %d", len(catalog.Offers), 81+len(goods))
	}
	byCode := map[string]MallPackage{}
	for _, offer := range catalog.Offers {
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
		case strings.Contains(good.Package, "_HAT"):
			want = 30
		}
		if offer.Silk != want || offer.PackageID == 0 || len(offer.Scraps) == 0 || offer.Icon == "" {
			t.Errorf("%s = silk %d (want %d), package %d, %d scraps, icon %q", good.Package, offer.Silk, want,
				offer.PackageID, len(offer.Scraps), offer.Icon)
		}
	}
}
