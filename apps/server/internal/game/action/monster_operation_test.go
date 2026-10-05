package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

func assertMonsterOperationHeld(t *testing.T, rt *Runtime) {
	t.Helper()
	lock := rt.operations.division[testDivision]
	if lock == nil {
		t.Fatal("missing division transaction")
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("committed monster result escaped the division transaction before publication")
	}
}

func TestMonsterOperationCoversDamageAndPublication(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
	skills[2] = skill
	before := enterworld.CurrentHP(c)
	rt.RunMonsterAction(testDivision, func(attack simulation.MonsterAttackOperation) {
		result := attack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
		if !result.Accepted || len(result.Frames) == 0 || enterworld.CurrentHP(c) >= before {
			t.Fatal("attack did not commit")
		}
		assertMonsterOperationHeld(t, rt)
	})
	unlock := rt.lockDivision(testDivision)
	unlock()
}

func TestPreparingMonsterPublishesBeforeUnlockWithoutReplay(t *testing.T) {
	for _, dead := range []bool{false, true} {
		rt, clock, c, m := newCombatTestRuntime(t, 100)
		m.Ref.DefaultSkillIDs[0] = 2
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.ActionCastingTimeMs = 1000
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
		skills[2] = skill
		if !rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs()).Accepted {
			t.Fatal("preparation refused")
		}
		before := enterworld.CurrentHP(c)
		if dead {
			rt.Monsters.ApplyDamage(testDivision, m.Gid, m.CurrentHP)
		}
		calls := 0
		rt.PushMonsterCast = func(division string, source uint32, result simulation.MonsterAttackResult) {
			calls++
			assertMonsterOperationHeld(t, rt)
			if division != testDivision || source != m.Gid || len(result.Frames) == 0 {
				t.Fatal("wrong publication route")
			}
			if dead {
				if result.Accepted || enterworld.CurrentHP(c) != before || result.Frames[0].Payload[0] != 2 {
					t.Fatal("dead preparation charged HP")
				}
			} else if !result.Accepted || enterworld.CurrentHP(c) >= before {
				t.Fatal("release did not commit")
			}
		}
		if out := rt.advanceMonsterCasts(clock.NowMs() + 1001); len(out) != 0 || calls != 1 {
			t.Fatalf("release duplicated or unpublished: %d/%d", len(out), calls)
		}
		if out := rt.advanceMonsterCasts(clock.NowMs() + 2000); len(out) != 0 || calls != 1 {
			t.Fatal("release replayed")
		}
	}
}
