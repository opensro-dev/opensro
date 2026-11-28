/*
===========================================================================

catalog_v1696_test.go - the v1.696 Magic POP tables

The 20-column prize table with its [BIIV] specs and cap column, the
six-column NPC map, the select window's tree, the voucher pairs a v1.696
catalog exchanges (its own and the v1.150 table's) and the published
pool. The fixture is testdata/v1696.

===========================================================================
*/

package gacha

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

const v1696Fixture = "testdata/v1696"

/*
================
v1696Items

The shared fixture items plus a second equipment row (4142) and a
lose-set consumable (4143).
================
*/
func v1696Items() testItems {
	items := fixtureItems()
	items["REWARD_C"] = &enterworld.ItemRef{RefObjID: 4142, Codename: "REWARD_C", TypeIDs: [4]int64{3, 1, 6, 2}}
	items["REWARD_D"] = &enterworld.ItemRef{RefObjID: 4143, Codename: "REWARD_D", TypeIDs: [4]int64{3, 3, 1, 1}}
	return items
}

/*
================
copyV1696Fixture

The fixture tables in a scratch directory, with one table replaced, so a
test can break one file without touching the committed copy.
================
*/
func copyV1696Fixture(t *testing.T, replace map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(v1696Fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(v1696Fixture, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if body, ok := replace[entry.Name()]; ok {
			data = []byte(body)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
TestV1696CatalogReadsSetsSpecsAndCap
================
*/
func TestV1696CatalogReadsSetsSpecsAndCap(t *testing.T) {
	if !HasV1696Tables(v1696Fixture) {
		t.Fatal("fixture tables not found")
	}
	catalog, err := LoadCatalogV1696(v1696Fixture, v1696Items())
	if err != nil {
		t.Fatalf("LoadCatalogV1696: %v", err)
	}
	if !catalog.V1696 || !catalog.HasNpc(9251) || catalog.VisibleSetForNpc(9251) != 1 {
		t.Fatal("the NPC map's visible set was not admitted")
	}
	if _, ok := catalog.PrizeForNpc(9251, 4); ok {
		t.Fatal("a lose-set (column 4) entry was offered for selection")
	}
	prize, ok := catalog.PrizeForNpc(9251, 2)
	if !ok || prize.MinLevelCap != degree10OpensAt {
		t.Fatalf("entry 2 = %+v ok=%v, want the degree-10 cap column 91 opened at %d", prize, ok, degree10OpensAt)
	}
	var order []uint32
	for _, visible := range catalog.VisiblePrizes(9251) {
		order = append(order, visible.EntryID)
	}
	if !reflect.DeepEqual(order, []uint32{1, 2, 3}) {
		t.Fatalf("visible order %v, want table order 1 2 3", order)
	}
	spec := catalog.SpecForPair(4140, 1)
	plus, hasPlus := spec.Plus()
	options := spec.MagicOptions()
	if !hasPlus || plus != 3 || len(options) != 2 || options[0].Name != "str" || options[1].Tag != 0x696E74 || options[1].Value != 3 {
		t.Fatalf("entry 1 spec %+v", spec)
	}
	if got := catalog.SpecForPair(4141, 5); len(got.Tokens) != 0 {
		t.Fatalf("a type-0 row carries spec %+v", got)
	}
	if tree := catalog.Tree(); len(tree) != 2 || tree[0].TypeID != [4]uint8{3, 1, 6, 2} || tree[0].Degrees != 15 || tree[1].SubLabel != "UIIT_CTL_WK_ETC_ETC" {
		t.Fatalf("tree %+v", tree)
	}
}

/*
================
TestV1696CatalogExchangesLegacyPairs

A voucher won from the v1.150 table before the switch still exchanges;
an invented pair does not.
================
*/
func TestV1696CatalogExchangesLegacyPairs(t *testing.T) {
	catalog, err := LoadCatalogV1696(v1696Fixture, v1696Items())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref      uint32
		quantity uint64
		want     bool
	}{
		{4141, 5, true},   // v1.696 visible row
		{4141, 2, true},   // v1.696 lose row
		{4141, 50, true},  // v1.150 lose row
		{4140, 1, true},   // both tables
		{4141, 7, false},  // no row
		{4143, 50, false}, // the lose ref at another quantity
	} {
		if got := catalog.IsPrizePair(tc.ref, tc.quantity); got != tc.want {
			t.Fatalf("IsPrizePair(%d, %d) = %v, want %v", tc.ref, tc.quantity, got, tc.want)
		}
	}
	if err := catalog.ApplyPoolV2(v1696Items()); err == nil {
		t.Fatal("pool v2 applied to the v1.696 table")
	}
}

/*
================
TestV1696CatalogRefusesMalformedTables
================
*/
func TestV1696CatalogRefusesMalformedTables(t *testing.T) {
	row := func(reward, spec string) string {
		return "1\t1\t" + reward + "\t3000\t1\t1\t0\t1\t" + spec + "\t0\txxx\t0\txxx\t0\txxx\t1\t1\t1\t1\t1\n" +
			"1\t2\t4143\t50\t1\t4\t0\t0\txxx\t0\txxx\t0\txxx\t0\txxx\t1\t50\t50\t50\t50\n"
	}
	for name, prizes := range map[string]string{
		"short row":             "1\t1\t4140\t3000\t1\t1\t0\t0\txxx\t0\txxx\t0\txxx\t0\txxx\t1\n",
		"spec on consumable":    row("4141", "[BIIV]<O:3>"),
		"repeated letter":       row("4140", "[BIIV]<O:3><O:4>"),
		"unknown letter":        row("4140", "[BIIV]<X:3>"),
		"missing marker":        row("4140", "<O:3>"),
		"six tokens":            row("4140", "[BIIV]<M:str,1,1><M:int,1,1><M:dex,1,1><M:hp,1,1><M:mp,1,1><O:1>"),
		"unknown spec type":     strings.Replace(row("4140", "xxx"), "\t1\txxx\t0\txxx", "\t2\txxx\t0\txxx", 1),
		"conflicting spec pair": row("4140", "[BIIV]<O:3>") + "1\t1\t4140\t10\t1\t9\t0\t1\t[BIIV]<O:4>\t0\txxx\t0\txxx\t0\txxx\t1\t1\t1\t1\t1\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyV1696Fixture(t, map[string]string{PrizeTableV1696: prizes})
			if _, err := LoadCatalogV1696(dir, v1696Items()); err == nil {
				t.Fatal("malformed table accepted")
			}
		})
	}
	dir := copyV1696Fixture(t, map[string]string{TreeTableV1696: "0\t3\t1\t6\t2\t100\tA\t101\tB\t15\n"})
	if _, err := LoadCatalogV1696(dir, v1696Items()); err == nil {
		t.Fatal("an empty tree was accepted")
	}
}

/*
================
TestV1696LegacyPairsReadLikeTheNativeLoader

The v1.150 table is read the way its own loader reads it: a commented or
blank line and a row whose first column is not 1 add no pair, while an
enabled row does; a numeric column that does not parse fails the catalog.
================
*/
func TestV1696LegacyPairsReadLikeTheNativeLoader(t *testing.T) {
	table := "// 1\t1\t4141\t9\t1\t3\n" +
		"# 1\t1\t4142\t9\t1\t4\n" +
		"\n" +
		"0\t1\t4142\t9\t1\t4\n" +
		"1\t1\t4140\t9\t1\t1\n" +
		"1\t2\t4141\t10000\t50\t13\n"
	dir := copyV1696Fixture(t, map[string]string{PrizeTableV1150: table})
	catalog, err := LoadCatalogV1696(dir, v1696Items())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref      uint32
		quantity uint64
		want     bool
	}{
		{4140, 1, true},  // the enabled row, also a v1.696 pair
		{4141, 50, true}, // the enabled lose row
		{4141, 1, false}, // the commented row
		{4142, 9, false}, // the disabled row
	} {
		if got := catalog.IsPrizePair(tc.ref, tc.quantity); got != tc.want {
			t.Fatalf("IsPrizePair(%d, %d) = %v, want %v", tc.ref, tc.quantity, got, tc.want)
		}
	}

	broken := copyV1696Fixture(t, map[string]string{PrizeTableV1150: "1\t1\t4140\t9\tone\t1\n"})
	if _, err := LoadCatalogV1696(broken, v1696Items()); err == nil {
		t.Fatal("a non-numeric legacy quantity was accepted")
	}
}

/*
================
TestV1696PublicPoolShape

The JSON the browser's window reads (the contract client-next decodes).
================
*/
func TestV1696PublicPoolShape(t *testing.T) {
	catalog, err := LoadCatalogV1696(v1696Fixture, v1696Items())
	if err != nil {
		t.Fatal(err)
	}
	// The magicoption rows the two M tokens resolve to.
	optionID := func(token SpecToken) (uint16, bool) {
		switch token.Tag {
		case 0x737472:
			return 11, true
		case 0x696E74:
			return 140, true
		}
		return 0, false
	}
	data, err := json.Marshal(catalog.PublicPool(90, optionID))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"npc":9251,"visibleSet":1,"levelCap":90,"entries":[` +
		`{"entry":1,"refObjId":4140,"quantity":1,"minLevelCap":1,"plus":3,"options":[{"name":"str","value":3,"paramId":11},{"name":"int","value":3,"paramId":140}]},` +
		`{"entry":2,"refObjId":4142,"quantity":1,"minLevelCap":90,"plus":5,"options":[]},` +
		`{"entry":3,"refObjId":4141,"quantity":5,"minLevelCap":1,"plus":0,"options":[]}],"tree":[` +
		`{"tid":[3,1,6,2],"group":100,"groupLabel":"UIIT_CTL_WK_CH_WEAPON","sub":101,"subLabel":"UIIT_CTL_WK_CH_WEAPON_SWORD","degrees":15},` +
		`{"tid":[3,3,1,1],"group":700,"groupLabel":"UIIT_CTL_WK_ETC_ETC","sub":712,"subLabel":"UIIT_CTL_WK_ETC_ETC","degrees":0}]}`
	if string(data) != want {
		t.Fatalf("pool JSON\n got %s\nwant %s", data, want)
	}
	// Without a magic option table the token publishes its name and value
	// alone; the browser then draws no blue line.
	unresolved, err := json.Marshal(catalog.PublicPool(90, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unresolved), "paramId") {
		t.Fatalf("a pool without option rows published a paramId: %s", unresolved)
	}
	v1150, err := LoadCatalog(v1696Fixture, v1696Items())
	if err != nil {
		t.Fatal(err)
	}
	if v1150.PublicPool(90, optionID) != nil {
		t.Fatal("the v1.150 catalog published a pool")
	}
}
