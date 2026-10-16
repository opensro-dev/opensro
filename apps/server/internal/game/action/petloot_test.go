/*
===========================================================================

petloot_test.go - monster-only pet gear creation, publication and pickup

===========================================================================
*/
package action

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestMonsterPetGearAllDegreesAndSlots
================
*/
func TestMonsterPetGearAllDegreesAndSlots(t *testing.T) {
	for _, degree := range companion.D.Gear {
		for slot, item := range degree.Items {
			rt, clock, c, _ := newCombatTestRuntime(t, 1)
			rt.AttackPetExpansion = true
			rt.PetGearDropFactor = 1
			parent := &enterworld.ItemRef{RefObjID: 50000, Codename: "TEST_NATIVE_GEAR", TypeIDs: [4]int64{3, 1, 6, 2}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": float64((degree.Degree-1)*3 + 1)})}
			rt.deps.ItemReferences().(staticItemSource)[parent.Codename] = parent
			ref := &enterworld.ItemRef{RefObjID: item.ID, Codename: item.Codename, TypeIDs: [4]int64{3, 3, 9, 0}}
			rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
			rolls := []uint32{0, uint32((slot*2 + 1) * dropRollDomain / 6), 0, dropRollDomain - 1}
			rt.DropRoll = func() (uint32, error) {
				if len(rolls) == 0 {
					return 0, nil
				}
				next := rolls[0]
				rolls = rolls[1:]
				return next, nil
			}
			drop, ok := rt.preparePetGearDrop(c, grounditem.Item{RefObjID: parent.RefObjID}, simulation.Spawn{RegionID: 1}, clock.Now())
			if !ok || drop.RefObjID != item.ID || drop.StackCount != 1 || len(drop.MagicOptions) != degree.MaxBlues {
				t.Fatalf("D%d %s drop: %+v", degree.Degree, item.Slot, drop)
			}
		}
	}
}

/*
================
TestPetGearFactorAndNativeParentRestrictions
================
*/
func TestPetGearFactorAndNativeParentRestrictions(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1)
	rt.AttackPetExpansion = true
	refs := rt.deps.ItemReferences().(staticItemSource)
	refs["ITEM_COS_PET_COLLAR_01"] = &enterworld.ItemRef{RefObjID: 30101, Codename: "ITEM_COS_PET_COLLAR_01", TypeIDs: [4]int64{3, 3, 9, 0}}
	parent := &enterworld.ItemRef{RefObjID: 50000, TypeIDs: [4]int64{3, 1, 6, 2}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 1})}
	refs["PARENT"] = parent
	for _, tc := range []struct {
		factor float64
		roll   uint32
		class  float64
		kind   int64
		want   bool
	}{
		{0, 0, 1, 1, false}, {1, 32767, 1, 1, true}, {0.1, 3276, 1, 1, true}, {0.1, 3277, 1, 1, false},
		{1, 0, 0, 1, false}, {1, 0, 28, 1, false}, {1, 0, 1, 3, false},
	} {
		rt.PetGearDropFactor = tc.factor
		parent.TypeIDs[1] = tc.kind
		parent.NativeFields = parent.NativeFields.With("itemClass", tc.class)
		first := true
		rt.DropRoll = func() (uint32, error) {
			if first {
				first = false
				return tc.roll, nil
			}
			return 0, nil
		}
		_, ok := rt.preparePetGearDrop(c, grounditem.Item{RefObjID: parent.RefObjID}, simulation.Spawn{RegionID: 1}, clock.Now())
		if ok != tc.want {
			t.Fatalf("factor/parent %+v: got %v", tc, ok)
		}
	}
}

/*
================
TestPetGearCannotBeBoughtFromNPCs
================
*/
func TestPetGearCannotBeBoughtFromNPCs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, bundled := range []bool{false, true} {
			rt, c := merchantFixture(t)
			rt.AttackPetExpansion = enabled
			base := rt.Commerce.Tabs[1][0]
			ref := *base.Ref
			ref.RefObjID, ref.Codename = 30109, "ITEM_COS_PET_COLLAR_09"
			rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = &ref
			offer := commerce.Offer{Slot: 3, Ref: &ref, Price: 1, Stack: 1}
			if bundled {
				offer.Ref = base.Ref
				offer.Contents = []commerce.Content{{Ref: base.Ref, Stack: 1}, {Ref: &ref, Stack: 1}}
			}
			rt.Commerce.Tabs[1] = append(rt.Commerce.Tabs[1], offer)
			var catalog shopProjection
			if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &catalog); err != nil {
				t.Fatal(err)
			}
			for _, o := range catalog.Offers {
				if o.Slot == 3 {
					t.Fatal("NPC advertised monster-only pet gear")
				}
			}
			before := c.Snapshot()
			result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 3, Quantity: 1})
			if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || goldOf(c) != goldOf(before) {
				t.Fatal("forged pet-gear purchase changed inventory/gold")
			}
		}
	}
}

/*
================
TestMonsterPetGearDeathPickupAndReplay

The real fatal hit must publish the rolled item, and ordinary pickup must
preserve its options without allowing a second pickup or repeated kill.
================
*/
func TestMonsterPetGearDeathPickupAndReplay(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1)
	d := companion.D.Gear[0]
	item := d.Items[0]
	ref := &enterworld.ItemRef{RefObjID: item.ID, Codename: item.Codename, TypeIDs: [4]int64{3, 3, 9, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
	rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	rt.AttackPetExpansion = true
	rt.PetGearDropFactor = 1
	parent := rt.deps.ItemReferences().(staticItemSource)["ITEM_CH_SWORD_01_A"]
	parent.NativeFields = parent.NativeFields.With("itemClass", 1)
	rt.DropRoll = constantDropRoll(0)
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	r = assertAndSeparateActionSession(t, r)
	drops := rt.Ground.All(testDivision)
	if len(drops) == 0 || drops[len(drops)-1].RefObjID != ref.RefObjID || len(drops[len(drops)-1].MagicOptions) != 1 || drops[len(drops)-1].OwnerJID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("death drop: %+v frames %+v", drops, r.Frames)
	}
	drop := drops[len(drops)-1]
	rt.Ground.Restore(rt.Ground.Snapshot())
	finishTestCast(t, rt, clock, c)
	r = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	if r.Pending != nil {
		clock.Advance(r.Pending.Eta + time.Millisecond)
		rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	}
	back, ok := bagItem(c, ref.Codename)
	if !ok || back.StackCount != 1 || !reflect.DeepEqual(back.MagicOptions, drop.MagicOptions) || rt.Ground.Count(testDivision) != len(drops)-1 {
		t.Fatalf("pickup lost item/options: %+v", back)
	}
	before := c.Snapshot()
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || rt.Ground.Count(testDivision) != len(drops)-1 {
		t.Fatal("pickup/kill replay duplicated pet gear")
	}
}
