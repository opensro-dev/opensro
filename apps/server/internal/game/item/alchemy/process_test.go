package alchemy

import (
	"errors"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
	"reflect"
	"testing"
)

func processFixture() (*Catalog, []inventory.Item) {
	c := &Catalog{Items: map[string]Reference{}, Magic: map[uint16]Magic{}}
	refs := []Reference{
		{ID: 10, Name: "tablet", Flags: 0x1dec, Stack: 20, Class: 1, Params: [5]uint32{2, 3, 4, 5, 1}, Descriptions: [5]string{"earth", "water", "fire", "wind", "stone"}},
		{ID: 11, Name: "earth", Flags: 0x2dec, Stack: 5000, Class: 1},
		{ID: 12, Name: "water", Flags: 0x2dec, Stack: 5000, Class: 1},
		{ID: 13, Name: "fire", Flags: 0x2dec, Stack: 5000, Class: 1},
		{ID: 14, Name: "wind", Flags: 0x2dec, Stack: 5000, Class: 1},
		{ID: 15, Name: "stone", Flags: 0x0dec, Stack: 1, Class: 1, Params: [5]uint32{1, 0x0a141e00}},
		{ID: 16, Name: "material", Flags: 0x25ec, Stack: 250, Class: 1, Price: 1000, Params: [5]uint32{2, 3, 4, 5}, Descriptions: [5]string{"earth", "water", "fire", "wind"}},
		{ID: 17, Name: "ITEM_ETC_ARCHEMY_RONDO_01", Flags: 0x35ec, Stack: 5000},
	}
	items := []inventory.Item{}
	for i, r := range refs {
		c.Items[r.Name] = r
		if i < 5 {
			items = append(items, inventory.Item{Slot: uint8(13 + i), RefObjID: r.ID, Codename: r.Name, TypeFlags: r.Flags, Quantity: 10})
		}
	}
	return c, items
}

func TestCompoundManufactureUsesAuthoredGradesAndConsumesOneTablet(t *testing.T) {
	c, items := processFixture()
	before := append([]inventory.Item(nil), items...)
	out, err := c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 2, Quantity: 1, Slots: []uint8{13, 14, 15, 16, 17}}, sequence(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items, before) {
		t.Fatal("planner mutated input")
	}
	for i, want := range []uint16{9, 8, 7, 6, 5, 1} {
		if out.Items[i].Quantity != want {
			t.Fatalf("row %d: %+v", i, out.Items)
		}
	}
	if out.Items[5].Plus != 30 || out.Items[5].Codename != "stone" {
		t.Fatal(out.Items)
	}
}

func TestCompoundRefusalsAndEntropyAreAtomic(t *testing.T) {
	for _, kind := range []string{"wrong-grade", "insufficient", "full", "entropy", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			c, items := processFixture()
			request := ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 2, Quantity: 1, Slots: []uint8{13, 14, 15, 16, 17}}
			roll := Roll(func() (uint32, error) { return 0, nil })
			switch kind {
			case "wrong-grade":
				ref := c.Items["tablet"]
				ref.Descriptions[0] = "earth-grade-2"
				c.Items["tablet"] = ref
			case "insufficient":
				items[1].Quantity = 1
			case "duplicate":
				request.Slots[2] = request.Slots[1]
			case "full":
				for s := uint8(18); s < domain.DefaultInventorySize; s++ {
					items = append(items, inventory.Item{Slot: s, RefObjID: 999, Quantity: 1})
				}
			case "entropy":
				roll = func() (uint32, error) { return 0, errors.New("unavailable") }
			}
			before := append([]inventory.Item(nil), items...)
			if _, err := c.Compound(items, request, roll); err == nil {
				t.Fatal("accepted invalid attempt")
			} else if kind == "full" && !errors.Is(err, Refusal(8)) {
				t.Fatalf("capacity refusal: %v", err)
			}
			if !reflect.DeepEqual(items, before) {
				t.Fatal("mutated refused recipe")
			}
		})
	}
}

func TestCompoundElementSelectionOrderDoesNotChangeRecipe(t *testing.T) {
	c, items := processFixture()
	a, e := c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 2, Quantity: 1, Slots: []uint8{13, 14, 15, 16, 17}}, sequence(t, 0))
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 2, Quantity: 1, Slots: []uint8{13, 17, 15, 14, 16}}, sequence(t, 0))
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("recipe depends on element selection order", e)
	}
}

func TestCompoundMaterialRondoAndStackSplitting(t *testing.T) {
	c, _ := processFixture()
	m, r := c.Items["material"], c.Items["ITEM_ETC_ARCHEMY_RONDO_01"]
	items := []inventory.Item{{Slot: 13, RefObjID: m.ID, Codename: m.Name, TypeFlags: m.Flags, Quantity: 3}, {Slot: 14, RefObjID: r.ID, Codename: r.Name, TypeFlags: r.Flags, Quantity: 7}}
	out, err := c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 1, Quantity: 3, Slots: []uint8{13}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]uint16{}
	for _, i := range out.Items {
		counts[i.Codename] += i.Quantity
	}
	if !reflect.DeepEqual(counts, map[string]uint16{r.Name: 1, "earth": 6, "water": 9, "fire": 12, "wind": 15}) {
		t.Fatal(counts)
	}
	items[1].Quantity = 5
	if _, err = c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 1, Quantity: 3, Slots: []uint8{13}}, nil); err == nil {
		t.Fatal("accepted insufficient Rondo")
	}
	items[1].Quantity = 7
	items[1].RefObjID++
	before := append([]inventory.Item(nil), items...)
	if _, err = c.Compound(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 1, Quantity: 3, Slots: []uint8{13}}, nil); err == nil || !reflect.DeepEqual(before, items) {
		t.Fatal("auto-selected Rondo bypassed reference admission", err)
	}
}

func TestDissolveMissingAssignmentsCannotConsumeEquipment(t *testing.T) {
	c, items := fixture()
	before := append([]inventory.Item(nil), items...)
	_, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{13}}, func() (uint32, error) { t.Fatal("missing data drew randomness"); return 0, nil })
	if err == nil || !reflect.DeepEqual(items, before) {
		t.Fatal("missing assignment data was not atomic")
	}
}

func TestDissolveRepairRestrictionsRefuseBeforeDrawingOrConsuming(t *testing.T) {
	for tag, refusal := range map[uint32]Refusal{0x6e726570: 0x13, 0x00726570: 0x15} {
		c, items := fixture()
		c.Magic[99] = Magic{ID: 99, Tag: tag}
		items[0].MagicOptions = []uint64{99}
		before := append([]inventory.Item(nil), items...)
		_, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{13}}, func() (uint32, error) { t.Fatal("restricted item drew randomness"); return 0, nil })
		if !errors.Is(err, refusal) || !reflect.DeepEqual(items, before) {
			t.Fatal(err, items)
		}
	}
}

func TestDissolveCandidateElementMathAndRondoConsumption(t *testing.T) {
	c, items := fixture()
	items = items[:1]
	r := c.Items["weapon"]
	r.Price = 75000
	c.Items["weapon"] = r
	c.Items["ITEM_ETC_ARCHEMY_RONDO_02"] = Reference{ID: 30, Name: "ITEM_ETC_ARCHEMY_RONDO_02", Flags: 0x35ec, Stack: 5000}
	items = append(items, inventory.Item{Slot: 14, RefObjID: 30, Codename: "ITEM_ETC_ARCHEMY_RONDO_02", TypeFlags: 0x35ec, Quantity: 4})
	for k, name := range []string{"ITEM_ETC_ARCHEMY_ELEMENT_EARTH_01", "ITEM_ETC_ARCHEMY_ELEMENT_WATER_01"} {
		c.Items[name] = Reference{ID: uint32(40 + k), Name: name, Flags: 0x2dec, Class: 1, Stack: 5000}
	}
	c.Items["attr"] = Reference{ID: 50, Name: "attr", Flags: 0x15ec, Class: 1, Stack: 1, Params: [5]uint32{1, 0x0a141e00}}
	c.Items["blue"] = Reference{ID: 51, Name: "blue", Flags: 0x0dec, Class: 1, Stack: 1, Params: [5]uint32{1, 0x0a141e00}}
	c.DissolveDrops = map[int]DissolvePool{1: {Attribute: []WeightedStone{{"attr", 1}}, Magic: []WeightedStone{{"blue", 1}}}}
	out, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{13}}, func() (uint32, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatal(out.Items)
	}
	for _, i := range out.Items {
		if i.Quantity != 252 || (i.RefObjID != 40 && i.RefObjID != 41) {
			t.Fatal(out.Items)
		}
	}
	if items[0].Quantity != 1 || items[1].Quantity != 4 {
		t.Fatal("planner changed source bag")
	}
	withRondo, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{14, 13}}, func() (uint32, error) { return 0, nil })
	if err != nil || !reflect.DeepEqual(withRondo, out) {
		t.Fatal("v1.150 Rondo/equipment form diverged", err)
	}
}

func TestProcessDecoderRejectsTruncationTrailingAndDuplicateSlots(t *testing.T) {
	valid := []byte{2, 2, 1, 0, 0, 0, 5, 13, 14, 15, 16, 17}
	if _, err := DecodeProcess(OpCompound, valid); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(valid); n++ {
		if _, err := DecodeProcess(OpCompound, valid[:n]); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	for _, p := range [][]byte{append(append([]byte(nil), valid...), 0), {2, 1, 1, 0, 0, 0, 2, 13, 13}, {2, 2, 1, 0, 0, 0, 1, 13, 12, 12, 12, 14}} {
		if _, err := DecodeProcess(OpCompound, p); err == nil {
			t.Fatal("accepted malformed", p)
		}
	}
	if r, err := DecodeProcess(OpDissolve, []byte{1, 13, 12}); err != nil || len(r.Slots) != 1 {
		t.Fatal(r, err)
	}
}
