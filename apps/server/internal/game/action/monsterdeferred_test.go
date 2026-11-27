package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestMonsterDeferredSelfHitEndsTheBomb

A monster whose attack queues the deferred vital (the anti-bot bomb's
shape: its whole current HP) strikes its target and then dies by its
own swing, credited to nobody. The same swing without the block leaves
the attacker standing.
================
*/
func TestMonsterDeferredSelfHitEndsTheBomb(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 1000)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)

	plain := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !plain.Accepted {
		t.Fatalf("plain swing refused: %+v", plain)
	}
	if live, _ := rt.Monsters.Get(testDivision, m.Gid); live.CurrentHP == 0 {
		t.Fatal("the unmodified swing killed its own caster")
	}

	bomb := skills[2]
	bomb.DeferredVital = enterworld.SkillDeferredVital{Present: true, DurationMs: 1000, Percent: 100}
	skills[2] = bomb

	result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted {
		t.Fatalf("bomb swing refused: %+v", result)
	}
	live, exists := rt.Monsters.Get(testDivision, m.Gid)
	if !exists || live.CurrentHP != 0 {
		t.Fatalf("the bomb survived its own swing: exists=%v hp=%d", exists, live.CurrentHP)
	}
}
