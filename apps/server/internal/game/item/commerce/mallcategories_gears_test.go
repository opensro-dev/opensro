/*
===========================================================================

mallcategories_gears_test.go - Premium > Gears excludes gears magic stones

Port-only, not native (issue #3257): the 112 gears stones stay
unlisted even when their imported rows stand. The layout keeps its
93 offers, and without Nasrun goods it writes no Gears tab at all
(#4846). Bought stones still use the alchemy stone path (alchemy
package).

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

// gearsStubRefs layers the given codenames over a real item table:
// the catalogue a build with a partial later-client import resolves
// against (the layout's own authored goods still need their shipped
// rows).
/*
================
gearsStubRefs
================
*/
type gearsStubRefs struct {
	base  enterworld.ItemRefSource
	extra map[string]*enterworld.ItemRef
}

/*
================
gearsStubRefs::ItemRefByCodename
================
*/
func (s gearsStubRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if ref, ok := s.extra[codename]; ok {
		return ref, true
	}
	return s.base.ItemRefByCodename(codename)
}

/*
================
gearsStubRefs::ItemRefByID
================
*/
func (s gearsStubRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s.extra {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return s.base.ItemRefByID(id)
}

/*
================
TestMallCategoriesGearsNeverListStones

An imported gears stone stays unlisted: the catalogue keeps its 93
layout offers, and with SRO_NASRUN off Premium has no Gears tab, since
nothing would stand in it.
================
*/
func TestMallCategoriesGearsNeverListStones(t *testing.T) {
	tier.Long(t, "gears stones stay unlisted despite their imported rows")
	t.Setenv(nasrun.Env, "")
	dir := gamedatatest.TextdataDir(t)
	stone := "ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_STR_05"
	refs := gearsStubRefs{base: enterworld.NewTextdataItems(dir), extra: map[string]*enterworld.ItemRef{
		stone: {RefObjID: 39262, Codename: stone, NameStrID: "SN_" + stone, TypeIDs: [4]int64{3, 3, 11, 1},
			NativeFields:      enterworld.NewNativeFields(map[string]float64{"maxStack": 20}),
			DescriptionSymbol: "SD_" + stone, Icon: "icon\\item\\etc\\archimy_magicstone_str.ddj"},
	}}
	catalog, err := LoadMall(dir, nil, true, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 93 {
		t.Fatalf("layout mall with one gears row: %d offers, want 93", len(catalog.Offers))
	}
	if listed := mallOfferNamed(catalog, "PACKAGE_"+stone); listed != nil {
		t.Fatalf("mall lists imported gears stone: %+v", listed)
	}
	if gears := mallTabCoded(catalog, "MALL_PREMIUM_GEARS"); gears != nil {
		t.Fatalf("Gears tab %+v stands with an imported stone and no Nasrun goods", gears)
	}
	if err := catalog.Hydrate(refs); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestMallCategoriesGearsAbsentWithoutImport

No imported rows, no listings: the 93 layout offers and no Gears
tab, exactly the catalogue a build without the later client boots.
================
*/
func TestMallCategoriesGearsAbsentWithoutImport(t *testing.T) {
	tier.Long(t, "gears stones stay unlisted without the import")
	t.Setenv(nasrun.Env, "")
	dir := gamedatatest.TextdataDir(t)
	refs := gearsStubRefs{base: enterworld.NewTextdataItems(dir), extra: map[string]*enterworld.ItemRef{}}
	catalog, err := LoadMall(dir, nil, true, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 93 {
		t.Fatalf("layout mall without gears rows: %d offers, want 93", len(catalog.Offers))
	}
	if gears := mallTabCoded(catalog, "MALL_PREMIUM_GEARS"); gears != nil {
		t.Fatalf("Gears tab %+v stands without the import", gears)
	}
}

// nasrunStubRefs stages the Devil's Spirit rows as
// scripts/nasrun_import.py writes them: the six A grade Spirits,
// the extension gear and Sabakun's jewel over a real item table.
/*
================
nasrunStubRefs
================
*/
func nasrunStubRefs(dir string) gearsStubRefs {
	extra := map[string]*enterworld.ItemRef{
		nasrunGear: {RefObjID: 24415, Codename: nasrunGear, NameStrID: "SN_" + nasrunGear,
			TypeIDs:           [4]int64{3, 3, 13, 16},
			NativeFields:      enterworld.NewNativeFields(map[string]float64{"maxStack": 1}),
			DescriptionSymbol: "SD_" + nasrunGear,
			Icon:              "icon\\item\\mall\\nasrun_extension.ddj"},
		nasrunJewel: {RefObjID: 24417, Codename: nasrunJewel, NameStrID: "SN_" + nasrunJewel,
			TypeIDs:           [4]int64{3, 3, 10, 1},
			NativeFields:      enterworld.NewNativeFields(map[string]float64{"maxStack": 250}),
			DescriptionSymbol: "SD_" + nasrunJewel, Icon: "icon\\item\\etc\\nasrun_jewel.ddj"},
	}
	durability := int64(100)
	for index, spirit := range nasrunSpirits {
		extra[spirit] = &enterworld.ItemRef{RefObjID: uint32(24409 + index), Codename: spirit, NameStrID: "SN_" + spirit,
			TypeIDs:           [4]int64{3, 1, 14, 1},
			NativeFields:      enterworld.NewNativeFields(map[string]float64{"maxStack": 1}),
			DescriptionSymbol: "SD_" + spirit, Icon: "icon\\item\\mall\\nasrun.ddj",
			VarianceIntMin1c0: &durability}
	}
	return gearsStubRefs{base: enterworld.NewTextdataItems(dir), extra: extra}
}

/*
================
TestMallCategoriesNasrunGoodsListWithTheSystem

The Devil's Spirit system stands now (#3456): with SRO_NASRUN on
and their imported rows staged, the six Spirits, the extension
gear and Sabakun's jewel list in Premium's Gears tab, the Spirits
and the gear at the later mall's prices
(150 and 50 silk, #4240 part 4), the jewel at the authored 30,
all badged new, and the quotes resolve (#3456 step 5; replaces
the #3331 refusal test, which stood until the system did).
================
*/
func TestMallCategoriesNasrunGoodsListWithTheSystem(t *testing.T) {
	tier.Long(t, "Nasrun goods list in Premium > Gears with SRO_NASRUN on")
	t.Setenv(nasrun.Env, "on")
	dir := gamedatatest.TextdataDir(t)
	refs := nasrunStubRefs(dir)
	catalog, err := LoadMall(dir, nil, true, refs)
	if err != nil {
		t.Fatal(err)
	}
	// The 93 layout offers plus the six Spirits, the gear and the jewel.
	if len(catalog.Offers) != 101 {
		t.Fatalf("layout mall with the Nasrun rows: %d offers, want 101", len(catalog.Offers))
	}
	wantSilk := map[string]uint32{}
	for _, spirit := range nasrunSpirits {
		wantSilk["PACKAGE_"+spirit] = nasrunSpiritSilk
	}
	wantSilk["PACKAGE_"+nasrunGear] = nasrunGearSilk
	wantSilk["PACKAGE_"+nasrunJewel] = nasrunJewelSilk
	// With goods to sell the Gears tab is written, before Others.
	gears, others := mallTabCoded(catalog, "MALL_PREMIUM_GEARS"), mallTabCoded(catalog, "MALL_PREMIUM_OTHERS")
	if gears == nil || others == nil || gears.Shop != others.Shop || gears.Tab+1 != others.Tab {
		t.Fatalf("Premium tabs Gears %+v and Others %+v, want Gears just before Others", gears, others)
	}
	for _, offer := range mallOffersIn(catalog, "MALL_PREMIUM_GEARS") {
		want, nasrunGood := wantSilk[offer.Code]
		if !nasrunGood {
			continue
		}
		delete(wantSilk, offer.Code)
		if offer.Silk != want || offer.Badge != mallBadgeNew {
			t.Fatalf("%s = %d silk badged %q, want %d silk badged new",
				offer.Code, offer.Silk, offer.Badge, want)
		}
	}
	if len(wantSilk) != 0 {
		t.Fatalf("Gears tab holds no %v", wantSilk)
	}
	if err := catalog.Hydrate(refs); err != nil {
		t.Fatal(err)
	}
	listed := *mallOfferNamed(catalog, "PACKAGE_"+nasrunJewel)
	if len(listed.ItemIDs) != 1 || listed.ItemIDs[0] != 24417 {
		t.Fatalf("jewel contents = %v, want the imported jewel id", listed.ItemIDs)
	}
	quote, err := catalog.Quote(wire.MallPurchase{
		Group: listed.Group, Shop: listed.Shop, Tab: listed.Tab, Slot: listed.Slot,
		Package: listed.PackageID, Quantity: 1,
	})
	if err != nil || quote.Cost.Silk != nasrunJewelSilk {
		t.Fatalf("jewel quote = %+v, %v; want %d silk", quote.Cost, err, nasrunJewelSilk)
	}
	spirit := *mallOfferNamed(catalog, "PACKAGE_"+nasrunSpirits[0])
	quote, err = catalog.Quote(wire.MallPurchase{
		Group: spirit.Group, Shop: spirit.Shop, Tab: spirit.Tab, Slot: spirit.Slot,
		Package: spirit.PackageID, Quantity: 1,
	})
	if err != nil || quote.Cost.Silk != nasrunSpiritSilk {
		t.Fatalf("spirit quote = %+v, %v; want %d silk", quote.Cost, err, nasrunSpiritSilk)
	}
}

/*
================
TestMallCategoriesNasrunGoodsOffWithoutTheSystem

The same rows in the table but SRO_NASRUN off: nothing lists, so a
mall never sells an item the (unported or switched-off) system
cannot use (#3331).
================
*/
func TestMallCategoriesNasrunGoodsOffWithoutTheSystem(t *testing.T) {
	tier.Long(t, "Nasrun goods stay unlisted with SRO_NASRUN off")
	t.Setenv(nasrun.Env, "")
	dir := gamedatatest.TextdataDir(t)
	refs := nasrunStubRefs(dir)
	catalog, err := LoadMall(dir, nil, true, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 93 {
		t.Fatalf("layout mall with SRO_NASRUN off: %d offers, want 93", len(catalog.Offers))
	}
	for _, offer := range catalog.Offers {
		if strings.Contains(offer.Code, "NASRUN") {
			t.Fatalf("mall lists %s with SRO_NASRUN off", offer.Code)
		}
	}
}
