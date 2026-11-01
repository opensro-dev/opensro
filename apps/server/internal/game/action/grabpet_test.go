/*
===========================================================================

grabpet_test.go - pickup companions remain outside combat

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestGrabPetCannotBeAttackedOrAcquired
================
*/
func TestGrabPetCannotBeAttackedOrAcquired(t *testing.T) {
	rt, clock, c, mon := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, c, 4)
	mon.Ref.DefaultSkillIDs[0] = 2
	before := c.ActiveCOS.CurrentHP
	c.BattleUntilMs = 0
	result := rt.MonsterBasicAttack(testDivision, mon, c.ActiveCOS.GID, 2, clock.NowMs())
	if result.Accepted || c.ActiveCOS.CurrentHP != before || c.BattleUntilMs != 0 {
		t.Fatalf("grab pet entered combat: %+v", result)
	}
	for _, target := range rt.CompanionTargets(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs()) {
		if target.Gid == c.ActiveCOS.GID {
			t.Fatal("grab pet appears in monster acquisition candidates")
		}
	}
}

/*
================
TestGrabPetIgnoresPeriodicDamageAndResourceDebit
================
*/
func TestGrabPetIgnoresPeriodicDamageAndResourceDebit(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, c, 4)
	pet := c.ActiveCOS
	before := pet.CurrentHP
	if rt.livePet(c, pet.GID) {
		t.Fatal("pickup pet admitted combat recovery items")
	}
	owner := rt.newCosAbnormalOwnerForPet(testDivision, c, pet, clock.NowMs())
	owner.Hit(99, true, before, 1, 0)
	owner.ConsumeResources(int32(before), 1, 0)
	owner.commit()
	if pet.CurrentHP != before || owner.fatal || len(owner.hits) != 0 {
		t.Fatalf("pickup pet admitted periodic combat: %+v", pet)
	}
}

/*
================
TestGrassOfLifeHasNoServerBandCheck

The reference server's COS item use case 6 revives any COS summoner whose
state bit 0 is clear; the v1.150 client (6961B0) keeps grab pets out with
UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT. The server adds no band check.
================
*/
func TestGrassOfLifeHasNoServerBandCheck(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	pet := c.Companions()[0]
	pet.Summoned, pet.StateFlags, pet.CurrentHP = false, 0, 0
	ref := &enterworld.ItemRef{Codename: "REVIVE", RefObjID: 999, TypeIDs: [4]int64{3, 3, 1, 6}, Country: 3,
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	refs.staticItemSource[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(25).U16(ref.TypeFlags()).U8(24).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 || pet.StateFlags&1 == 0 {
		t.Fatalf("revival refused by band: %+v", result)
	}
}
