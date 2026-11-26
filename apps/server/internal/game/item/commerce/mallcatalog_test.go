/*
===========================================================================

mallcatalog_test.go - complete authored mall coverage and native shop addresses

===========================================================================
*/
package commerce

import (
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestShippedMallCatalog
================
*/
func TestShippedMallCatalog(t *testing.T) {
	tier.Long(t, "the whole shipped mall catalogue loads")
	catalog, err := LoadMall(gamedatatest.TextdataDir(t), nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 81 || len(catalog.Tabs) != 16 {
		t.Fatalf("incomplete v1.150 mall: %d offers, %d tabs", len(catalog.Offers), len(catalog.Tabs))
	}
	if err := catalog.Hydrate(enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))); err != nil {
		t.Fatal(err)
	}
	seen := map[MallAddress]bool{}
	for _, offer := range catalog.Offers {
		if seen[offer.MallAddress] || offer.Group != 852 || offer.PackageID == 0 || len(offer.Scraps) == 0 {
			t.Fatalf("invalid native merchandise address: %+v", offer)
		}
		seen[offer.MallAddress] = true
	}
	if catalog.Tabs[0].Category != "MALL_ARCHEMY" || catalog.Tabs[0].Shop != 0 || catalog.Tabs[0].Tab != 0 {
		t.Fatalf("shop indices were replaced with UI category order: %+v", catalog.Tabs[0])
	}
}

/*
================
TestMallCatalogRequiresReferenceFiles
================
*/
func TestMallCatalogRequiresReferenceFiles(t *testing.T) {
	if _, err := LoadMall(t.TempDir(), nil, false, nil); err == nil {
		t.Fatal("accepted missing reference data")
	}
}

/*
================
TestLoadMallNasrunShelvesFollowTheSwitch

The built tree can carry the later mall's two Nasrun shelves
(#4240 part 4): SRO_NASRUN on, the Spirit and the extension gear
stand at the later mall's silk prices; off, neither the shelves
nor their listings exist and the native stock is untouched.
================
*/
func TestLoadMallNasrunShelvesFollowTheSwitch(t *testing.T) {
	scrap := func(code string) []string {
		row := []string{"1", "15", code, "ITEM"}
		for len(row) < 20 {
			row = append(row, "0")
		}
		return row
	}
	dir := t.TempDir()
	writeMallTables(t, dir, map[string][][]string{
		"refshopgroup":          {{"1", "15", "852", "GROUP_MALL"}},
		"refmappingshopgroup":   {{"1", "15", "GROUP_MALL", "MALL_PREMIUM"}},
		"refmappingshopwithtab": {{"1", "15", "MALL_PREMIUM", "MALL_PREMIUM_GROUP1"}},
		"refshoptab": {
			{"1", "11", "4401", "MALL_PREMIUM_ETC", "MALL_PREMIUM_GROUP1", "UIIT_STT_SILKMALL_ETC"},
			{"1", "11", "4409", "MALL_PREMIUM_NASRUN", "MALL_PREMIUM_GROUP1", "UIIT_STT_SILKMALL_PREMIUM_NASRUN"},
			{"1", "11", "4410", "MALL_PREMIUM_NASRUN_ETC", "MALL_PREMIUM_GROUP1", "UIIT_STT_SILKMALL_PREMIUM_NASRUN_ETC"},
		},
		"refshopgoods": {
			{"1", "11", "MALL_PREMIUM_ETC", "PACKAGE_NATIVE", "0"},
			{"1", "11", "MALL_PREMIUM_NASRUN", "PACKAGE_ITEM_MALL_AVATAR_M_NASRUN", "0"},
			{"1", "11", "MALL_PREMIUM_NASRUN_ETC", "PACKAGE_ITEM_MALL_NASRUN_EXTENSION", "0"},
		},
		"refpackageitem": {
			{"1", "11", "51001", "PACKAGE_NATIVE", "0", "0", "Name", "Description", "item/test.ddj"},
			{"1", "11", "51522", "PACKAGE_ITEM_MALL_AVATAR_M_NASRUN", "0", "0", "Name", "Description", "item/test.ddj"},
			{"1", "11", "51528", "PACKAGE_ITEM_MALL_NASRUN_EXTENSION", "0", "0", "Name", "Description", "item/test.ddj"},
		},
		"refpricepolicyofitem": {
			{"1", "11", "PACKAGE_NATIVE", "2", "20"},
			{"1", "11", "PACKAGE_ITEM_MALL_AVATAR_M_NASRUN", "2", "150"},
			{"1", "11", "PACKAGE_ITEM_MALL_NASRUN_EXTENSION", "2", "50"},
		},
		"refscrapofpackageitem":            {scrap("PACKAGE_NATIVE"), scrap("PACKAGE_ITEM_MALL_AVATAR_M_NASRUN"), scrap("PACKAGE_ITEM_MALL_NASRUN_EXTENSION")},
		"refconditiontosellpackageitem":    nil,
		"refrewardpolicytosellpackageitem": nil,
	})

	t.Setenv(nasrun.Env, "on")
	catalog, err := LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tabs) != 3 {
		t.Fatalf("tabs with %s on: %+v", nasrun.Env, catalog.Tabs)
	}
	byCode := map[string]MallPackage{}
	for _, offer := range catalog.Offers {
		byCode[offer.Code] = offer
	}
	if offer := byCode["PACKAGE_ITEM_MALL_AVATAR_M_NASRUN"]; offer.Silk != 150 || offer.Tab != 1 {
		t.Fatalf("the Spirit's listing with %s on: %+v", nasrun.Env, offer)
	}
	if offer := byCode["PACKAGE_ITEM_MALL_NASRUN_EXTENSION"]; offer.Silk != 50 || offer.Tab != 2 {
		t.Fatalf("the extension gear's listing with %s on: %+v", nasrun.Env, offer)
	}

	t.Setenv(nasrun.Env, "")
	catalog, err = LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tabs) != 1 || catalog.Tabs[0].Code != "MALL_PREMIUM_ETC" {
		t.Fatalf("tabs with %s off: %+v", nasrun.Env, catalog.Tabs)
	}
	if len(catalog.Offers) != 1 || catalog.Offers[0].Code != "PACKAGE_NATIVE" ||
		catalog.Offers[0].Silk != 20 || catalog.Offers[0].Tab != 0 {
		t.Fatalf("offers with %s off: %+v", nasrun.Env, catalog.Offers)
	}
}

/*
================
mallFixtureTables
================
*/
func mallFixtureTables() map[string][][]string {
	return map[string][][]string{
		"refshopgroup":          {{"1", "15", "852", "GROUP_MALL"}},
		"refmappingshopgroup":   {{"1", "15", "GROUP_MALL", "MALL_CONSUME"}},
		"refmappingshopwithtab": {{"1", "15", "MALL_CONSUME", "MALL_TABS"}},
		"refshoptab":            {{"1", "15", "1", "MALL_POTION", "MALL_TABS", "Potion"}},
		"refshopgoods":          {{"1", "15", "MALL_POTION", "PACKAGE", "0"}},
		"refpackageitem":        {{"1", "15", "100", "PACKAGE", "0", "0", "Name", "Description", "item/test.ddj"}},
		"refpricepolicyofitem":  {{"1", "15", "PACKAGE", "2", "20"}, {"1", "15", "PACKAGE", "16", "0"}},
		"refscrapofpackageitem": {{"1", "15", "PACKAGE", "ITEM", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"}},
	}
}

/*
================
TestMallCatalogRejectsAmbiguousAndUnsupportedData
================
*/
func TestMallCatalogRejectsAmbiguousAndUnsupportedData(t *testing.T) {
	if catalog, err := buildMallCatalog(mallFixtureTables()); err != nil || len(catalog.Offers) != 1 {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	for _, name := range []string{"refmappingshopgroup", "refmappingshopwithtab", "refshoptab", "refshopgoods", "refpackageitem", "refpricepolicyofitem"} {
		t.Run("duplicate-"+name, func(t *testing.T) {
			tables := mallFixtureTables()
			tables[name] = append(tables[name], append([]string(nil), tables[name][0]...))
			if _, err := buildMallCatalog(tables); err == nil {
				t.Fatal("ambiguous catalogue was accepted")
			}
		})
	}
	for _, name := range []string{"refmappingshopwithtab", "refshoptab", "refpackageitem", "refpricepolicyofitem", "refscrapofpackageitem"} {
		t.Run("missing-"+name, func(t *testing.T) {
			tables := mallFixtureTables()
			delete(tables, name)
			if _, err := buildMallCatalog(tables); err == nil {
				t.Fatal("incomplete catalogue was accepted")
			}
		})
	}
	for _, name := range []string{"refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		t.Run(name, func(t *testing.T) {
			tables := mallFixtureTables()
			tables[name] = [][]string{{"1", "15", "PACKAGE"}}
			if _, err := buildMallCatalog(tables); err == nil {
				t.Fatal("unsupported restriction or reward silently ignored")
			}
		})
	}
	tables := mallFixtureTables()
	tables["refpricepolicyofitem"][1][4] = "1"
	if _, err := buildMallCatalog(tables); err == nil {
		t.Fatal("fixed point charge silently ignored")
	}
}

// rentScrapFixture is a mall fixture whose one scrap rents its item for
// the named refrentitem term (#4240).
/*
================
rentScrapFixture
================
*/
func rentScrapFixture(term string) map[string][][]string {
	tables := mallFixtureTables()
	row := tables["refscrapofpackageitem"][0]
	row[3] = "ITEM_ETC_ARCHEMY_RONDO_02"
	for len(row) <= 21 {
		row = append(row, "xxx")
	}
	row[21] = term
	tables["refscrapofpackageitem"][0] = row
	return tables
}

// rentTermRow is a refrentitem row: the term at cell 1, the rented
// item's RefObjID at 2, RentTime at 9.
/*
================
rentTermRow
================
*/
func rentTermRow(term string, item uint32, minutes string) []string {
	row := make([]string, 15)
	row[0] = "1"
	row[1] = term
	row[2] = strconv.FormatUint(uint64(item), 10)
	row[9] = minutes
	return row
}

/*
================
TestRentTermsParse

Periods resolve to their minutes and the item they rent, fixed
windows are kept apart, and a row too short or unparseable is skipped,
never priced.
================
*/
func TestRentTermsParse(t *testing.T) {
	noItem := rentTermRow("RENT_ITEM_NO_ITEM", 0, "1440")
	noItem[rentTermItem] = "NULL"
	terms := parseRentTerms([][]string{
		rentTermRow("RENT_ITEM_D1", 29411, "1440"),
		rentTermRow("RENT_ITEM_D7", 29405, "10080"),
		rentTermRow("RENT_ITEM_WINDOW", 29411, "0"),
		{"1", "RENT_ITEM_SHORT"},
		rentTermRow("RENT_ITEM_BAD", 29411, "abc"),
		noItem,
	})
	if terms.periods["RENT_ITEM_D1"] != 1440 || terms.periods["RENT_ITEM_D7"] != 10080 {
		t.Fatalf("periods = %v, want the one-day and seven-day terms", terms.periods)
	}
	if terms.items["RENT_ITEM_D1"] != 29411 || terms.items["RENT_ITEM_D7"] != 29405 {
		t.Fatalf("items = %v, want the attachment and the hat", terms.items)
	}
	if !terms.windows["RENT_ITEM_WINDOW"] {
		t.Fatalf("windows = %v, want RENT_ITEM_WINDOW", terms.windows)
	}
	for _, code := range []string{"RENT_ITEM_SHORT", "RENT_ITEM_BAD", "RENT_ITEM_NO_ITEM"} {
		if terms.periods[code] != 0 || terms.windows[code] {
			t.Fatalf("%s resolved; want it skipped", code)
		}
	}
}

/*
================
TestRentTermStampsItsContent

A rented scrap delivers with its refrentitem period on the content;
a term with no row, a fixed-window term, and a term renting another
item all fail hydration instead (a rental without a clock would ship
permanently, and one whose row names another item sends a deadline the
wire does not measure that item's rows for).
================
*/
func TestRentTermStampsItsContent(t *testing.T) {
	tier.Long(t, "a rented scrap resolves its refrentitem period")
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	rondo, ok := items.ItemRefByCodename("ITEM_ETC_ARCHEMY_RONDO_02")
	if !ok || rondo == nil {
		t.Fatal("fixture item ITEM_ETC_ARCHEMY_RONDO_02 missing from the shipped textdata")
	}
	for _, test := range []struct {
		name    string
		term    string
		rents   rentTerms
		minutes uint32
		refusal string
	}{
		{"daily", "RENT_ITEM_D1", parseRentTerms([][]string{rentTermRow("RENT_ITEM_D1", rondo.RefObjID, "1440")}), 1440, ""},
		{"no row", "RENT_ITEM_MISSING", parseRentTerms(nil), 0, "has no refrentitem row"},
		{"window", "RENT_ITEM_WINDOW", parseRentTerms([][]string{rentTermRow("RENT_ITEM_WINDOW", rondo.RefObjID, "0")}), 0, "fixed window"},
		// r204l: the row named the later client's id for the item.
		{"another item", "RENT_ITEM_D1", parseRentTerms([][]string{rentTermRow("RENT_ITEM_D1", rondo.RefObjID+1, "1440")}), 0, "rents item"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := buildMallCatalog(rentScrapFixture(test.term))
			if err != nil {
				t.Fatal(err)
			}
			catalog.rents = test.rents
			err = catalog.Hydrate(items)
			if test.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), test.refusal) {
					t.Fatalf("Hydrate = %v, want a refusal naming %q", err, test.refusal)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Offers) != 1 || len(catalog.Offers[0].Contents) != 1 {
				t.Fatalf("offers = %+v, want one hydrated offer", catalog.Offers)
			}
			if got := catalog.Offers[0].Contents[0].RentMinutes; got != test.minutes {
				t.Fatalf("RentMinutes = %d, want %d", got, test.minutes)
			}
		})
	}
}
