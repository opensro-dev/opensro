package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"sort"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

func TestPublishedLootAlchemyActivationForAvailableEquipment(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	catalog, err := alchemy.LoadCatalog(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	var equipment []alchemy.Reference
	degrees := map[int]bool{}
	codes := publishedLootCodes(t)
	for _, code := range codes {
		ref := catalog.Items[code]
		if _, ok := inventory.EquipSocketForTypeFlags(ref.Flags); ok && ref.Flags&0x7e == 0x2c && ref.Degree() > 0 {
			equipment = append(equipment, ref)
			degrees[ref.Degree()] = true
		}
	}
	sort.Slice(equipment, func(i, j int) bool { return equipment[i].ID < equipment[j].ID })
	covered := 0
	unavailable := 0
	for _, code := range codes {
		material := catalog.Items[code]
		reinforce := material.Flags == wire.PackTypeFlags(3, 3, 10, 1)
		magic := material.Flags == wire.PackTypeFlags(3, 3, 11, 1) || material.Flags == wire.PackTypeFlags(3, 3, 11, 7)
		attribute := material.Flags == wire.PackTypeFlags(3, 3, 11, 2)
		if !reinforce && !magic && !attribute {
			continue
		}
		// The joined data includes future degree 10-12 stones while this
		// build has loot equipment only through degree 9. These are pickup-covered
		// by TestPublishedLootPickupAndActivation, NOT usable coverage. Report
		// the gap separately; never fabricate target equipment or remove loot
		// just to turn a compatibility gap into a successful activation.
		if !reinforce && !degrees[int(material.Params[0])] {
			unavailable++
			continue
		}
		covered++
		t.Run(code, func(t *testing.T) {
			// Find an actual compatible target from itemdata. Recipe compatibility
			// belongs to alchemy; test fixtures must not duplicate category tables.
			var target alchemy.Reference
			var options []uint64
			var lastError error
			var lastCandidate alchemy.Reference
			for _, candidate := range equipment {
				if !reinforce && candidate.Degree() != int(material.Params[0]) {
					continue
				}
				options = nil
				if magic {
					option, ok := catalog.Option(material.Descriptions[0], int(material.Params[0]))
					if !ok {
						continue
					}
					if option.Tag == 0x61737472 {
						immortal, ok := catalog.Option("MATTR_ATHANASIA", candidate.Degree())
						if !ok {
							t.Fatal("astral prerequisite reference missing")
						}
						options = []uint64{1<<32 | uint64(immortal.ID)}
					} else if option.Tag == 0x726570 {
						options = []uint64{1<<32 | uint64(option.ID)}
					}
				}
				rows := []inventory.Item{{Slot: 13, RefObjID: candidate.ID, Codename: candidate.Name, TypeFlags: candidate.Flags, Quantity: 1, MagicOptions: options}, {Slot: 14, RefObjID: material.ID, Codename: material.Name, TypeFlags: material.Flags, Quantity: 1}}
				roll := func() (uint32, error) { return 0, nil }
				var err error
				if reinforce {
					_, err = catalog.Reinforce(rows, []uint8{13, 14}, 0, roll)
				} else {
					_, err = catalog.Stone(rows, []uint8{13, 14}, magic, 0, roll)
				}
				if err == nil {
					target = candidate
					break
				}
				lastError, lastCandidate = err, candidate
			}
			if target.ID == 0 {
				t.Fatalf("loot material has no usable published recipe/target: last=%s flags=%x class=%d error=%v", lastCandidate.Name, lastCandidate.Flags, lastCandidate.Class, lastError)
			}
			for _, dead := range []bool{false, true} {
				c := testCharacter()
				c.MissionInventory = nil
				rt, _ := newTestRuntime(c, items)
				rt.Alchemy = catalog
				rt.DropRoll = func() (uint32, error) { return 0, nil }
				rt.AlchemyRoll = alchemy.Roll(rt.DropRoll)
				targetRef, _ := items.ItemRefByCodename(target.Name)
				materialRef, _ := items.ItemRefByCodename(code)
				weapon := pickupPublishedLoot(t, rt, c, targetRef, 1)
				stone := pickupPublishedLoot(t, rt, c, materialRef, 1)
				for i := range c.MissionInventory {
					if c.MissionInventory[i].Slot == weapon.Slot {
						c.MissionInventory[i].MagicOptions = append([]uint64(nil), options...)
					}
				}
				if dead {
					hp := int64(0)
					c.CurrentHP = &hp
				}
				before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
				invoke := func() []wire.Frame {
					if reinforce {
						return rt.HandleAlchemyReinforce(testDivision, c, []byte{2, byte(weapon.Slot), byte(stone.Slot)})
					}
					mode := byte(2)
					if magic {
						mode = 3
					}
					return rt.HandleAlchemyStone(testDivision, c, []byte{mode, 2, byte(weapon.Slot), byte(stone.Slot)})
				}
				frames := invoke()
				last := frames[len(frames)-1]
				if dead {
					if last.Payload[0] == 1 || !reflect.DeepEqual(before, c.MissionInventory) {
						t.Fatal("dead owner spent dropped alchemy material")
					}
					continue
				}
				if len(frames) < 2 || frames[0].Opcode != 0x3645 || last.Payload[0] != 1 || len(c.MissionInventory) != 1 {
					t.Fatalf("drop-to-alchemy failed: %+v", frames)
				}
				if reinforce && c.MissionInventory[0].Plus != 1 {
					t.Fatal("reinforcement effect missing")
				}
				if magic && reflect.DeepEqual(c.MissionInventory[0].MagicOptions, options) {
					t.Fatal("magic effect missing")
				}
				after := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
				invoke()
				if !reflect.DeepEqual(after, c.MissionInventory) {
					t.Fatal("alchemy replay changed inventory")
				}
			}
		})
	}
	if covered == 0 {
		t.Fatal("no alchemy loot exercised")
	}
	t.Logf("exercised %d published reinforcement/stone assignments", covered)
	t.Logf("UNSUPPORTED: %d assignments have no published loot equipment degree", unavailable)
}
