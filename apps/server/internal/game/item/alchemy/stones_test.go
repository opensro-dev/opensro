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
	if _, e := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t)); !errors.Is(e, Refusal(0x24)) {
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
	if _, e = c.Stone(items, []uint8{13, 14}, true, 0, sequence(t)); !errors.Is(e, Refusal(0x24)) {
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
