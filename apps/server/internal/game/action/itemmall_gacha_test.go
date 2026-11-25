/*
===========================================================================

itemmall_gacha_test.go - the Magic Pop card is uniques-only (#4360)

Port-only, not native (owner decision 2026-11-25): while the unique pop
ticket runs (SRO_UNIQUE_POP_TICKET), uniques drop the Magic Pop card
and the Item Mall does not sell it, so ConfigureMall withholds its
package. With the ticket off the configured mall lists the card, as
the later mall does. The card item itself is untouched either way.

The listing is exercised through a fixture catalogue: the tree a test
runs against may predate the later-goods build that stamps the card's
mall rows into the built textdata.

===========================================================================
*/
package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
writeGachaMallDir

A one-shelf mall (MALL_CONSUME_SPECIAL) listing the Magic Pop card
beside a neighbour, in the table shape the commerce fixtures use.
================
*/
func writeGachaMallDir(t *testing.T) string {
	t.Helper()
	scrap := func(packageCode, item string) string {
		row := []string{"1", "15", packageCode, item}
		for len(row) < 20 {
			row = append(row, "0")
		}
		return strings.Join(row, "\t")
	}
	tables := map[string][]string{
		"refshopgroup":          {"1\t15\t852\tGROUP_MALL"},
		"refmappingshopgroup":   {"1\t15\tGROUP_MALL\tMALL_CONSUME"},
		"refmappingshopwithtab": {"1\t15\tMALL_CONSUME\tMALL_TABS"},
		"refshoptab":            {"1\t15\t1\tMALL_CONSUME_SPECIAL\tMALL_TABS\tSpecial"},
		"refshopgoods": {
			"1\t15\tMALL_CONSUME_SPECIAL\tPACKAGE_ITEM_MALL_GACHA_CARD\t0",
			"1\t15\tMALL_CONSUME_SPECIAL\tPACKAGE_NEIGHBOUR\t1",
		},
		"refpackageitem": {
			"1\t15\t100\tPACKAGE_ITEM_MALL_GACHA_CARD\t0\t0\tName\tDescription\titem/test.ddj",
			"1\t15\t101\tPACKAGE_NEIGHBOUR\t0\t0\tName\tDescription\titem/test.ddj",
		},
		"refpricepolicyofitem": {
			"1\t15\tPACKAGE_ITEM_MALL_GACHA_CARD\t2\t30",
			"1\t15\tPACKAGE_NEIGHBOUR\t2\t20",
		},
		"refscrapofpackageitem": {
			scrap("PACKAGE_ITEM_MALL_GACHA_CARD", "ITEM_MALL_GACHA_CARD"),
			scrap("PACKAGE_NEIGHBOUR", "ITEM_ETC_GOLD_02"),
		},
		"refconditiontosellpackageitem":    {},
		"refrewardpolicytosellpackageitem": {},
	}
	dir := t.TempDir()
	for name, lines := range tables {
		body := strings.Join(lines, "\n")
		if len(lines) > 0 {
			body += "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
TestMallWithholdsTheGachaCardWhileThePopTicketRuns
================
*/
func TestMallWithholdsTheGachaCardWhileThePopTicketRuns(t *testing.T) {
	tier.Long(t, "the configured mall with and without the unique pop ticket")
	dir := writeGachaMallDir(t)
	items := testItems()
	stack := enterworld.NewNativeFields(map[string]float64{"maxStack": 50})
	items["ITEM_ETC_GOLD_02"] = &enterworld.ItemRef{
		RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeIDs: [4]int64{3, 3, 5, 2},
		NativeFields: stack,
	}
	items["ITEM_MALL_GACHA_CARD"] = &enterworld.ItemRef{
		RefObjID: 9238, Codename: "ITEM_MALL_GACHA_CARD", TypeIDs: [4]int64{3, 3, 14, 1},
		NativeFields: stack,
	}
	for _, tc := range []struct {
		name       string
		popTicket  bool
		wantListed bool
	}{
		{"pop ticket on", true, false},
		{"pop ticket off", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _ := newTestRuntime(testCharacter(), items)
			rt.UniqueRules = progression.UniqueRules{PopTicket: tc.popTicket}
			if err := rt.ConfigureMall(dir, &mallTestAuthority{}, nil, false); err != nil {
				t.Fatal(err)
			}
			listed, neighbours := false, 0
			for _, offer := range rt.mallCatalog.Offers {
				switch offer.Code {
				case "PACKAGE_ITEM_MALL_GACHA_CARD":
					listed = true
				case "PACKAGE_NEIGHBOUR":
					neighbours++
				}
			}
			if listed != tc.wantListed {
				t.Fatalf("pop ticket %v: gacha card listed = %v, want %v",
					tc.popTicket, listed, tc.wantListed)
			}
			if neighbours != 1 {
				t.Fatalf("pop ticket %v: neighbour listed %d times, want 1", tc.popTicket, neighbours)
			}
		})
	}
}
