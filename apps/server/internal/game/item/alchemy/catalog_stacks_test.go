/*
===========================================================================

catalog_stacks_test.go - effective stone caps reach alchemy output placement

Load the shipped catalog through its real admission path, then allocate
products into a full bag to distinguish native singles from raised stacks.

===========================================================================
*/
package alchemy

import (
	"errors"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const catalogStoneTestCap uint16 = 50

/*
================
TestPublishedCatalogStoneStackPlacement
================
*/
func TestPublishedCatalogStoneStackPlacement(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	for _, raised := range []bool{false, true} {
		name := "native"
		if raised {
			name = "raised"
		}
		t.Run(name, func(t *testing.T) {
			source := enterworld.NewTextdataItems(dir)
			if raised {
				count, err := source.ApplyStackSizes(enterworld.StackSizes{"magicstone": catalogStoneTestCap, "attrstone": catalogStoneTestCap})
				if err != nil || count == 0 {
					t.Fatalf("raise stone caps: count %d, error %v", count, err)
				}
			}
			catalog, err := LoadCatalog(dir, source)
			if err != nil {
				t.Fatal(err)
			}
			for _, subtype := range []uint8{1, 2, 7} {
				var stone Reference
				for _, ref := range catalog.Items {
					if ref.Flags == wire.PackTypeFlags(3, 3, 11, subtype) && (stone.ID == 0 || ref.ID < stone.ID) {
						stone = ref
					}
				}
				wantCap := uint16(1)
				if raised {
					wantCap = catalogStoneTestCap
				}
				if stone.ID == 0 || stone.Stack != wantCap {
					t.Fatalf("subtype %d: reference %+v, want cap %d", subtype, stone, wantCap)
				}
				for _, matching := range []bool{true, false} {
					if subtype == 7 && !matching {
						continue
					}
					plus := uint8(70)
					if subtype == 7 {
						plus = 0
					}
					items := []inventory.Item{{Slot: inventory.EquipmentSlotEnd, RefObjID: stone.ID, Codename: stone.Name, TypeFlags: stone.Flags, Quantity: 1, Plus: plus}}
					for slot := uint8(inventory.EquipmentSlotEnd + 1); slot < domain.DefaultInventorySize; slot++ {
						items = append(items, inventory.Item{Slot: slot, RefObjID: stone.ID, Codename: stone.Name, TypeFlags: stone.Flags, Quantity: wantCap, Plus: plus})
					}
					before := append([]inventory.Item(nil), items...)
					productPlus := plus
					if !matching {
						productPlus = 40
					}
					out, err := allocateProducts(items, nil, []Product{{Reference: stone, Quantity: 1, Plus: productPlus}}, domain.DefaultInventorySize)
					if raised && matching {
						want := append([]inventory.Item(nil), before...)
						want[0].Quantity++
						if err != nil || !reflect.DeepEqual(out, want) {
							t.Fatalf("subtype %d: matching product did not merge: %v, rows %+v", subtype, err, out)
						}
					} else if !errors.Is(err, Refusal(8)) || out != nil {
						t.Fatalf("subtype %d matching %v: full bag accepted product: %v", subtype, matching, err)
					}
					if !reflect.DeepEqual(items, before) {
						t.Fatalf("subtype %d: allocation mutated input", subtype)
					}
				}
			}
		})
	}
}
