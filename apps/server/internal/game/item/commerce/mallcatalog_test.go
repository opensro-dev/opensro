/*
===========================================================================

mallcatalog_test.go - complete authored mall coverage and native shop addresses

===========================================================================
*/
package commerce

import (
	"opensro.online/server/internal/game/enterworld"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestShippedMallCatalog
================
*/
func TestShippedMallCatalog(t *testing.T) {
	catalog, err := LoadMall(gamedatatest.TextdataDir(t))
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
	if _, err := LoadMall(t.TempDir()); err == nil {
		t.Fatal("accepted missing reference data")
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
