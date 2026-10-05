/*
===========================================================================

questdrops_test.go - quest drop reference publication and pickup tests

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestFatalQuestDropPublishesReferenceBeforeSpawnAndSurvivesPickup
================
*/
func TestFatalQuestDropPublishesReferenceBeforeSpawnAndSurvivesPickup(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1)
	ref := &enterworld.ItemRef{RefObjID: 2201, Codename: "ITEM_QNO_CH_CHEF_1", TypeIDs: [4]int64{3, 3, 8, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
	rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	// Ordinary equipment admission fails; quest loot is independent.
	rt.DropRoll = func() (uint32, error) { return 32767, nil }
	called := 0
	rt.QuestMonsterDrops = func(character *enterworld.Character, code string, roll func() (uint32, error)) []inventory.ItemAmount {
		called++
		if character.ID != c.ID || code != target.Ref.Codename || roll == nil {
			t.Fatal("wrong quest drop context")
		}
		return []inventory.ItemAmount{{Codename: ref.Codename, Count: 1}}
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	r = assertAndSeparateActionSession(t, r)
	assertOpcodes(t, r.Frames, wire.OpSkillCastResult, wire.OpObjectStateRefresh, opCommerceItemReferences, wire.OpSingleObjectSpawn)
	drops := rt.Ground.All(testDivision)
	if called != 1 || len(drops) != 1 || drops[0].Codename != ref.Codename || drops[0].OwnerJID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("quest drops: %+v calls=%d", drops, called)
	}
	finishTestCast(t, rt, clock, c)
	pick := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drops[0].Gid}.Encode())
	// Scattered loot can require an approach; advance the real pending movement.
	if pick.Pending == nil {
		t.Fatal("scattered drop did not require approach")
	}
	clock.Advance(pick.Pending.Eta + time.Millisecond)
	pick = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drops[0].Gid}.Encode())
	assertOpcodes(t, pick.Frames, wire.OpPickupAnim, wire.OpItemMoveResponse, wire.OpObjectDespawn, wire.OpActionState)
	count := 0
	for _, item := range c.MissionInventory {
		if item.RefObjID == ref.RefObjID {
			count++
		}
	}
	if count != 1 || rt.Ground.Count(testDivision) != 0 {
		t.Fatal("quest pickup failed")
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if called != 1 || rt.Ground.Count(testDivision) != 0 {
		t.Fatal("fatal replay rerolled quest loot")
	}
}

/*
================
TestLivingMonsterDoesNotRollQuestLoot
================
*/
func TestLivingMonsterDoesNotRollQuestLoot(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 1000000)
	rt.QuestMonsterDrops = func(*enterworld.Character, string, func() (uint32, error)) []inventory.ItemAmount {
		t.Fatal("nonfatal quest drop")
		return nil
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
}
