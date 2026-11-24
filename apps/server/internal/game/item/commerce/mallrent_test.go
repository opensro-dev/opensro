/*
===========================================================================

mallrent_test.go - LoadMall reads the tree's refrentitem (issue #4240)

===========================================================================
*/
package commerce

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// staticRentRefs is the one-item reference source the fixture's rented
// scrap resolves against.
/*
================
staticRentRefs
================
*/
type staticRentRefs struct{ ref *enterworld.ItemRef }

/*
================
ItemRefByCodename
================
*/
func (s staticRentRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if codename == s.ref.Codename {
		return s.ref, true
	}
	return nil, false
}

/*
================
ItemRefByID
================
*/
func (s staticRentRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	if id == s.ref.RefObjID {
		return s.ref, true
	}
	return nil, false
}

/*
================
writeMallTables
================
*/
func writeMallTables(t *testing.T, dir string, tables map[string][][]string) {
	t.Helper()
	for name, rows := range tables {
		var lines []string
		for _, row := range rows {
			lines = append(lines, strings.Join(row, "\t"))
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
TestLoadMallRentTermsReachTheContents

A tree whose refrentitem prices the scrap's term lands the period on
the hydrated content; a tree without the table still loads (no
rentals), and the same scrap then fails hydration instead of shipping
permanently.
================
*/
func TestLoadMallRentTermsReachTheContents(t *testing.T) {
	durability := int64(100)
	refs := staticRentRefs{ref: &enterworld.ItemRef{RefObjID: 4242, Codename: "ITEM", TypeIDs: [4]int64{3, 1, 13, 1},
		VarianceIntMin1c0: &durability}}
	dir := t.TempDir()
	tables := rentScrapFixture("RENT_ITEM_TEST")
	tables["refrentitem"] = [][]string{rentTermRow("RENT_ITEM_TEST", "1440")}
	// The loader requires these two tables to exist; empty is fine.
	tables["refconditiontosellpackageitem"] = nil
	tables["refrewardpolicytosellpackageitem"] = nil
	// The fixture's item is a shipped codename; the rented scrap names
	// the test reference's codename instead.
	tables["refscrapofpackageitem"][0][3] = "ITEM"
	writeMallTables(t, dir, tables)
	catalog, err := LoadMall(dir, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(refs); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Offers[0].Contents[0].RentMinutes; got != 1440 {
		t.Fatalf("RentMinutes = %d, want 1440", got)
	}

	bare := t.TempDir()
	delete(tables, "refrentitem")
	writeMallTables(t, bare, tables)
	catalog, err = LoadMall(bare, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(refs); err == nil || !strings.Contains(err.Error(), "has no refrentitem row") {
		t.Fatalf("bare tree hydration = %v, want the missing-row refusal", err)
	}
}
