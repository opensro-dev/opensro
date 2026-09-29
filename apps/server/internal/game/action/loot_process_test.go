package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
)

func TestPublishedLootTabletsProduceAuthoredProduct(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	catalog, err := alchemy.LoadCatalog(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, code := range publishedLootCodes(t) {
		ref := catalog.Items[code]
		if ref.Flags != wire.PackTypeFlags(3, 3, 11, 3) {
			continue
		}
		count++
		t.Run(code, func(t *testing.T) {
			c := testCharacter()
			c.MissionInventory = nil
			rt, _ := newTestRuntime(c, items)
			rt.Alchemy = catalog
			rt.AlchemyRoll = func() (uint32, error) { return 0, nil }
			if !slices.Contains(rt.alchemyOutputCodenames(), ref.Descriptions[4]) {
				t.Fatal("manufactured product reference missing from bootstrap")
			}
			tabletRef, _ := items.ItemRefByCodename(code)
			tablet := pickupPublishedLoot(t, rt, c, tabletRef, 1)
			payload := []byte{2, 2, 1, 0, 0, 0, 5, byte(tablet.Slot)}
			for i := 0; i < 4; i++ {
				ingredient, ok := items.ItemRefByCodename(ref.Descriptions[i])
				if !ok || ref.Params[i] == 0 || ref.Params[i] > 65535 {
					t.Fatal("missing ingredient recipe")
				}
				row := pickupPublishedLoot(t, rt, c, ingredient, uint16(ref.Params[i]))
				payload = append(payload, byte(row.Slot))
			}
			frames := rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, payload)
			if len(frames) < 2 || frames[len(frames)-1].Payload[0] != 1 {
				t.Fatalf("dropped tablet failed: %+v", frames)
			}
			if len(c.MissionInventory) != 1 || c.MissionInventory[0].Codename != ref.Descriptions[4] || c.MissionInventory[0].StackCount != 1 {
				t.Fatalf("wrong tablet output: %+v", c.MissionInventory)
			}
			before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
			rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, payload)
			if !reflect.DeepEqual(before, c.MissionInventory) {
				t.Fatal("tablet replay changed inventory")
			}
		})
	}
	if count == 0 {
		t.Fatal("no published loot tablets exercised")
	}
}

func TestPublishedLootAmmunitionPickupEquipAndFire(t *testing.T) {
	licensed.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	count := 0
	for _, code := range publishedLootCodes(t) {
		ref, ok := items.ItemRefByCodename(code)
		if !ok || ref.TypeIDs[1] != 3 || ref.TypeIDs[2] != 4 {
			continue
		}
		count++
		t.Run(code, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 100)
			source := rt.deps.ItemReferences().(staticItemSource)
			source[code] = ref
			weaponKind := uint8(6)
			if ref.TypeIDs[3] == 2 {
				weaponKind = 12
			}
			weapon := source[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = int64(weaponKind)
			weapon.Combat.ActionRange = 180
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.RequiredWeaponKinds = [2]uint8{weaponKind, 0xff}
			skills[2] = skill
			row := pickupPublishedLoot(t, rt, c, ref, 2)
			frames := rt.HandleItemMove(testDivision, c, []byte{0, byte(row.Slot), 7, 2, 0}).Frames
			if len(frames) == 0 || frames[0].Payload[0] != 1 {
				t.Fatalf("ammo equip failed: %+v", frames)
			}
			result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
			if len(result.Frames) != 2 || result.Frames[1].Opcode != wire.OpAvatarInventorySlot7StackCount || !reflect.DeepEqual(result.Frames[1].Payload, []byte{1, 0}) {
				t.Fatalf("shot did not spend one dropped ammo: %+v", result.Frames)
			}
			found := false
			for _, row := range c.MissionInventory {
				if row.RefObjID == ref.RefObjID {
					found = true
					if row.Slot != 7 || row.StackCount != 1 {
						t.Fatal("wrong ammunition debit")
					}
				}
			}
			if !found {
				t.Fatal("remaining ammo disappeared")
			}
		})
	}
	if count == 0 {
		t.Fatal("no published ammunition exercised")
	}
}
