/*
===========================================================================

ammunitionpreference_test.go - the smallest-stack declaration lane

The one-byte preference frame (ammunitionpreference.go) is the whole
wire surface of the Experimental smallest-stack reload (#4527): a clean
declaration flips which bag stack the shoot door reloads from, anything
else changes nothing.
===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestAmmoReloadPreferenceKeepsOnlyCleanDeclarations
================
*/
func TestAmmoReloadPreferenceKeepsOnlyCleanDeclarations(t *testing.T) {
	rt := &Runtime{}
	character := &enterworld.Character{Name: "BoltTester"}
	for _, payload := range [][]byte{nil, {}, {2}, {1, 0}, {0xff}} {
		rt.HandleAmmoReloadPreference("", character, payload)
		if rt.ammoReloadsSmallest(character) {
			t.Fatalf("payload %v: smallest set, want unchanged (native)", payload)
		}
	}
	rt.HandleAmmoReloadPreference("", character, []byte{1})
	if !rt.ammoReloadsSmallest(character) {
		t.Fatal("payload [1]: smallest unset, want set")
	}
	rt.HandleAmmoReloadPreference("", character, []byte{0})
	if rt.ammoReloadsSmallest(character) {
		t.Fatal("payload [0]: smallest set, want cleared (native)")
	}
	if rt.ammoReloadsSmallest(nil) {
		t.Fatal("nil character: smallest set, want native")
	}
}

/*
================
TestSmallestStackReloadsThroughTheShootDoor

One arrow in the quiver, a 250 stack low in the bag and a 40 stack
higher up: without the declaration the reload takes the native lowest
slot (250); after [1] it takes the smallest stack (40).
================
*/
func TestSmallestStackReloadsThroughTheShootDoor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare []byte
		want    int64
	}{
		{"native lowest slot", nil, 250},
		{"smallest stack", []byte{1}, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, character, target := newCombatTestRuntime(t, 100000)
			character.Name = "BoltTester"
			items := rt.deps.ItemReferences().(staticItemSource)
			weapon := items[character.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = 6
			weapon.Combat.ActionRange = 180
			character.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			arrow := &enterworld.ItemRef{RefObjID: 62_001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
			items[arrow.Codename] = arrow
			character.MissionInventory = append(character.MissionInventory,
				enterworld.InventoryRow{
					Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 1,
				},
				enterworld.InventoryRow{
					Slot: 15, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 250,
				},
				enterworld.InventoryRow{
					Slot: 20, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 40,
				},
			)
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Codename = "SKILL_CH_BOW_BASE_01"
			skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
			skills[2] = skill
			if tc.declare != nil {
				rt.HandleAmmoReloadPreference(testDivision, character, tc.declare)
			}
			result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
				ActionId: 2, HasTarget: true, TargetGid: target.Gid,
			}.Encode())
			result = assertAndSeparateActionSession(t, result)
			_ = result
			for _, row := range character.MissionInventory {
				if row.Slot == 7 {
					if row.StackCount != tc.want {
						t.Fatalf("quiver after the reload shot = %d, want %d", row.StackCount, tc.want)
					}
					return
				}
			}
			t.Fatalf("no quiver row after the shot: %+v", character.MissionInventory)
		})
	}
}
