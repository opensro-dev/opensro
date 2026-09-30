/*
===========================================================================

starter_inventory_test.go - creation grants the starter inventory (BUG-045)

A character created with a bow stood empty-handed at the dock until its
first world entry, because the starter items were granted only by the
enter-world bootstrap. The creation seed now grants them, and the character
list loadout shows them at once.

===========================================================================
*/
package enterworld

import (
	"reflect"
	"testing"
)

const chinaBowWeaponIndex = 5

/*
================
starterBowFixture

A China archer and the itemdata rows its creation grants: the bow, its
starter arrows and the light armour set.
================
*/
func starterBowFixture() (*Character, fakeItems) {
	character := chinaSpearman()
	character.WeaponIndex = i64(chinaBowWeaponIndex)
	items := testItems()
	items["ITEM_CH_BOW_01_A_DEF"] = &ItemRef{RefObjID: 3655, Codename: "ITEM_CH_BOW_01_A_DEF", TypeIDs: [4]int64{3, 1, 6, 6}, Name: "bow", Country: 3, RequiredSex: 2}
	items["ITEM_ETC_AMMO_ARROW_01_DEF"] = &ItemRef{RefObjID: 62, Codename: "ITEM_ETC_AMMO_ARROW_01_DEF", TypeIDs: [4]int64{3, 3, 4, 1}, Name: "arrow", Country: 3, RequiredSex: 2}
	return character, items
}

/*
================
TestCreationSeedDressesTheCharacterListLoadout
================
*/
func TestCreationSeedDressesTheCharacterListLoadout(t *testing.T) {
	character, items := starterBowFixture()
	roster := testRoster()
	if before := ResolveVisualLoadout(character, roster, 0); len(before.Items) != 0 {
		t.Fatalf("unseeded character already wears %v", before.Items)
	}
	StarterInventorySeeder(roster, items, true)(character)
	loadout := ResolveVisualLoadout(character, roster, 0)
	wearsBow := false
	for _, item := range loadout.Items {
		wearsBow = wearsBow || item.RefObjID == 3655
	}
	if !wearsBow {
		t.Fatalf("character list loadout %v lacks the creation bow", loadout.Items)
	}
	arrows := findRowBySlot(character.MissionInventory, slotShield)
	if arrows == nil || arrows.RefObjID != 62 || arrows.StackCount != 250 {
		t.Fatalf("starter arrows = %+v, want 250 in the secondary socket", arrows)
	}
}

/*
================
TestStarterGrantAppliesOnce

The bootstrap grant for records created before the creation seed must not
replace an inventory the creation seed (or play) already installed.
================
*/
func TestStarterGrantAppliesOnce(t *testing.T) {
	character, items := starterBowFixture()
	roster := testRoster()
	StarterInventorySeeder(roster, items, true)(character)
	character.MissionInventory = character.MissionInventory[:1]
	kept := character.MissionInventory[0]
	GrantStarterInventory(character, StarterEquipRoster(character, roster, items, true))
	if len(character.MissionInventory) != 1 || !reflect.DeepEqual(character.MissionInventory[0], kept) {
		t.Fatalf("second grant changed the inventory: %+v", character.MissionInventory)
	}
}
