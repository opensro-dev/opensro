package alchemy

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

func fixture() (*Catalog, []inventory.Item) {
	c := &Catalog{Items: map[string]Reference{}, Magic: map[uint16]Magic{}}
	c.Items["weapon"] = Reference{ID: 1, Name: "weapon", Flags: wire.PackTypeFlags(3, 1, 6, 2), Class: 1}
	c.Items["elixir"] = Reference{ID: 2, Name: "elixir", Flags: wire.PackTypeFlags(3, 3, 10, 1), Params: [5]uint32{6 << 24, 420744970, 168430090, 168101125, 0xffffffff}}
	c.Items["powder"] = Reference{ID: 3, Name: "powder", Flags: wire.PackTypeFlags(3, 3, 10, 2), Params: [5]uint32{1, 840832008, 134744072, 134744072, 0xffffffff}}
	c.Magic[1] = Magic{ID: 1, Name: "MATTR_DEC_MAXDUR", Degree: 3, Tag: 0x64757261, Params: [3]uint32{7, 36, 50}, Categories: []string{"weapon"}}
	for i, tag := range []uint32{0x61746861, 0x736f6c69, 0x61737472, 0x6c75636b} {
		id := uint16(i + 2)
		c.Magic[id] = Magic{ID: id, Tag: tag}
	}
	items := []inventory.Item{}
	for i, name := range []string{"weapon", "elixir", "powder"} {
		r := c.Items[name]
		items = append(items, inventory.Item{Slot: uint8(13 + i), RefObjID: r.ID, Codename: name, TypeFlags: r.Flags, Quantity: 1, Durability: 87, VarianceBits: 0xfedcba9876543210})
	}
	items[1].Quantity = 2
	return c, items
}
func sequence(t *testing.T, values ...uint32) Roll {
	t.Helper()
	return func() (uint32, error) {
		t.Helper()
		if len(values) == 0 {
			t.Fatal("unexpected random draw")
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

func TestReinforceNativeBranches(t *testing.T) {
	for _, tc := range []struct {
		name             string
		plus             uint8
		magic            []uint64
		draws            []uint32
		success, destroy bool
		wantPlus         uint8
		wantMagic        []uint64
	}{
		{"success", 0, nil, []uint32{24}, true, false, 1, nil},
		{"strict threshold", 0, nil, []uint32{25}, false, false, 0, nil},
		{"reset", 4, nil, []uint32{99}, false, false, 0, nil},
		{"destroy", 5, nil, []uint32{99, 49}, false, true, 0, nil},
		{"immortal and astral", 5, []uint64{1<<32 | 2, 2<<32 | 4}, []uint32{99, 0}, false, false, 4, []uint64{1<<32 | 4}},
		{"steady", 5, []uint64{1<<32 | 3}, []uint32{99, 50}, false, false, 0, []uint64{}},
		{"durability range minimum", 5, nil, []uint32{99, 50, 0}, false, false, 0, []uint64{36<<32 | 1}},
		{"durability range maximum", 5, nil, []uint32{99, 50, 32767}, false, false, 0, []uint64{50<<32 | 1}},
		{"lucky consumed before failure", 1, []uint64{1<<32 | 5}, []uint32{99}, false, false, 0, []uint64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, items := fixture()
			items[0].Plus = tc.plus
			items[0].MagicOptions = tc.magic
			before := clone(items)
			r, err := c.Reinforce(items, []uint8{13, 14}, 0, sequence(t, tc.draws...))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(items, before) {
				t.Fatal("planner mutated caller inventory")
			}
			if r.Success != tc.success || r.Destroyed != tc.destroy {
				t.Fatalf("outcome %+v", r)
			}
			for _, item := range r.Items {
				if item.Slot == 14 && item.Quantity != 1 {
					t.Fatal("elixir not consumed exactly once")
				}
				if item.Slot != 13 {
					continue
				}
				if tc.destroy {
					t.Fatal("destroyed target retained")
				}
				if item.Plus != tc.wantPlus || !reflect.DeepEqual(item.MagicOptions, tc.wantMagic) {
					t.Fatalf("target %+v", item)
				}
				if item.Durability != 87 || item.VarianceBits != before[0].VarianceBits {
					t.Fatal("unrelated target fields changed")
				}
			}
		})
	}
}

/*
================
TestReinforceNeverDestroysACashItem

The destroy draw that ruins ordinary gear (+5, second draw under 50) only
resets a cash item, as the native failure path tests the cash-item
flag before destroying.
================
*/
func TestReinforceNeverDestroysACashItem(t *testing.T) {
	for _, cash := range []bool{false, true} {
		c, items := fixture()
		weapon := c.Items["weapon"]
		weapon.Cash = cash
		c.Items["weapon"] = weapon
		items[0].Plus = 5
		r, err := c.Reinforce(items, []uint8{13, 14}, 0, sequence(t, 99, 49))
		if err != nil {
			t.Fatal(err)
		}
		if r.Success || r.Destroyed == cash {
			t.Fatalf("cash=%v: outcome %+v", cash, r)
		}
		kept := false
		for _, item := range r.Items {
			if item.Slot == 13 {
				kept = true
				if item.Plus != 0 {
					t.Fatalf("cash=%v: kept item at +%d, want reset to +0", cash, item.Plus)
				}
			}
		}
		if kept == !cash {
			t.Fatalf("cash=%v: target kept=%v", cash, kept)
		}
	}
}

func TestReinforceNoPartialPlanOnRefusalOrRandomFailure(t *testing.T) {
	c, items := fixture()
	before := clone(items)
	for _, slots := range [][]uint8{{13, 13}, {0, 14}, {13, 44}, {13, 15}, {13, 14, 14}} {
		if r, err := c.Reinforce(items, slots, 0, sequence(t)); err == nil || len(r.Items) != 0 {
			t.Fatalf("accepted invalid slots %v", slots)
		}
		if !reflect.DeepEqual(items, before) {
			t.Fatalf("refusing slots %v mutated the inventory", slots)
		}
	}
	items[0].MagicOptions = []uint64{1<<32 | 5}
	before = clone(items)
	r, err := c.Reinforce(items, []uint8{13, 14}, 0, func() (uint32, error) { return 0, errors.New("entropy unavailable") })
	if err == nil || len(r.Items) != 0 || !reflect.DeepEqual(items, before) {
		t.Fatal("random failure committed a lucky charge")
	}
}

func TestPackedProbabilityAndPowderBoundary(t *testing.T) {
	c, items := fixture()
	want := []int{25, 20, 15, 10, 10, 10, 10, 10, 10, 5, 5, 5, 5, 5}
	for i, p := range want {
		if got := probability(c.Items["elixir"], uint8(i)); got != p {
			t.Fatalf("plus %d: %d != %d", i, got, p)
		}
	}
	for _, draw := range []uint32{74, 75} {
		r, err := c.Reinforce(items, []uint8{15, 13, 14}, 0, sequence(t, draw))
		if err != nil {
			t.Fatal(err)
		}
		if r.Success != (draw == 74) {
			t.Fatalf("powder boundary %d: %+v", draw, r)
		}
		for _, item := range r.Items {
			if item.Slot == 15 {
				t.Fatal("last powder not consumed")
			}
		}
	}
}

/*
================
TestReinforceTablesReadTheAuthoredChances

The published tables are probability itself at each plus, the last level
standing for every higher plus, and a powder carries its degree.
================
*/
func TestReinforceTablesReadTheAuthoredChances(t *testing.T) {
	packed := [5]uint32{0, 0x32281e14, 0x0f0c0a08, 0x07060504}
	c := &Catalog{Items: map[string]Reference{
		"ELIXIR": {Flags: wire.PackTypeFlags(3, 3, 10, 1), Params: packed},
		"POWDER": {Flags: wire.PackTypeFlags(3, 3, 10, 2), Params: [5]uint32{3, 0x0a090807}},
		"OTHER":  {Flags: wire.PackTypeFlags(3, 3, 1, 1)},
	}}
	elixirs, powders := c.ReinforceTables()
	if len(elixirs) != 1 || len(powders) != 1 || powders[0].Degree != 3 {
		t.Fatalf("tables %+v %+v", elixirs, powders)
	}
	for plus, chance := range elixirs[0].Chances {
		if chance != probability(c.Items["ELIXIR"], uint8(plus)) {
			t.Fatalf("+%d chance %d", plus, chance)
		}
	}
	if elixirs[0].Chances[0] != 0x32 || elixirs[0].Chances[11] != probability(c.Items["ELIXIR"], 40) {
		t.Fatalf("chances %v", elixirs[0].Chances)
	}
}

/*
================
TestElixirKindsNameTheAcceptedFamilies

The classes an elixir's first and last parameters list are the families
Reinforce admits: weapon 6, shield 4, accessory 5/12, armor 1..3/9..11.
================
*/
func TestElixirKindsNameTheAcceptedFamilies(t *testing.T) {
	for _, tc := range []struct {
		params [5]uint32
		want   string
	}{
		{[5]uint32{0x06}, "weapon"},
		{[5]uint32{0x04}, "shield"},
		{[5]uint32{0x0c05}, "accessory"},
		{[5]uint32{0x03020109, 0, 0, 0, 0x0b0a}, "armor"},
		{[5]uint32{0x0604}, "shield,weapon"},
		{[5]uint32{}, ""},
	} {
		if got := strings.Join(elixirKinds(Reference{Params: tc.params}), ","); got != tc.want {
			t.Errorf("params %x: kinds %q, want %q", tc.params, got, tc.want)
		}
	}
}
