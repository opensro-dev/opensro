/*
===========================================================================

mallcategories_gears_test.go - the gears magic stones in Premium > Gears

Port-only, not native (issue #3257): the 112 gears stones list only
while the item table holds their imported rows; without them the
layout is the shipped 91 offers and the Gears tab stands empty.
Their use is the native alchemy stone path (alchemy package).

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
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
TestMallCategoriesGearsListOnlyImportedStones

One imported gears stone lists alone in Premium's Gears tab (shop
4, tab 1) at the authored price, badged new, and its quote resolves;
the other 111 stay unlisted and every shipped address stands.
================
*/
func TestMallCategoriesGearsListOnlyImportedStones(t *testing.T) {
	tier.Long(t, "gears stones list behind their imported rows")
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
	// The shipped 91 plus the one imported stone.
	if len(catalog.Offers) != 92 {
		t.Fatalf("layout mall with one gears row: %d offers, want 92", len(catalog.Offers))
	}
	gears := mallOffersOn(catalog, 4, 1)
	if len(gears) != 1 || gears[0].Code != "PACKAGE_"+stone {
		t.Fatalf("Gears tab holds %+v, want the one imported stone", gears)
	}
	listed := gears[0]
	if listed.Silk != gearsStoneSilk || listed.Badge != mallBadgeNew {
		t.Fatalf("%s = %d silk badged %q, want %d silk badged new",
			listed.Code, listed.Silk, listed.Badge, gearsStoneSilk)
	}
	if err := catalog.Hydrate(refs); err != nil {
		t.Fatal(err)
	}
	listed = *mallOfferNamed(catalog, "PACKAGE_"+stone)
	if len(listed.ItemIDs) != 1 || listed.ItemIDs[0] != 39262 {
		t.Fatalf("gears contents = %v, want the imported stone id", listed.ItemIDs)
	}
	quote, err := catalog.Quote(wire.MallPurchase{
		Group: listed.Group, Shop: listed.Shop, Tab: listed.Tab, Slot: listed.Slot,
		Package: listed.PackageID, Quantity: 1,
	})
	if err != nil || quote.Cost.Silk != gearsStoneSilk {
		t.Fatalf("gears quote = %+v, %v; want %d silk", quote.Cost, err, gearsStoneSilk)
	}
}

/*
================
TestMallCategoriesGearsAbsentWithoutImport

No imported rows, no listings: the Gears tab stands empty at the
shipped 91 offers, exactly the catalogue a build without the
later client boots.
================
*/
func TestMallCategoriesGearsAbsentWithoutImport(t *testing.T) {
	tier.Long(t, "gears stones stay unlisted without the import")
	dir := gamedatatest.TextdataDir(t)
	refs := gearsStubRefs{base: enterworld.NewTextdataItems(dir), extra: map[string]*enterworld.ItemRef{}}
	catalog, err := LoadMall(dir, nil, true, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 91 {
		t.Fatalf("layout mall without gears rows: %d offers, want 91", len(catalog.Offers))
	}
	if gears := mallOffersOn(catalog, 4, 1); len(gears) != 0 {
		t.Fatalf("Gears tab holds %+v without the import, want empty", gears)
	}
	if len(GearsStoneKinds)*gearsStoneDegrees != 112 {
		t.Fatalf("gears stock = %d kinds at %d degrees, want 112",
			len(GearsStoneKinds), gearsStoneDegrees)
	}
}
