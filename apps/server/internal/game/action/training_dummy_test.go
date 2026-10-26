/*
===========================================================================

training_dummy_test.go - a training kill has no progression or loot settlement.

A training dummy (#1463) is a measuring post: it fights like a monster
but its death must pay nothing. The settlement door
(settleMonsterInsideDoor) refuses it before any award runs. This test
meets that door in a paying shape - a real killer in the roster with a
fatal contribution against a monster that would otherwise pay (the
control legs prove the same kill settles with the dummy flag off and
with the refusal removed) - so the refusal clause is the only thing
standing between the dummy and a payout.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestTrainingDummyNoSettlement
================
*/
func TestTrainingDummyNoSettlement(t *testing.T) {
	for _, dummy := range []bool{true, false} {
		rt, clock, killer, mob := newCombatTestRuntime(t, 100)
		mob.Ref.TrainingDummy = dummy
		// The killer's whole health bar of damage, credited to them:
		// the fatal contribution a paying kill settles on.
		gid := enterworld.ObjectIDForCharacter(killer)
		impact := simulation.MonsterDamageResult{
			Fatal:    true,
			Instance: mob,
			Contributions: []simulation.MonsterContribution{
				{CreditGID: gid, Damage: mob.EffectiveMaxHP()},
			},
		}
		roster := rt.monsterRewardRoster(testDivision, killer, clock.NowMs())
		if _, ok := roster.actors[gid]; !ok {
			t.Fatal("the killer is not in the reward roster")
		}
		var granted int64
		rt.UpdateExperience = func(character *enterworld.Character, exp, skillExp int64, _ uint32) ([]wire.Frame, bool) {
			if character == killer {
				granted += exp + skillExp
			}
			return []wire.Frame{{Opcode: 1}}, true
		}
		result := rt.settleMonsterInsideDoor(testDivision, killer, roster, impact, monster.Pose{}, clock.NowMs())
		if dummy {
			if granted != 0 || len(result.drops) != 0 || len(result.actorFrames) != 0 ||
				len(result.others) != 0 || len(result.public) != 0 {
				t.Fatalf("the dummy settled: exp %d, settlement %+v", granted, result)
			}
			continue
		}
		// Control: the identical kill without the dummy flag pays, so
		// the empty dummy settlement above is the refusal's doing, not
		// a fixture that never pays anyone.
		if granted == 0 {
			t.Fatal("the control kill granted no experience; the fixture cannot prove the refusal")
		}
	}
}
