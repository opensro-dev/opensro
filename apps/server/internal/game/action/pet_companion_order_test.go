/*
===========================================================================

pet_companion_order_test.go - direct attack orders retain companion victims

The command validates permission through the victim owner, while its target
identity and subsequent damage belong to the requested companion.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPetAttackOrderTargetsEnemyCompanion
================
*/
func TestPetAttackOrderTargetsEnemyCompanion(t *testing.T) {
	for _, name := range []string{"live", "dead", "dismissed", "mounted", "pickup", "protected", "hidden", "owner-protected", "own", "missing", "party", "same-cape"} {
		t.Run(name, func(t *testing.T) {
			rt, clock, owner, victim := newPvpPair(t)
			ref := equipCombatTestPet(t, rt, owner, attackPetBand)
			ref.Parameters.DefaultSkillIDs[0] = 2
			ref.Parameters.HitRate = 10000
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			rt.BindPetSession(testDivision, owner, 1)
			pet := *owner.ActiveCOS
			pet.GID, _ = enterworld.CosObjectIDForCharacter(victim)
			victimRef := *ref
			victimRef.RefObjID, victimRef.Codename = ref.RefObjID+1, "ENEMY_PET"
			pet.RefObjID, pet.Codename = victimRef.RefObjID, victimRef.Codename
			rt.deps.ItemReferences().(cosTestItemSource).characters[victimRef.Codename] = &victimRef
			victim.ActiveCOS = &pet
			rt.BindPetSession(testDivision, victim, 1)
			target := pet.GID
			switch name {
			case "dead":
				pet.CurrentHP = 0
			case "dismissed":
				pet.Summoned = false
			case "mounted":
				pet.Mounted = true
			case "pickup":
				victimRef.TidWord = 4<<11 | 0x1c6
			case "protected":
				pet.NativeBodyStatus = untouchableBodyStatus
			case "hidden":
				pet.NativeBodyStatus = mercenaryBodyHiddenFirst
			case "owner-protected":
				victim.NativeBodyStatus = untouchableBodyStatus
			case "own":
				target = owner.ActiveCOS.GID
			case "missing":
				target = 0x7fff_fff0
			case "party":
				rt.RewardParties = func(string) []RewardParty {
					return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(owner), enterworld.ObjectIDForCharacter(victim)}}}
				}
			case "same-cape":
				cape := &enterworld.ItemRef{
					RefObjID: 63000, Codename: "ORDER_CAPE", TypeIDs: [4]int64{3, 1, 7, 5},
					NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: 1}),
				}
				rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[cape.Codename] = cape
				for _, actor := range []*enterworld.Character{owner, victim} {
					actor.MissionInventory = append(actor.MissionInventory, enterworld.InventoryRow{
						Slot: int64(jobSuitSlot), RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1,
					})
				}
			}
			rt.HandleCosCommand(testDivision, owner, petAttackOrder(owner.ActiveCOS.GID, target))
			state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
			if name != "live" {
				if state.combat != nil {
					t.Fatalf("invalid companion order admitted: %+v", state.combat)
				}
				return
			}
			if state.combat == nil || state.combat.target != pet.GID {
				t.Fatalf("enemy companion order lost its target: %+v", state.combat)
			}
			hp, ownerHP := pet.CurrentHP, enterworld.CurrentHP(victim)
			tickPetCombat(t, rt, clock, 100, func() bool { return pet.CurrentHP < hp })
			if enterworld.CurrentHP(victim) != ownerHP {
				t.Fatal("companion order damaged its owner")
			}
		})
	}
}
