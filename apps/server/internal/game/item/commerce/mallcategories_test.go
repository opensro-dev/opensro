/*
===========================================================================

mallcategories_test.go - SRO_MALL_CATEGORIES against the shipped data

Port-only, not native (issue #3257): "on" appends the later mall's HOT
shop and tabs, re-homes the Halloween outfits out of the avatar tabs,
lists the Destruction Rondo and badges the N/H/R goods; "off" is the
shipped mall exactly. Both settings are proven here against the shipped
reference data.

===========================================================================
*/
package commerce

import (
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
mallOffersOn

The offers of one shop and tab, for the layout's address assertions.
================
*/
func mallOffersOn(catalog *MallCatalog, shop uint8, tab uint8) []MallPackage {
	var offers []MallPackage
	for _, offer := range catalog.Offers {
		if offer.Shop == shop && offer.Tab == tab {
			offers = append(offers, offer)
		}
	}
	return offers
}

/*
================
mallOfferNamed
================
*/
func mallOfferNamed(catalog *MallCatalog, code string) *MallPackage {
	for index := range catalog.Offers {
		if catalog.Offers[index].Code == code {
			return &catalog.Offers[index]
		}
	}
	return nil
}

/*
================
TestMallCategoriesFromEnv
================
*/
func TestMallCategoriesFromEnv(t *testing.T) {
	t.Setenv(EnvMallCategories, "")
	if on, err := MallCategoriesFromEnv(); err != nil || on {
		t.Fatalf("unset = %v, %v; want off, no error", on, err)
	}
	t.Setenv(EnvMallCategories, "on")
	if on, err := MallCategoriesFromEnv(); err != nil || !on {
		t.Fatalf("on = %v, %v; want on, no error", on, err)
	}
	t.Setenv(EnvMallCategories, "later")
	if _, err := MallCategoriesFromEnv(); err == nil || !strings.Contains(err.Error(), EnvMallCategories) {
		t.Fatalf("garbage = %v, want a refusal naming %s", err, EnvMallCategories)
	}
}

/*
================
TestMallCategoriesOn

The layout against the shipped data with no extra goods: the HOT shop
is shop 5, Premium and Alchemy gain their tabs, the Halloween packages
stand in the HOT tabs with shipped data alone, the Destruction Rondo
lists in Alchemy's Others tab at 5 silk, and every shipped address
stands.
================
*/
func TestMallCategoriesOn(t *testing.T) {
	tier.Long(t, "the later mall's category layout loads over the shipped mall")
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	shipped, err := LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadMall(dir, nil, true, items)
	if err != nil {
		t.Fatal(err)
	}
	// 81 shipped + 6 Halloween + the Rondo + the 3 summon scrolls
	// + the kangaroo and pegasus scrolls.
	if len(catalog.Offers) != 93 {
		t.Fatalf("layout mall: %d offers, want 93", len(catalog.Offers))
	}
	// 16 shipped tabs + 2 HOT + 2 Premium + 1 Alchemy.
	if len(catalog.Tabs) != 21 {
		t.Fatalf("layout mall: %d tabs, want 21", len(catalog.Tabs))
	}
	tabCount := map[uint8]int{}
	for _, tab := range catalog.Tabs {
		tabCount[tab.Shop]++
	}
	if tabCount[5] != 2 || tabCount[4] != 3 || tabCount[0] != 3 {
		t.Fatalf("tab counts HOT/Premium/Alchemy = %d/%d/%d, want 2/3/3",
			tabCount[5], tabCount[4], tabCount[0])
	}
	// The shipped addresses stand: the astral offer keeps shop 0,
	// tab 0, and every shipped offer keeps its price.
	astral := mallOffersOn(catalog, 0, 0)
	if len(astral) != 9 {
		t.Fatalf("Alchemy tab 0: %d offers, want the shipped 9", len(astral))
	}
	for _, offer := range shipped.Offers {
		got := mallOfferNamed(catalog, offer.Code)
		if got == nil || got.MallAddress != offer.MallAddress || got.Silk != offer.Silk {
			t.Fatalf("shipped offer %s moved: %+v vs %+v", offer.Code, got, offer)
		}
	}
	// The Halloween packages stand once each, in the HOT tabs, badged.
	exclusive := mallOffersOn(catalog, 5, 0)
	models := mallOffersOn(catalog, 5, 1)
	if len(exclusive) != 2 || len(models) != 4 {
		t.Fatalf("HOT tabs hold %d exclusive and %d models, want 2 and 4",
			len(exclusive), len(models))
	}
	for _, offer := range append(exclusive, models...) {
		if !strings.Contains(offer.Code, "HALLOWEEN") {
			t.Fatalf("HOT tab holds %s, want only the Halloween outfits", offer.Code)
		}
		if offer.Badge != mallBadgeHot {
			t.Fatalf("%s badge = %q, want %q", offer.Code, offer.Badge, mallBadgeHot)
		}
	}
	// The Destruction Rondo: Alchemy tab 2, 5 silk, badged new, and the
	// purchase quote resolves its hydrated item.
	others := mallOffersOn(catalog, 0, 2)
	if len(others) != 1 {
		t.Fatalf("Alchemy tab 2: %d offers, want the Rondo alone", len(others))
	}
	listed := others[0]
	if listed.Code != "PACKAGE_ITEM_MALL_ARCHEMY_RONDO_02" || listed.Silk != 5 || listed.Badge != mallBadgeNew {
		t.Fatalf("Rondo = %s at %d silk badged %q, want the authored package at 5 silk badged new",
			listed.Code, listed.Silk, listed.Badge)
	}
	if err := catalog.Hydrate(items); err != nil {
		t.Fatal(err)
	}
	rondo := mallOfferNamed(catalog, "PACKAGE_ITEM_MALL_ARCHEMY_RONDO_02")
	if rondo == nil || len(rondo.ItemIDs) != 1 {
		t.Fatalf("Rondo contents = %v, want the one Rondo item", rondo)
	}
	quote, err := catalog.Quote(wire.MallPurchase{
		Group: rondo.Group, Shop: rondo.Shop, Tab: rondo.Tab, Slot: rondo.Slot,
		Package: rondo.PackageID, Quantity: 1,
	})
	if err != nil || quote.Cost.Silk != 5 {
		t.Fatalf("Rondo quote = %+v, %v; want 5 silk", quote.Cost, err)
	}
	// The summon scrolls: Premium's Others tab (shop 4, tab 2), badged
	// new at the authored prices, one item each once hydrated.
	scrolls := mallOffersOn(catalog, 4, 2)
	if len(scrolls) != 3 {
		t.Fatalf("Premium Others tab: %d offers, want the 3 summon scrolls", len(scrolls))
	}
	wantSilk := map[string]uint32{
		"PACKAGE_ITEM_ETC_E050729_SUMMON_SCROLL":     30,
		"PACKAGE_ITEM_ETC_E050729_SUMMON_ONE_SCROLL": 30,
		"PACKAGE_ITEM_ETC_E050729_SUMMON_TWO_SCROLL": 60,
	}
	for _, offer := range scrolls {
		if offer.Badge != mallBadgeNew || offer.Silk != wantSilk[offer.Code] {
			t.Fatalf("%s = %d silk badged %q, want %d silk badged new",
				offer.Code, offer.Silk, offer.Badge, wantSilk[offer.Code])
		}
	}
	two := mallOfferNamed(catalog, "PACKAGE_ITEM_ETC_E050729_SUMMON_TWO_SCROLL")
	if two == nil || len(two.ItemIDs) != 1 {
		t.Fatalf("two-scroll contents = %v, want the one scroll item", two)
	}
	scrollQuote, err := catalog.Quote(wire.MallPurchase{
		Group: two.Group, Shop: two.Shop, Tab: two.Tab, Slot: two.Slot,
		Package: two.PackageID, Quantity: 1,
	})
	if err != nil || scrollQuote.Cost.Silk != 60 {
		t.Fatalf("two-scroll quote = %+v, %v; want 60 silk", scrollQuote.Cost, err)
	}
	// The premium tickets are badged; the shipped offers are not.
	for code, want := range map[string]string{
		"PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME":      mallBadgeRecommended,
		"PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME_PLUS": mallBadgeRecommended,
		"PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_SILVERTIME":    mallBadgeRecommended,
	} {
		if offer := mallOfferNamed(catalog, code); offer == nil || offer.Badge != want {
			t.Fatalf("%s badge = %v, want %q", code, offer, want)
		}
	}
	if offer := mallOfferNamed(catalog, "PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_01"); offer != nil && offer.Badge != "" {
		t.Fatalf("a shipped alchemy offer is badged %q", offer.Badge)
	}
	// The imported pet scrolls (#3328) wait for their rows: this
	// catalogue's item table imports none, so no pet listing stands.
	for _, code := range []string{
		"PACKAGE_ITEM_COS_P_FOX_SCROLL",
		"PACKAGE_ITEM_COS_P_WINTER_SNOWMAN_SCROLL",
		"PACKAGE_ITEM_COS_P_WINTER_SNOWMAN_EVENT_SCROLL",
	} {
		if offer := mallOfferNamed(catalog, code); offer != nil {
			t.Fatalf("%s is listed without its imported item row", code)
		}
	}
	// The kangaroo's and the pegasus's rows are the tree's own, so
	// their listings stand: badged new at their kinds' tiers, and the
	// purchase quotes resolve their scroll items.
	for code, silk := range map[string]uint32{
		"PACKAGE_ITEM_COS_P_KANGAROO_SCROLL": growthPetScrollSilk,
		"PACKAGE_ITEM_COS_C_PEGASUS_SCROLL":  vehiclePetScrollSilk,
	} {
		offer := mallOfferNamed(catalog, code)
		if offer == nil || offer.Silk != silk || offer.Badge != mallBadgeNew {
			t.Fatalf("%s = %+v, want a new-badged listing at %d silk", code, offer, silk)
		}
		quote, err := catalog.Quote(wire.MallPurchase{
			Group: offer.Group, Shop: offer.Shop, Tab: offer.Tab, Slot: offer.Slot,
			Package: offer.PackageID, Quantity: 1,
		})
		if err != nil || quote.Cost.Silk != silk {
			t.Fatalf("%s quote = %+v, %v; want %d silk", code, quote.Cost, err, silk)
		}
	}
}

/*
================
TestMallCategoriesOffIsTheShippedMall

"off" stamps no badge and adds no shop: the catalogue is the shipped
one, byte for byte in its tabs and offers.
================
*/
func TestMallCategoriesOffIsTheShippedMall(t *testing.T) {
	tier.Long(t, "categories off is the shipped mall")
	dir := gamedatatest.TextdataDir(t)
	catalog, err := LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 81 || len(catalog.Tabs) != 16 {
		t.Fatalf("categories off: %d offers, %d tabs; want the shipped 81 and 16",
			len(catalog.Offers), len(catalog.Tabs))
	}
	for _, offer := range catalog.Offers {
		if offer.Badge != "" {
			t.Fatalf("categories off: %s is badged %q", offer.Code, offer.Badge)
		}
	}
	for _, tab := range catalog.Tabs {
		if tab.Category == "MALL_HOT" {
			t.Fatal("categories off: the HOT shop is listed")
		}
	}
}

/*
================
TestMallCategoriesWithExtraGoods

The two settings together: the Halloween outfits move (never
duplicate) from the avatar tabs the extra goods listed them in, so
every package keeps one listing and the basket quotes it once.
================
*/
func TestMallCategoriesWithExtraGoods(t *testing.T) {
	tier.Long(t, "extra goods and categories together keep one listing per package")
	dir := gamedatatest.TextdataDir(t)
	goods, err := ParseMallExtraGoods("on")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadMall(dir, goods, true, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	// 81 shipped + 22 extra + the Rondo + the 3 summon scrolls + the
	// kangaroo and pegasus scrolls: the Halloween six are among the
	// 22 and move tabs rather than twice.
	if len(catalog.Offers) != 109 {
		t.Fatalf("both settings: %d offers, want 109", len(catalog.Offers))
	}
	listings := map[string]int{}
	for _, offer := range catalog.Offers {
		listings[offer.Code]++
		if strings.Contains(offer.Code, "HALLOWEEN") && offer.Shop != 5 {
			t.Fatalf("%s still lists in shop %d, want the HOT shop alone", offer.Code, offer.Shop)
		}
	}
	for code, count := range listings {
		if count != 1 {
			t.Fatalf("%s lists %d times, want once", code, count)
		}
	}
	// The avatar tabs keep the other sixteen extra outfits.
	avatar := 0
	for _, offer := range catalog.Offers {
		if offer.Shop == 1 {
			avatar++
		}
	}
	if avatar != 16+8 {
		t.Fatalf("avatar shops hold %d offers, want 24 (8 shipped, 16 extra)", avatar)
	}
}

/*
================
TestMallCategoriesKeepAPackageTheDataAlreadySells

The winter snowman scroll reaches the data as its own v1.696 mall row
(#4241). Here SRO_MALL_EXTRA_GOODS stands in for that import, at a price
the layout does not use. The layout then sells the data's package: one
listing, in its own pet tab, at the data's price, badged new. Before the
fix this refused the boot with "already has package data".
================
*/
func TestMallCategoriesKeepAPackageTheDataAlreadySells(t *testing.T) {
	tier.Long(t, "a layout good the data already sells keeps the data's package")
	const snowmanPackage, dataSilk = "PACKAGE_ITEM_COS_P_WINTER_SNOWMAN_SCROLL", 77
	dir := gamedatatest.TextdataDir(t)
	shipped := enterworld.NewTextdataItems(dir)
	base, ok := shipped.ItemRefByCodename("ITEM_COS_P_CAT_SCROLL")
	if !ok {
		t.Fatal("no shipped pet scroll to model the snowman on")
	}
	snowman := *base
	snowman.RefObjID, snowman.Codename = 88019, "ITEM_COS_P_WINTER_SNOWMAN_SCROLL"
	items := importedItems{ItemRefSource: shipped, extra: map[string]*enterworld.ItemRef{snowman.Codename: &snowman}}
	native := []MallExtraGood{{Tab: "MALL_PET_B_SILK", Package: snowmanPackage, Item: snowman.Codename, Silk: dataSilk}}
	catalog, err := LoadMall(dir, native, true, items)
	if err != nil {
		t.Fatal(err)
	}
	listings := 0
	for _, offer := range catalog.Offers {
		if offer.Code == snowmanPackage {
			listings++
		}
	}
	offer := mallOfferNamed(catalog, snowmanPackage)
	cat := mallOfferNamed(catalog, "PACKAGE_ITEM_COS_P_CAT_SCROLL")
	if listings != 1 || offer == nil || cat == nil {
		t.Fatalf("snowman lists %d times (offer %+v), want once beside the cat", listings, offer)
	}
	if offer.Silk != dataSilk || offer.Badge != mallBadgeNew || offer.Shop != cat.Shop || offer.Tab != cat.Tab {
		t.Fatalf("snowman offer = %+v, want the data's %d silk, badged new, in the cat's tab %d/%d",
			offer, dataSilk, cat.Shop, cat.Tab)
	}
}

/*
================
TestMallCategoriesRefuseALayoutPackageTwice

A layout table naming one package in two tabbed goods would list it
twice; the boot refuses it naming the setting.
================
*/
func TestMallCategoriesRefuseALayoutPackageTwice(t *testing.T) {
	tier.Long(t, "a layout package named twice refuses the boot")
	saved := mallCategoryGoods
	t.Cleanup(func() { mallCategoryGoods = saved })
	var twice mallCategoryGood
	for _, good := range saved {
		if good.Tab != "" && good.Item == "" {
			twice = good
			break
		}
	}
	if twice.Package == "" {
		t.Fatal("no tabbed layout good to repeat")
	}
	mallCategoryGoods = append(slices.Clone(saved), twice)
	dir := gamedatatest.TextdataDir(t)
	_, err := LoadMall(dir, nil, true, enterworld.NewTextdataItems(dir))
	if err == nil || !strings.Contains(err.Error(), "the layout lists "+twice.Package+" twice") {
		t.Fatalf("a repeated layout package: err %v, want the layout lists %s twice", err, twice.Package)
	}
}

/*
================
importedItems

The shipped item table plus rows only the later-client import authors,
for the model switcher listing.
================
*/
type importedItems struct {
	enterworld.ItemRefSource
	extra map[string]*enterworld.ItemRef
}

func (s importedItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if ref, ok := s.extra[codename]; ok {
		return ref, true
	}
	return s.ItemRefSource.ItemRefByCodename(codename)
}

func (s importedItems) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s.extra {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return s.ItemRefSource.ItemRefByID(id)
}

/*
================
TestMallCategoriesListOnlyImportedSwitchers

The shipped data holds no switcher row, so the layout lists none of
them and still boots (TestMallCategoriesOn's 91). With one switcher
imported, exactly that one joins Halloween Models at 600 silk, badged
hot, and its quote resolves.
================
*/
func TestMallCategoriesListOnlyImportedSwitchers(t *testing.T) {
	tier.Long(t, "the model switchers list only when the import authored them")
	dir := gamedatatest.TextdataDir(t)
	shipped := enterworld.NewTextdataItems(dir)
	base, ok := shipped.ItemRefByCodename("ITEM_ETC_ARCHEMY_RONDO_02")
	if !ok {
		t.Fatal("no shipped row to model the switcher on")
	}
	switcher := *base
	switcher.RefObjID, switcher.Codename = 88018, "ITEM_ORIGIN_CH_HW_SWORD_SCROLL"
	items := importedItems{ItemRefSource: shipped, extra: map[string]*enterworld.ItemRef{switcher.Codename: &switcher}}
	catalog, err := LoadMall(dir, nil, true, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 94 {
		t.Fatalf("layout mall: %d offers, want 93 plus the one imported switcher", len(catalog.Offers))
	}
	offer := mallOfferNamed(catalog, "PACKAGE_ITEM_ORIGIN_CH_HW_SWORD_SCROLL")
	if offer == nil || offer.Shop != 5 || offer.Tab != 1 || offer.Silk != modelSwitcherSilk || offer.Badge != mallBadgeHot {
		t.Fatalf("switcher offer = %+v, want HOT Halloween Models at %d silk badged hot", offer, modelSwitcherSilk)
	}
	if err := catalog.Hydrate(items); err != nil {
		t.Fatal(err)
	}
	quote, err := catalog.Quote(wire.MallPurchase{
		Group: offer.Group, Shop: offer.Shop, Tab: offer.Tab, Slot: offer.Slot,
		Package: offer.PackageID, Quantity: 1,
	})
	if err != nil || quote.Cost.Silk != modelSwitcherSilk {
		t.Fatalf("switcher quote = %+v, %v; want %d silk", quote.Cost, err, modelSwitcherSilk)
	}
}

/*
================
TestImportedPetScrollsTakeTheShippedTiers

The mall's pet scrolls sell at the shipped mall's own pet prices, read
from its listings: the fox and the kangaroo at the growth pets' (the
bear's), the snowman at the silk pickup pets' (the cat's), the pegasus
at the vehicles' (the ostrich's). The event snowman scroll is never
sold (#3328 listed all three at 30).
================
*/
func TestImportedPetScrollsTakeTheShippedTiers(t *testing.T) {
	tier.Long(t, "imported pet scroll prices")
	catalog, err := LoadMall(gamedatatest.TextdataDir(t), nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	growth := mallOfferNamed(catalog, "PACKAGE_ITEM_COS_P_BEAR_SCROLL")
	pickup := mallOfferNamed(catalog, "PACKAGE_ITEM_COS_P_CAT_SCROLL")
	vehicle := mallOfferNamed(catalog, "PACKAGE_ITEM_COS_C_OSTRICH_SCROLL")
	if growth == nil || pickup == nil || vehicle == nil {
		t.Fatal("the shipped mall lost its bear, cat or ostrich scroll")
	}
	want := map[string]uint32{
		"PACKAGE_ITEM_COS_P_FOX_SCROLL":            growth.Silk,
		"PACKAGE_ITEM_COS_P_WINTER_SNOWMAN_SCROLL": pickup.Silk,
		"PACKAGE_ITEM_COS_P_KANGAROO_SCROLL":       growth.Silk,
		"PACKAGE_ITEM_COS_C_PEGASUS_SCROLL":        vehicle.Silk,
	}
	goods := petScrollGoods()
	if len(goods) != len(want) {
		t.Fatalf("%d pet listings, want %d (the event scroll is not sold)", len(goods), len(want))
	}
	for _, good := range goods {
		if price, ok := want[good.Package]; !ok || good.Silk != price {
			t.Fatalf("%s at %d silk, want %d", good.Package, good.Silk, price)
		}
	}
}
