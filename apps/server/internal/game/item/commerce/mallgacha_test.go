/*
===========================================================================

mallgacha_test.go - the Magic Pop card's mall listing (port-only, #4360)

The later mall lists PACKAGE_ITEM_MALL_GACHA_CARD; while the unique pop
ticket runs the card is uniques-only and the configured mall withholds
the package (action.ConfigureMall). WithholdPackage is the removal this
package owns: it drops exactly the named package's offers and leaves
every other listing and tab standing. The listing itself is exercised
through a fixture catalogue, since the tree a test runs against may
predate the later-goods build that stamps the card's rows.

===========================================================================
*/
package commerce

import (
	"testing"
)

/*
================
gachaMallTables

One MALL_CONSUME_SPECIAL shelf holding the Magic Pop card beside a
neighbouring package, in the fixture shape mallFixtureTables uses.
================
*/
func gachaMallTables() map[string][][]string {
	scrap := func(packageCode, item string) []string {
		row := []string{"1", "15", packageCode, item}
		for len(row) < 20 {
			row = append(row, "0")
		}
		return row
	}
	return map[string][][]string{
		"refshopgroup":          {{"1", "15", "852", "GROUP_MALL"}},
		"refmappingshopgroup":   {{"1", "15", "GROUP_MALL", "MALL_CONSUME"}},
		"refmappingshopwithtab": {{"1", "15", "MALL_CONSUME", "MALL_TABS"}},
		"refshoptab":            {{"1", "15", "1", "MALL_CONSUME_SPECIAL", "MALL_TABS", "Special"}},
		"refshopgoods": {
			{"1", "15", "MALL_CONSUME_SPECIAL", MallGachaCardPackage, "0"},
			{"1", "15", "MALL_CONSUME_SPECIAL", "PACKAGE_NEIGHBOUR", "1"},
		},
		"refpackageitem": {
			{"1", "15", "100", MallGachaCardPackage, "0", "0", "Name", "Description", "item/test.ddj"},
			{"1", "15", "101", "PACKAGE_NEIGHBOUR", "0", "0", "Name", "Description", "item/test.ddj"},
		},
		"refpricepolicyofitem": {
			{"1", "15", MallGachaCardPackage, "2", "30"},
			{"1", "15", "PACKAGE_NEIGHBOUR", "2", "20"},
		},
		"refscrapofpackageitem": {
			scrap(MallGachaCardPackage, "ITEM_MALL_GACHA_CARD"),
			scrap("PACKAGE_NEIGHBOUR", "ITEM_NEIGHBOUR"),
		},
		"refconditiontosellpackageitem":    nil,
		"refrewardpolicytosellpackageitem": nil,
	}
}

/*
================
TestWithholdPackageDropsOnlyTheGachaCard
================
*/
func TestWithholdPackageDropsOnlyTheGachaCard(t *testing.T) {
	dir := t.TempDir()
	writeMallTables(t, dir, gachaMallTables())
	catalog, err := LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Offers) != 2 || len(catalog.Tabs) != 1 {
		t.Fatalf("fixture mall is %d offers / %d tabs, want 2 / 1", len(catalog.Offers), len(catalog.Tabs))
	}
	if removed := catalog.WithholdPackage(MallGachaCardPackage); removed != 1 {
		t.Fatalf("withheld %d offers, want the one gacha card package", removed)
	}
	if len(catalog.Offers) != 1 || len(catalog.Tabs) != 1 {
		t.Fatalf("mall is %d offers / %d tabs after the withhold, want 1 / 1",
			len(catalog.Offers), len(catalog.Tabs))
	}
	if catalog.Offers[0].Code != "PACKAGE_NEIGHBOUR" {
		t.Fatalf("surviving offer is %q, want the neighbour", catalog.Offers[0].Code)
	}
	if removed := catalog.WithholdPackage(MallGachaCardPackage); removed != 0 {
		t.Fatalf("second withhold removed %d, want 0", removed)
	}
	if removed := catalog.WithholdPackage("PACKAGE_ABSENT"); removed != 0 {
		t.Fatalf("absent withhold removed %d, want 0", removed)
	}
}
