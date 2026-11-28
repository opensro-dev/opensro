/*
===========================================================================

monsterdeferred_recovery_test.go - the deferred hit's latch against
natural recovery

A carrier that survives its own deferred hit keeps its HP latch down
until the instruction's duration runs out: no four-second recovery
while the latch stands, recovery resumes after it returns.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestDeferredSurvivorLatchBlocksRecovery
================
*/
func TestDeferredSurvivorLatchBlocksRecovery(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntimeMonsterTuned(t, 1000, 1, 0, func(ref *monster.MonsterRef) {
		ref.RecoveryHP = 100
	})
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	half := skills[2]
	half.DeferredVital = enterworld.SkillDeferredVital{Present: true, DurationMs: 60000, Percent: 50}
	skills[2] = half

	result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted {
		t.Fatalf("swing refused: %+v", result)
	}
	now := clock.NowMs()
	live, exists := rt.Monsters.Get(testDivision, m.Gid)
	if !exists || live.CurrentHP != 500 {
		t.Fatalf("survivor HP = %d (exists %v), want 500", live.CurrentHP, exists)
	}
	if hpLatch, _ := rt.Monsters.MonsterVitalLatches(testDivision, m.Gid); hpLatch {
		t.Fatal("HP latch still up after the deferred application")
	}

	rt.Monsters.AdvanceMonsterVitals(now + 4000)
	if live, _ := rt.Monsters.Get(testDivision, m.Gid); live.CurrentHP != 500 {
		t.Fatalf("HP while latched = %d, want 500 (no recovery)", live.CurrentHP)
	}

	rt.Monsters.AdvanceMonsterVitals(now + 60000) // latch returns here
	if live, _ := rt.Monsters.Get(testDivision, m.Gid); live.CurrentHP != 600 {
		t.Fatalf("HP at the latch's return = %d, want 600", live.CurrentHP)
	}
	rt.Monsters.AdvanceMonsterVitals(now + 64000)
	if live, _ := rt.Monsters.Get(testDivision, m.Gid); live.CurrentHP != 700 {
		t.Fatalf("HP one period later = %d, want 700", live.CurrentHP)
	}
}
