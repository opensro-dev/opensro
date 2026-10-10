/*
===========================================================================

siegeforge_test.go - the forge projection against the shipped table

===========================================================================
*/
package enterworld

import (
	"path/filepath"
	"strconv"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestSiegeItemForgeProjectionMatchesTheTable

Every enabled siegefortressitemforge.txt row appears once with its gold,
guild points and minutes, in file order, and each item sits in exactly one
staff member's vector.
================
*/
func TestSiegeItemForgeProjectionMatchesTheTable(t *testing.T) {
	rows := ReadTextdataFile(filepath.Join(licensed.RetailTextdataDir(t), "siegefortressitemforge.txt"))
	if len(rows) == 0 {
		t.Skip("shipped siegefortressitemforge.txt unavailable")
	}
	groups := DefaultSiegeItemForgeGroups()
	if len(groups) != 1 {
		t.Fatalf("groups %d, want 1", len(groups))
	}
	items := groups[0].Items
	i := 0
	for _, r := range rows {
		if r[0] != "1" {
			continue
		}
		var want [4]uint32
		for j := range want {
			v, err := strconv.ParseUint(r[2+j], 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			want[j] = uint32(v)
		}
		if i >= len(items) || items[i] != (SiegeItemForgeItem{want[0], want[1], want[2], want[3]}) {
			t.Fatalf("row %d = %v, projection %+v", i, want, items)
		}
		i++
	}
	if i != len(items) {
		t.Fatalf("projection has %d items, table %d", len(items), i)
	}
	staff := map[uint32]int{}
	for _, ref := range append(append([]uint32{}, groups[0].SmithItemRefs...), groups[0].TrainerItemRefs...) {
		staff[ref]++
	}
	for _, item := range items {
		if staff[item.RefObjID] != 1 {
			t.Fatalf("item %d sits in %d staff vectors", item.RefObjID, staff[item.RefObjID])
		}
	}
}
