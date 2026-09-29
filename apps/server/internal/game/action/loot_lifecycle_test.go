/*
===========================================================================

loot_lifecycle_test.go - loot behavior and lifecycle verification

===========================================================================
*/

package action

import (
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// The public inventory includes ordinary, assigned, and native special rewards.
/*
================
publishedLootCodes
================
*/
func publishedLootCodes(t *testing.T) []string {
	t.Helper()
	return loot.CatalogItemCodenames()
}

/*
================
pickupPublishedLoot
================
*/
func pickupPublishedLoot(t *testing.T, rt *Runtime, c *enterworld.Character, ref *enterworld.ItemRef, count uint16) enterworld.InventoryRow {
	t.Helper()
	at := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, rt.Now().UnixMilli())
	drop, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: ref.Codename, Count: count}, at, c.Name, rt.Now())
	if !ok {
		t.Fatal("drop planning refused")
	}
	drop.OwnerJID = enterworld.ObjectIDForCharacter(c)
	stored := rt.Ground.Add(testDivision, drop)
	raw, err := json.Marshal(rt.Ground.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := rt.Ground.Snapshot()
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	rt.Ground = grounditem.NewRegistry()
	rt.Ground.Restore(snapshot)
	result := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: stored.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatalf("pickup failed: %+v", result.Frames)
	}
	before, _ := json.Marshal(c.MissionInventory)
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: stored.Gid}.Encode())
	after, _ := json.Marshal(c.MissionInventory)
	if string(before) != string(after) {
		t.Fatal("pickup replay changed inventory")
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID == ref.RefObjID {
			if row.Codename != ref.Codename || row.TypeFlags != ref.TypeFlags() || row.StackCount != int64(count) || row.Durability != int64(drop.Durability) {
				t.Fatalf("pickup lost identity/quantity/durability: %+v", row)
			}
			variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
			if err != nil || variance != drop.VarianceBits || row.Plus != int64(drop.Plus) || !reflect.DeepEqual(row.MagicOptions, drop.MagicOptions) {
				t.Fatal("pickup lost instance modifiers")
			}
			return row
		}
	}
	t.Fatal("pickup produced no inventory row")
	return enterworld.InventoryRow{}
}

/*
================
TestPublishedLootPickupAndActivation
================
*/
func TestPublishedLootPickupAndActivation(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	magic := enterworld.NewTextdataMagicOptions(textdata)
	codes := publishedLootCodes(t)
	t.Logf("pickup/persistence coverage: %d production loot identities; activation assertions cover equipment and recovery here", len(codes))
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			ref, ok := items.ItemRefByCodename(code)
			if !ok || ref == nil {
				t.Fatal("production loot has no published reference")
			}
			c := testCharacter()
			c.MissionInventory = nil
			level, stat, hp, mp := int64(110), int64(1000), int64(1), int64(1)
			c.Level, c.MaxLevel, c.Strength, c.Intellect, c.CurrentHP, c.CurrentMP = &level, &level, &stat, &stat, &hp, &mp
			race, sex := "CH", "MAN"
			if ref.Country == 1 {
				race = "EU"
			}
			if ref.RequiredSex == 0 {
				sex = "WOMAN"
			}
			c.ModelCodename = "CHAR_" + race + "_" + sex + "_ADVENTURER"
			rt, _ := newTestRuntime(c, items)
			rt.deps.(*enterworld.Deps).MagicOptions = magic
			rt.DropRoll = func() (uint32, error) { return 0, nil }
			row := pickupPublishedLoot(t, rt, c, ref, 1)
			if socket, equip := inventory.EquipSocketForTypeFlags(ref.TypeFlags()); equip {
				expectedItems := 1
				family := ammoFamily(inventory.Item{TypeFlags: ref.TypeFlags()})
				if family != 0 {
					before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
					refused := rt.HandleItemMove(testDivision, c, []byte{0, byte(row.Slot), socket, 1, 0})
					if len(refused.Frames) != 1 || !reflect.DeepEqual(refused.Frames[0].Payload, []byte{2, wire.ErrCodeCantEquip}) || !reflect.DeepEqual(before, c.MissionInventory) {
						t.Fatal("ammo without weapon must refuse atomically")
					}
					code := "ITEM_CH_BOW_01_A"
					if family == 2 {
						code = "ITEM_EU_CROSSBOW_01_A"
					}
					weapon, ok := items.ItemRefByCodename(code)
					if !ok {
						t.Fatal("missing compatible weapon", code)
					}
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 6, RefObjID: weapon.RefObjID, Codename: weapon.Codename, TypeFlags: weapon.TypeFlags(), Durability: 30, StackCount: 1})
					expectedItems = 2
				}
				result := rt.HandleItemMove(testDivision, c, []byte{0, byte(row.Slot), socket, 1, 0})
				if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
					t.Fatalf("dropped item cannot equip: %+v", result.Frames)
				}
				if len(c.MissionInventory) != expectedItems || !slices.ContainsFunc(c.MissionInventory, func(item enterworld.InventoryRow) bool {
					return item.Slot == int64(socket) && item.RefObjID == ref.RefObjID
				}) {
					t.Fatal("equip did not commit")
				}
			} else if admittedItemUseFamily(ref) == itemUseRecovery {
				result := rt.HandleItemUse(testDivision, c, []byte{byte(row.Slot), byte(row.TypeFlags), byte(row.TypeFlags >> 8)})
				if len(result.Frames) < 2 || result.Frames[0].Payload[0] != 1 {
					t.Fatalf("dropped recovery cannot be used: %+v", result.Frames)
				}
				if len(c.MissionInventory) != 0 || (*c.CurrentHP == 1 && *c.CurrentMP == 1) {
					t.Fatal("recovery did not commit effect and consumption")
				}
			}
			// Persistence checks the post-action state, not just the grant.
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			var restored enterworld.Character
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.MissionInventory, restored.MissionInventory) || c.ItemUseCooldowns != restored.ItemUseCooldowns {
				t.Fatal("post-use state lost on persistence round trip")
			}
		})
	}
}
