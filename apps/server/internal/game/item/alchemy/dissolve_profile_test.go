package alchemy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
)

// Portable fixture for admission and transaction tests. PublishedCatalog tests
// independently admit the actual v1.150 textdata, not these generated references.
func dissolveProfileFixture(t *testing.T) (*Catalog, []inventory.Item) {
	t.Helper()
	c, items := fixture()
	items = items[:1]
	var p struct {
		Rows []struct {
			Codename string
			Degree   int
		}
	}
	if err := json.Unmarshal(dissolveV1150, &p); err != nil {
		t.Fatal(err)
	}
	for i, row := range p.Rows {
		flags := uint16(0x0dec)
		if strings.Contains(row.Codename, "ATTRSTONE") {
			flags = 0x15ec
		}
		c.Items[row.Codename] = Reference{ID: uint32(100 + i), Name: row.Codename, Flags: flags, Class: row.Degree, Stack: 1, Params: [5]uint32{1, 0x0a141e00}}
	}
	for degree := 1; degree <= 12; degree++ {
		for k, kind := range []string{"EARTH", "WATER", "FIRE", "WIND"} {
			name := fmt.Sprintf("ITEM_ETC_ARCHEMY_ELEMENT_%s_%02d", kind, degree)
			c.Items[name] = Reference{ID: uint32(1000 + degree*4 + k), Name: name, Flags: 0x2dec, Class: degree, Stack: 5000}
		}
	}
	name := "ITEM_ETC_ARCHEMY_RONDO_02"
	c.Items[name] = Reference{ID: 2000, Name: name, Flags: 0x35ec, Stack: 5000}
	items = append(items, inventory.Item{Slot: 14, RefObjID: 2000, Codename: name, TypeFlags: 0x35ec, Quantity: 100})
	r := c.Items["weapon"]
	r.Price = 1000000
	c.Items[r.Name] = r
	c.Magic[99] = Magic{ID: 99}
	items[0].MagicOptions = []uint64{99}
	if err := c.loadDissolveProfile(); err != nil {
		t.Fatal(err)
	}
	return c, items
}

func TestDissolveProfileWeightsAndExclusions(t *testing.T) {
	c, _ := dissolveProfileFixture(t)
	for degree := 1; degree <= 12; degree++ {
		pool := c.DissolveDrops[degree]
		if len(pool.Attribute) != 13 || len(pool.Magic) != 16 {
			t.Fatal(degree, pool)
		}
		for kind, choices := range [][]WeightedStone{pool.Attribute, pool.Magic} {
			var start uint32
			for _, choice := range choices {
				if strings.Contains(choice.Codename, "ASTRAL") || strings.Contains(choice.Codename, "ATHANASIA") {
					t.Fatal("zero-weight reward admitted", choice)
				}
				for _, n := range []uint32{start, start + choice.Weight - 1} {
					got, err := c.selectDissolveStone(choices, degree, kind, sequence(t, n))
					if err != nil || got.Name != choice.Codename {
						t.Fatal(degree, n, got, err)
					}
				}
				start += choice.Weight
			}
		}
	}
}

func TestDissolveAssimilationUsesBothParametersAndAdmitsEmptyDistribution(t *testing.T) {
	r := Reference{Params: [5]uint32{1, 0x0a141e00, 0x05230000}}
	for i, want := range []uint8{30, 20, 10, 35, 5} {
		got, err := dissolveAssimilation(r, sequence(t, uint32(i)))
		if err != nil || got != want {
			t.Fatal(i, got, err)
		}
	}
	// Manufacture must still draw from the first three values only.
	got, err := stoneAssimilation(r, sequence(t, 3))
	if err != nil || got != 30 {
		t.Fatal(got, err)
	}
	r.Params = [5]uint32{}
	got, err = dissolveAssimilation(r, func() (uint32, error) { t.Fatal("empty distribution drew randomness"); return 0, nil })
	if err != nil || got != 0 {
		t.Fatal(got, err)
	}
	if _, err := stoneAssimilation(r, nil); err == nil {
		t.Fatal("manufacture guard weakened")
	}
}

func TestDissolveProfileAdmissionIsAtomic(t *testing.T) {
	for _, mutation := range []string{"missing", "grade", "type", "stack", "element"} {
		t.Run(mutation, func(t *testing.T) {
			c, _ := dissolveProfileFixture(t)
			before := c.DissolveDrops
			name := "ITEM_ETC_ARCHEMY_MAGICSTONE_STR_01"
			r := c.Items[name]
			switch mutation {
			case "missing":
				delete(c.Items, name)
			case "grade":
				r.Class = 2
				c.Items[name] = r
			case "type":
				r.Flags = 0x256c
				c.Items[name] = r
			case "stack":
				r.Stack = 0
				c.Items[name] = r
			case "element":
				delete(c.Items, "ITEM_ETC_ARCHEMY_ELEMENT_WIND_12")
			}
			if err := c.loadDissolveProfile(); err == nil || !reflect.DeepEqual(before, c.DissolveDrops) {
				t.Fatal("partial/invalid profile published", err)
			}
		})
	}
}

func TestDissolveInferredRewardsAndLateFailuresAreAtomic(t *testing.T) {
	for _, failure := range []string{"", "entropy", "capacity", "rondo"} {
		t.Run(failure, func(t *testing.T) {
			c, items := dissolveProfileFixture(t)
			roll := Roll(func() (uint32, error) { return 0, nil })
			if failure == "rondo" {
				items[1].Quantity = 50
			}
			if failure == "capacity" {
				for s := uint8(15); s < domain.DefaultInventorySize; s++ {
					items = append(items, inventory.Item{Slot: s, RefObjID: 9999, Quantity: 1})
				}
			}
			if failure == "entropy" {
				calls := 0
				roll = func() (uint32, error) {
					calls++
					if calls == 13 {
						return 0, fmt.Errorf("last magic draw unavailable")
					}
					return 0, nil
				}
			}
			before := append([]inventory.Item(nil), items...)
			out, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{14, 13}}, roll)
			if !reflect.DeepEqual(before, items) {
				t.Fatal("planner mutated inventory")
			}
			if failure != "" {
				if err == nil {
					t.Fatal("accepted failed transaction")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			counts := map[uint16]int{}
			for _, row := range out.Items {
				counts[row.TypeFlags]++
				if row.Codename == "weapon" {
					t.Fatal("equipment survived")
				}
				if row.Codename == "ITEM_ETC_ARCHEMY_RONDO_02" && row.Quantity != 49 {
					t.Fatal(row)
				}
			}
			if counts[0x15ec] != 2 || counts[0x0dec] != 2 || counts[0x2dec] != 2 || out.Completed != 1 {
				t.Fatal(counts, out)
			}
		})
	}
}
