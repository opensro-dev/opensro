/*
===========================================================================

pet_area_team_test.go - cape teams protect secondary companion victims

Exercise the live area selector with aggression opening the owner's attack
permission. Companion team protection must still win after cape changes.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetAreaRechecksFreeBattleTeam
================
*/
func TestPetAreaRechecksFreeBattleTeam(t *testing.T) {
	for _, band := range []uint16{attackPetBand, domain.MercenaryBand} {
		for group := 1; group <= freeBattleAllOpponents; group++ {
			t.Run(fmt.Sprintf("band=%d/group=%d", band, group), func(t *testing.T) {
				rt, clock, owner, victim := newPvpPair(t)
				ref := equipCombatTestPet(t, rt, owner, band)
				rt.BindPetSession(testDivision, owner, 1)
				pet := *owner.ActiveCOS
				var valid bool
				pet.GID, valid = enterworld.CosObjectIDForCharacter(victim)
				if !valid {
					t.Fatal("victim has no companion identity")
				}
				victim.ActiveCOS = &pet
				rt.BindPetSession(testDivision, victim, 1)
				rt.RewardActorPresent = func(string, string) bool { return true }
				// No damage is calculated here. Level 20 opens the soldier's
				// normal-world enemy query; aggression keeps every control eligible.
				owner.Level, victim.Level = testInt64(playerCombatMinimumLevel), testInt64(playerCombatMinimumLevel)
				victim.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(owner): playerAggressionTicks}
				items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
				var capes [freeBattleAllOpponents + 1]*enterworld.ItemRef
				for color := 1; color <= freeBattleAllOpponents; color++ {
					cape := &enterworld.ItemRef{
						RefObjID: uint32(63000 + color), Codename: fmt.Sprintf("AREA_CAPE_%d", color),
						TypeIDs:      [4]int64{3, 1, 7, 5},
						NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: float64(color)}),
					}
					items[cape.Codename], capes[color] = cape, cape
				}
				state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
				at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
				state.follower = simulation.NewPetFollower(owner.ActiveCOS.GID, at)
				step := petCombatStep{
					key:   petOwnerKey{division: testDivision, name: owner.Name, gid: owner.ActiveCOS.GID},
					state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, nowMs: clock.NowMs(),
				}
				// The already-admitted primary supplies only the area center.
				primary := petCombatTarget{combatTarget: combatTarget{gid: 999999, at: at}}
				skill := enterworld.SkillRow{ActionArea: enterworld.SkillOffensiveArea{Shape: 2, Radius: 20, MaxTargets: 3, Select: 24}}
				for _, phase := range []struct {
					name                    string
					ownerGroup, victimGroup int
					want                    bool
				}{
					{"different", group, group%freeBattleAllOpponents + 1, true},
					{"same", group, group, group == freeBattleAllOpponents},
					{"owner-exit", 0, group, true},
					{"owner-return", group, group, group == freeBattleAllOpponents},
					{"victim-exit", group, 0, true},
				} {
					for i, actor := range []*enterworld.Character{owner, victim} {
						var inventory []enterworld.InventoryRow
						for _, row := range actor.MissionInventory {
							if row.Slot != int64(jobSuitSlot) {
								inventory = append(inventory, row)
							}
						}
						color := []int{phase.ownerGroup, phase.victimGroup}[i]
						if cape := capes[color]; cape != nil {
							inventory = append(inventory, enterworld.InventoryRow{
								Slot: int64(jobSuitSlot), RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1,
							})
						}
						actor.MissionInventory = inventory
					}
					found, foundPet := false, false
					for _, target := range rt.petAreaTargets(step, primary, skill) {
						found = found || target.gid == enterworld.ObjectIDForCharacter(victim)
						foundPet = foundPet || target.gid == pet.GID
					}
					if found != phase.want || foundPet != phase.want {
						t.Errorf("%s: secondary player=%t companion=%t, want %t", phase.name, found, foundPet, phase.want)
					}
				}
			})
		}
	}
}
