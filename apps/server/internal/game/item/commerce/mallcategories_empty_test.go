/*
===========================================================================

mallcategories_empty_test.go - the layout writes no tab without goods

Port-only, not native (#4846). The category layout's goods are gated
(Imported, Nasrun), so a layout tab can end with nothing to sell. The
v1.150 mall draws one tab button per tab row and never counts the goods
(CIFItemMallShop_CreateTabs 0x6c76f0), so an empty tab would show as an
empty page. dropEmptyLayoutTabs takes such tabs back, and a layout shop
whose tabs all went goes with them. These tests drive it on small
tables, where no shipped data fills the HOT shop.

===========================================================================
*/
package commerce

import "testing"

/*
================
layoutTablesWithGoods

Tables as addRows leaves them before the clean-up: the shipped Premium
shop with its own tab, the layout's appended tabs and the new HOT shop,
and goods in the tabs named.
================
*/
func layoutTablesWithGoods(filled ...string) map[string][][]string {
	tables := map[string][][]string{
		"refmappingshopgroup": {
			{"1", "11", mallGroupCode, "MALL_PREMIUM"},
			{"1", "11", mallGroupCode, "MALL_HOT"},
		},
		"refmappingshopwithtab": {
			{"1", "11", "MALL_PREMIUM", "MALL_PREMIUM_GROUP1"},
			{"1", "11", "MALL_HOT", "MALL_HOT_GROUP1"},
		},
		"refshoptab":   {{"1", "11", "0", "MALL_PREMIUM_PREMIUM", "MALL_PREMIUM_GROUP1", "UIIT_STT_SILKMALL_PREMIUM"}},
		"refshopgoods": {{"1", "11", "MALL_PREMIUM_PREMIUM", "PACKAGE_SHIPPED", "0"}},
	}
	for _, tab := range mallCategoryTabs {
		tables["refshoptab"] = append(tables["refshoptab"], []string{"1", "11", "0", tab.Code, tab.Group, tab.Label})
	}
	for index, code := range filled {
		tables["refshopgoods"] = append(tables["refshopgoods"], []string{"1", "11", code, "PACKAGE_" + code, string(rune('0' + index))})
	}
	return tables
}

/*
================
tabCodes
================
*/
func tabCodes(tables map[string][][]string) map[string]bool {
	codes := map[string]bool{}
	for _, row := range tables["refshoptab"] {
		codes[row[3]] = true
	}
	return codes
}

/*
================
TestLayoutDropsOnlyItsEmptyTabs

Gears without goods goes; Others with goods and the shipped Premium tab
stay, and so does the Premium shop, which is shipped.
================
*/
func TestLayoutDropsOnlyItsEmptyTabs(t *testing.T) {
	tables := layoutTablesWithGoods("MALL_PREMIUM_OTHERS", "MALL_HOT_MODELS")
	dropEmptyLayoutTabs(tables, map[string]bool{"MALL_HOT": true})
	codes := tabCodes(tables)
	for _, code := range []string{"MALL_PREMIUM_PREMIUM", "MALL_PREMIUM_OTHERS", "MALL_HOT_MODELS"} {
		if !codes[code] {
			t.Fatalf("tab %s with goods was dropped: %v", code, codes)
		}
	}
	for _, code := range []string{"MALL_PREMIUM_GEARS", "MALL_HOT_EXCLUSIVE", "MALL_ARCHEMY_OTHERS"} {
		if codes[code] {
			t.Fatalf("empty layout tab %s was written: %v", code, codes)
		}
	}
	if len(tables["refmappingshopgroup"]) != 2 || len(tables["refmappingshopwithtab"]) != 2 {
		t.Fatalf("a shop with a filled tab lost its mapping: %v / %v",
			tables["refmappingshopgroup"], tables["refmappingshopwithtab"])
	}
}

/*
================
TestLayoutDropsAShopItLeftEmpty

A new layout shop whose tabs all ended empty leaves with them: its
group and tab-group mappings go, so buildMallCatalog never meets a shop
without tabs. The shipped Premium shop stays whatever the layout did.
================
*/
func TestLayoutDropsAShopItLeftEmpty(t *testing.T) {
	tables := layoutTablesWithGoods()
	dropEmptyLayoutTabs(tables, map[string]bool{"MALL_HOT": true})
	if codes := tabCodes(tables); len(codes) != 1 || !codes["MALL_PREMIUM_PREMIUM"] {
		t.Fatalf("tabs left = %v, want only the shipped Premium tab", codes)
	}
	for _, name := range []string{"refmappingshopgroup", "refmappingshopwithtab"} {
		rows := tables[name]
		if len(rows) != 1 || (name == "refmappingshopgroup" && rows[0][3] != "MALL_PREMIUM") ||
			(name == "refmappingshopwithtab" && rows[0][2] != "MALL_PREMIUM") {
			t.Fatalf("%s = %v, want only the shipped Premium shop", name, rows)
		}
	}
}
