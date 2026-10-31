package alchemy

import (
	"errors"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func stoneFixture(magic bool) (*Catalog, []inventory.Item) {
	c, items := fixture()
	r := c.Items["weapon"]
	r.MaxMagic = 9
	c.Items["weapon"] = r
	stone := Reference{ID: 4, Name: "stone", Flags: wire.PackTypeFlags(3, 3, 11, 2), Params: [5]uint32{1, 0, 0, 50}, Descriptions: [5]string{"NATTR_PA", "", "", "weapon"}}
	if magic {
		stone.Flags = wire.PackTypeFlags(3, 3, 11, 1)
		stone.Descriptions[0] = "MATTR_STR"
	}
	c.Items["stone"] = stone
	c.Magic[11] = Magic{ID: 11, Name: "MATTR_STR", Degree: 1, Tag: 0x737472, Params: [3]uint32{1<<16 | 2, 3 << 16}, Categories: []string{"weapon"}}
	c.Magic[12] = Magic{ID: 12, Name: "MATTR_ASTRAL", Degree: 1, Tag: 0x61737472, Categories: []string{"weapon"}}
	items = items[:1]
	items = append(items, inventory.Item{Slot: 14, RefObjID: 4, Codename: "stone", TypeFlags: stone.Flags, Quantity: 1})
	return c, items
}

func TestStoneInclusiveSuccessBoundaryAndConsumption(t *testing.T) {
	for _, magic := range []bool{false, true} {
		for _, n := range []uint32{50, 51} {
			c, items := stoneFixture(magic)
			before := clone(items)
			draws := []uint32{n}
			if n == 50 {
				if magic {
					draws = append(draws, 1)
				} else {
					draws = append(draws, 5, 4, 3, 2)
				}
				draws = append(draws, 100)
			}
			r, e := c.Stone(items, []uint8{13, 14}, magic, 0, sequence(t, draws...))
			if e != nil {
				t.Fatal(e)
			}
			if r.Success != (n == 50) || len(r.Items) != 1 {
				t.Fatalf("stone outcome %+v", r)
			}
			if !reflect.DeepEqual(items, before) {
				t.Fatal("stone mutated source")
			}
			if n == 51 && !reflect.DeepEqual(r.Items[0], items[0]) {
				t.Fatal("failed stone altered equipment")
			}
			if n == 50 && magic && !reflect.DeepEqual(r.Items[0].MagicOptions, []uint64{2<<32 | 11}) {
				t.Fatalf("magic values %+v", r.Items[0])
			}
			if n == 50 && !magic {
				mask := uint64(31) << 20
				if r.Items[0].VarianceBits & ^mask != items[0].VarianceBits & ^mask || r.Items[0].VarianceBits>>20&31 != 13 {
					t.Fatal("attribute selection or unrelated variance bits changed")
				}
			}
			frames := ResultFrames(OpStoneResult, items, r)
			last := frames[len(frames)-1]
			if n == 51 && !reflect.DeepEqual(last.Payload, []byte{2, 0x23}) {
				t.Fatalf("stone failure wire %x", last.Payload)
			}
		}
	}
}

func TestStoneAssimilationProtectionAndUnrelatedAttribute(t *testing.T) {
	c, items := stoneFixture(false)
	items[1].Plus = 100
	c.Magic[13] = Magic{ID: 13, Tag: 0x617065}
	items[0].MagicOptions = []uint64{1<<32 | 13}
	r, e := c.Stone(items, []uint8{13, 14}, false, 0, sequence(t, 0, 5, 5, 5, 5, 0))
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Items[0].MagicOptions) != 0 {
		t.Fatal("assimilation protection charge not consumed")
	}
	items[0].MagicOptions = nil
	r, e = c.Stone(items, []uint8{13, 14}, false, 0, sequence(t, 0, 5, 5, 5, 5, 0, 0, 1, 1, 1, 1))
	if e != nil {
		t.Fatal(e)
	}
	if r.Items[0].VarianceBits&31 != 7 || r.Items[0].VarianceBits>>20&31 != 31 {
		t.Fatal("assimilation did not retain primary result and change selected secondary")
	}
}

func TestAstralRequiresMoreImmortalChargesAndEnforcesCapacity(t *testing.T) {
	c, items := stoneFixture(true)
	r := c.Items["stone"]
	r.Descriptions[0] = "MATTR_ASTRAL"
	c.Items["stone"] = r
	if _, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(e, Refusal(0x24)) {
		t.Fatalf("astral without immortal %v", e)
	}
	items[0].MagicOptions = []uint64{1<<32 | 2}
	result, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0, 100))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(result.Items[0].MagicOptions, []uint64{1<<32 | 2, 1<<32 | 12}) {
		t.Fatal("astral modified immortal charge")
	}
	items = result.Items
	items = append(items, inventory.Item{Slot: 14, RefObjID: r.ID, Codename: r.Name, TypeFlags: r.Flags, Quantity: 1})
	if _, e = c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(e, Refusal(0x24)) {
		t.Fatalf("astral exceeded immortal count: %v", e)
	}
}

/*
================
TestStackedStoneSpendsOneAndKeepsItsValue

Port-only stacking (#583): alchemy from a stack of five 70% stones uses
one, success or failure, and the other four keep their slot and value.
================
*/
func TestStackedStoneSpendsOneAndKeepsItsValue(t *testing.T) {
	for _, draws := range [][]uint32{{51}, {50, 5, 4, 3, 2, 100}} {
		c, items := stoneFixture(false)
		items[1].Quantity, items[1].Plus = 5, 70
		r, e := c.Stone(items, []uint8{13, 14}, false, 0, sequence(t, draws...))
		if e != nil {
			t.Fatal(e)
		}
		if r.Success != (draws[0] == 50) || len(r.Items) != 2 {
			t.Fatalf("draws %v: rows %+v", draws, r.Items)
		}
		left := r.Items[1]
		if left.Slot != 14 || left.Quantity != 4 || left.Plus != 70 {
			t.Fatalf("draws %v: stone stack %+v, want four 70%% stones in slot 14", draws, left)
		}
	}
}

/*
================
TestStoneOfAnotherDegreeIsRefusedWithItsOwnCode

The native server refuses an attribute stone of another degree before
the roll with 0x5421 (its target check) and keeps the stone. A magic
stone's 0x540F degree gate runs after the roll: a passed roll is
refused and keeps the stone, a failed roll consumes the stone and
answers as a plain failure.
================
*/
func TestStoneOfAnotherDegreeIsRefusedWithItsOwnCode(t *testing.T) {
	c, items := stoneFixture(false)
	r := c.Items["stone"]
	r.Params[0] = 2
	c.Items["stone"] = r
	if _, e := c.Stone(items, []uint8{13, 14}, false, 0, sequence(t)); !errors.Is(e, Refusal(0x21)) {
		t.Fatalf("attribute degree: %v, want refusal %#x", e, 0x21)
	}

	c, items = stoneFixture(true)
	r = c.Items["stone"]
	r.Params[0] = 2
	c.Items["stone"] = r
	// The degree-2 option row exists, so a passed roll reaches the
	// degree gate rather than the unknown-option answer.
	c.Magic[14] = Magic{ID: 14, Name: "MATTR_STR", Degree: 2, Tag: 0x737472, Categories: []string{"weapon"}}
	if _, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(e, Refusal(stoneErrMagicDegree)) {
		t.Fatalf("magic degree, passed roll: %v, want refusal %#x", e, stoneErrMagicDegree)
	}
	result, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 51))
	if e != nil {
		t.Fatal(e)
	}
	if result.Success || len(result.Items) != 1 {
		t.Fatalf("magic degree, failed roll: %+v, want the stone consumed as a plain failure", result)
	}
}

/*
================
TestMagicStoneGatesRunAfterTheRoll

Native order: the roll comes first, so a magic
stone on an item a gate would refuse is consumed by a failed roll and
answered as a plain failure; only a passed roll reaches the gates, in
native order, and keeps the stone. Every gate is exercised both ways,
under both assimilation settings.
================
*/
func TestMagicStoneGatesRunAfterTheRoll(t *testing.T) {
	stoneDesc := func(name string) func(*Catalog, []inventory.Item) []inventory.Item {
		return func(c *Catalog, items []inventory.Item) []inventory.Item {
			r := c.Items["stone"]
			r.Descriptions[0] = name
			c.Items["stone"] = r
			return items
		}
	}
	options := func(values ...uint64) func(*Catalog, []inventory.Item) []inventory.Item {
		return func(c *Catalog, items []inventory.Item) []inventory.Item {
			items[0].MagicOptions = values
			return items
		}
	}
	gates := []struct {
		name  string
		setup func(*Catalog, []inventory.Item) []inventory.Item
		want  Refusal
	}{
		{"option count above the limit", options(
			1<<32|11, 1<<32|11, 1<<32|11, 1<<32|11, 1<<32|11,
			1<<32|11, 1<<32|11, 1<<32|11, 1<<32|11, 1<<32|11,
		), Refusal(2)},
		{"option not in the ref table", stoneDesc("MATTR_MISSING"), Refusal(2)},
		{"stone degree differs", func(c *Catalog, items []inventory.Item) []inventory.Item {
			r := c.Items["stone"]
			r.Params[0] = 2
			c.Items["stone"] = r
			c.Magic[14] = Magic{ID: 14, Name: "MATTR_STR", Degree: 2, Tag: 0x737472, Categories: []string{"weapon"}}
			return items
		}, Refusal(stoneErrMagicDegree)},
		{"option not assigned to the item", func(c *Catalog, items []inventory.Item) []inventory.Item {
			c.Magic[15] = Magic{ID: 15, Name: "MATTR_SHIELD", Degree: 1, Tag: 0x73686965, Categories: []string{"shield"}}
			return stoneDesc("MATTR_SHIELD")(c, items)
		}, Refusal(9)},
		{"repair option absent", func(c *Catalog, items []inventory.Item) []inventory.Item {
			c.Magic[16] = Magic{ID: 16, Name: "MATTR_REPAIR", Degree: 1, Tag: 0x726570, Categories: []string{"weapon"}}
			return stoneDesc("MATTR_REPAIR")(c, items)
		}, Refusal(0x25)},
		{"astral without more immortal", stoneDesc("MATTR_ASTRAL"), Refusal(0x24)},
		{"value at its cap", func(c *Catalog, items []inventory.Item) []inventory.Item {
			items = options(7<<32|2, 6<<32|12)(c, items)
			return stoneDesc("MATTR_ASTRAL")(c, items)
		}, Refusal(0x22)},
		{"new option on a full item", options(
			1<<32|2, 1<<32|2, 1<<32|2, 1<<32|2, 1<<32|2,
			1<<32|2, 1<<32|2, 1<<32|2, 1<<32|2,
		), Refusal(2)},
	}
	for _, noAssimilation := range []bool{false, true} {
		for _, gate := range gates {
			c, items := stoneFixture(true)
			c.NoAssimilation = noAssimilation
			items = gate.setup(c, items)
			if _, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(e, gate.want) {
				t.Fatalf(
					"%s (noAssimilation %v): passed roll %v, want refusal %#x",
					gate.name, noAssimilation, e, uint8(gate.want),
				)
			}
			result, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 51))
			if e != nil {
				t.Fatalf("%s (noAssimilation %v): failed roll error %v", gate.name, noAssimilation, e)
			}
			if result.Success || len(result.Items) != 1 {
				t.Fatalf(
					"%s (noAssimilation %v): failed roll %+v, want the stone consumed as a plain failure",
					gate.name, noAssimilation, result,
				)
			}
		}
	}
}
