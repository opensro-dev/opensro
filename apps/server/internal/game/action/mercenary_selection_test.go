/*
===========================================================================

mercenary_selection_test.go - soldier sight and nearest-distance boundaries

Drive acquisition through live player candidates, isolating the native
selector's inclusive sight test from its strict integer-best replacement.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestMercenarySelectionDistanceBoundaries
================
*/
func TestMercenarySelectionDistanceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second float64
		want          int
	}{
		{"inside", 129.5, 200, 1},
		{"at-sight-limit", 130, 200, 1},
		{"rounds-to-sight-limit", 130.000001, 200, 1},
		{"outside", 130.00002, 200, 0},
		{"equal-keeps-first", 10, 10, 1},
		{"fractional-nearer-keeps-integer-best", 10.9, 10.1, 1},
		{"integer-tie-keeps-first", 10.9, 10, 1},
		{"strictly-nearer-replaces", 10.9, 9.9, 2},
		{"rounded-integer-tie-keeps-first", 10.9, 9.9999999, 1},
		{"zero-keeps-first", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, owner, first := newPvpPair(t)
			ref := equipCombatTestPet(t, rt, owner, domain.MercenaryBand)
			rt.BindPetSession(testDivision, owner, 1)
			rt.Monsters = nil
			rt.RewardActorPresent = func(string, string) bool { return true }
			owner.Level = testInt64(playerCombatMinimumLevel)
			second := first.Snapshot()
			second.ID, second.Name = first.ID+1, "second"
			deps := rt.deps.(*enterworld.Deps)
			fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], second)
			at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
			for i, target := range []*enterworld.Character{first, second} {
				// This selector test does not calculate combat stats. Level 20
				// and target aggression make both players eligible enemies.
				target.Level = testInt64(playerCombatMinimumLevel)
				target.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(owner): playerAggressionTicks}
				pose := at
				pose.X += []float64{tc.first, tc.second}[i]
				rt.Worlds.Update(simulation.WorldKey(testDivision, target.Name), func() simulation.WorldState {
					return simulation.SeedWorldState(target)
				}, func(w *simulation.WorldState) { w.Spawn, w.SpawnSet = pose, true })
			}
			state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
			state.follower = simulation.NewPetFollower(owner.ActiveCOS.GID, at)
			rt.acquireMercenaryTarget(petCombatStep{
				key:   petOwnerKey{division: testDivision, name: owner.Name, gid: owner.ActiveCOS.GID},
				state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, nowMs: clock.NowMs(),
			})
			got := uint32(0)
			if state.combat != nil {
				got = state.combat.target
			}
			want := []uint32{0, enterworld.ObjectIDForCharacter(first), enterworld.ObjectIDForCharacter(second)}[tc.want]
			if got != want {
				t.Fatalf("selected %d, want %d", got, want)
			}
		})
	}
}

/*
================
TestMercenaryAcquiresMonsterAtSightLimit
================
*/
func TestMercenaryAcquiresMonsterAtSightLimit(t *testing.T) {
	for _, distance := range []float64{129.5, 130, 130.5} {
		rt, clock, owner, target := newCombatTestRuntime(t, 1000000)
		ref := equipCombatTestPet(t, rt, owner, domain.MercenaryBand)
		rt.BindPetSession(testDivision, owner, 1)
		state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
		at := rt.monsterSpawn(testDivision, target.Gid, clock.NowMs())
		at.X -= distance
		state.follower = simulation.NewPetFollower(owner.ActiveCOS.GID, at)
		rt.acquireMercenaryTarget(petCombatStep{
			key:   petOwnerKey{division: testDivision, name: owner.Name, gid: owner.ActiveCOS.GID},
			state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, nowMs: clock.NowMs(),
		})
		if distance > mercenarySightRange {
			if state.combat != nil {
				t.Fatalf("outside monster acquired at %g", distance)
			}
			continue
		}
		if state.combat == nil || state.combat.target != target.Gid {
			t.Fatalf("monster at %g not acquired: %+v", distance, state.combat)
		}
	}
}
